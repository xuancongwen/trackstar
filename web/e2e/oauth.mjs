// OAuth for MCP clients, as a client and a browser see it: register, sign in
// on the way to the consent screen (phone-sized), approve, exchange the
// code, call /mcp, then disconnect the app from the Account dialog.
import { createHash, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import { KnownDevices } from 'puppeteer-core'
import { apiClient, checker, launchBrowser, login, openBoard, seed, sleep, startTrackstar } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()

// The client's redirect URI: a loopback listener that records what arrives.
const arrivals = []
let waiting = null
const callbackServer = createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1')
  if (url.pathname === '/callback') {
    if (waiting) waiting(url.searchParams)
    else arrivals.push(url.searchParams)
    waiting = null
  }
  res.end('<html><body>back at the client</body></html>')
})
await new Promise((resolve) => callbackServer.listen(0, '127.0.0.1', resolve))
const redirectURI = `http://127.0.0.1:${callbackServer.address().port}/callback`
const nextCallback = () =>
  arrivals.length
    ? Promise.resolve(arrivals.shift())
    : Promise.race([
        new Promise((resolve) => (waiting = resolve)),
        sleep(10000).then(() => {
          throw new Error('the browser never reached the redirect URI')
        }),
      ])

const form = (path, fields) =>
  fetch(trackstar.base + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded', Origin: 'https://inspector.example' },
    body: new URLSearchParams(fields),
  })
const mcp = (token) =>
  fetch(trackstar.base + '/mcp', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '0' } },
    }),
  })
const watch = (page) => page.on('pageerror', (e) => errors.push('pageerror: ' + e.message))
const heading = (page) => page.$eval('section[aria-label="Connect an app"] h2', (e) => e.textContent)
const press = (page, label) =>
  page.evaluate((l) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === l).click(), label)

try {
  const sam = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam')
  const project = await seed(apiClient(trackstar.base, sam.cookie))

  // Discovery, the way a client that only has the /mcp URL does it.
  const challenge401 = await mcp()
  const metadataURL = challenge401.headers.get('www-authenticate')?.match(/resource_metadata="([^"]+)"/)?.[1]
  t.eq('/mcp without credentials is a 401 that names the metadata', [challenge401.status, metadataURL], [401, `${trackstar.base}/.well-known/oauth-protected-resource`])
  const resource = await (await fetch(metadataURL)).json()
  const as = await (await fetch(`${resource.authorization_servers[0]}/.well-known/oauth-authorization-server`)).json()
  t.eq('metadata leads to the endpoints', [resource.resource, as.authorization_endpoint, as.token_endpoint], [`${trackstar.base}/mcp`, `${trackstar.base}/oauth/authorize`, `${trackstar.base}/oauth/token`])

  const registration = await fetch(as.registration_endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client_name: 'E2E Client', redirect_uris: [redirectURI], token_endpoint_auth_method: 'none' }),
  })
  const { client_id } = await registration.json()
  t.eq('client registered itself', [registration.status, typeof client_id], [201, 'string'])

  const verifier = randomBytes(32).toString('base64url')
  const authorizeURL = (overrides = {}) => {
    const params = new URLSearchParams({
      response_type: 'code',
      client_id,
      redirect_uri: redirectURI,
      code_challenge: createHash('sha256').update(verifier).digest('base64url'),
      code_challenge_method: 'S256',
      state: 'e2e-state',
      resource: resource.resource,
      ...overrides,
    })
    for (const [k, v] of [...params]) if (v === '') params.delete(k)
    return `${as.authorization_endpoint}?${params}`
  }

  // Not signed in, on a phone: the login form, then straight to consent.
  const phone = await browser.newPage()
  await phone.emulate(KnownDevices['iPhone 13'])
  watch(phone)
  await phone.goto(authorizeURL(), { waitUntil: 'domcontentloaded' })
  await phone.waitForSelector('input[type="email"]')
  await phone.type('input[type="email"]', 'sam@example.com')
  await phone.type('input[type="password"]', 'e2e-password-1')
  await phone.keyboard.press('Enter')
  await phone.waitForSelector('section[aria-label="Connect an app"] h2')
  t.eq('consent names the client after signing in', await heading(phone), 'Connect E2E Client?')
  const consentText = await phone.$eval('section[aria-label="Connect an app"]', (e) => e.textContent)
  t.eq('consent shows where the browser goes next', consentText.includes(new URL(redirectURI).host), true)
  t.eq('consent says who is signed in', consentText.includes('sam@example.com'), true)
  t.eq('consent fits a phone screen', await phone.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
  t.eq('the request survived the login form', new URL(phone.url()).searchParams.get('state'), 'e2e-state')

  await press(phone, 'Approve')
  const approved = await nextCallback()
  t.eq('approve returns a code and the state', [approved.get('state'), approved.get('error'), (approved.get('code') ?? '').length > 20], ['e2e-state', null, true])

  const exchange = await form('/oauth/token', {
    grant_type: 'authorization_code',
    client_id,
    code: approved.get('code'),
    redirect_uri: redirectURI,
    code_verifier: verifier,
    resource: resource.resource,
  })
  const tokens = await exchange.json()
  t.eq('code exchanged for tokens', [exchange.status, tokens.token_type, tokens.access_token?.slice(0, 4), tokens.refresh_token?.slice(0, 4)], [200, 'Bearer', 'tsa_', 'tsr_'])
  t.eq('the access token opens /mcp', (await mcp(tokens.access_token)).status, 200)
  t.eq('a code works once', (await form('/oauth/token', { grant_type: 'authorization_code', client_id, code: approved.get('code'), code_verifier: verifier })).status, 400)
  t.eq('reusing the code revoked what it issued', (await mcp(tokens.access_token)).status, 401)

  // Signed in already: no login form. Deny goes back to the client too.
  const desktop = await browser.newPage()
  await desktop.setViewport({ width: 1200, height: 800 })
  watch(desktop)
  await desktop.setCookie({ name: 'trackstar_session', value: sam.cookie, url: trackstar.base })
  await desktop.goto(authorizeURL(), { waitUntil: 'domcontentloaded' })
  await desktop.waitForSelector('section[aria-label="Connect an app"] h2')
  await press(desktop, 'Deny')
  const denied = await nextCallback()
  t.eq('deny reports access_denied', [denied.get('error'), denied.get('state'), denied.get('code')], ['access_denied', 'e2e-state', null])

  // A request the client got wrong goes back to it without asking the user.
  await desktop.goto(authorizeURL({ code_challenge: '', code_challenge_method: '' }), { waitUntil: 'domcontentloaded' })
  const rejected = await nextCallback()
  t.eq('missing PKCE is rejected by redirect', [rejected.get('error'), rejected.get('state')], ['invalid_request', 'e2e-state'])

  // A redirect URI the client never registered is shown, not followed.
  await desktop.goto(authorizeURL({ redirect_uri: 'https://evil.example/callback' }), { waitUntil: 'domcontentloaded' })
  await desktop.waitForSelector('section[aria-label="Connect an app"] [role="alert"]')
  t.eq('unregistered redirect URI stays on Trackstar', [await heading(desktop), new URL(desktop.url()).origin], ['Cannot connect this app', trackstar.base])
  t.eq('and offers nothing to approve', await desktop.$$eval('section[aria-label="Connect an app"] button.primary', (els) => els.length), 0)

  // Connect for real, then disconnect from the Account dialog.
  await desktop.goto(authorizeURL(), { waitUntil: 'domcontentloaded' })
  await desktop.waitForSelector('section[aria-label="Connect an app"] h2')
  await press(desktop, 'Approve')
  const again = await nextCallback()
  const live = await (await form('/oauth/token', { grant_type: 'authorization_code', client_id, code: again.get('code'), code_verifier: verifier })).json()
  t.eq('connected again', (await mcp(live.access_token)).status, 200)
  const refreshed = await (await form('/oauth/token', { grant_type: 'refresh_token', client_id, refresh_token: live.refresh_token })).json()
  t.eq('refresh rotates both tokens', [refreshed.access_token !== live.access_token, refreshed.refresh_token !== live.refresh_token, (await mcp(refreshed.access_token)).status], [true, true, 200])

  const board = await openBoard(browser, trackstar.base, sam.cookie, project.slug, errors)
  await board.click('button[aria-label="Menu"]')
  await press(board, 'Account…')
  await board.waitForSelector('form[aria-label="Account"] fieldset.apps li')
  t.eq('the app is listed under Connected apps', await board.$$eval('fieldset.apps li .name', (els) => els.map((e) => e.textContent)), ['E2E Client'])
  await board.click('fieldset.apps li button')
  await board.waitForSelector('fieldset.apps li', { hidden: true })
  t.eq('disconnecting removes it from the list', await board.$eval('fieldset.apps', (e) => e.textContent.includes('No connected apps.')), true)
  t.eq('and cuts the client off', (await mcp(refreshed.access_token)).status, 401)
  t.eq('for good', (await form('/oauth/token', { grant_type: 'refresh_token', client_id, refresh_token: refreshed.refresh_token })).status, 400)
} finally {
  await browser.close()
  callbackServer.close()
  trackstar.stop()
}
t.done(errors)
