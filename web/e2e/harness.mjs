// Shared harness: starts a fresh tracker binary on a free port with a temp
// data dir, registers a user, seeds a project, and returns logged-in pages.
//
//   TRACKER_BIN=../bin/tracker CHROME_PATH=/usr/bin/chromium node e2e/board.mjs
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import path from 'node:path'
import puppeteer from 'puppeteer-core'

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const freePort = () =>
  new Promise((resolve, reject) => {
    const srv = createServer()
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address()
      srv.close(() => resolve(port))
    })
    srv.on('error', reject)
  })

export async function startTracker() {
  const bin = process.env.TRACKER_BIN ?? path.resolve('../bin/tracker')
  const port = await freePort()
  const dataDir = mkdtempSync(path.join(tmpdir(), 'tracker-e2e-'))
  const base = `http://127.0.0.1:${port}`
  const proc = spawn(bin, [], {
    env: { ...process.env, TRACKER_ADDR: `127.0.0.1:${port}`, TRACKER_DATA_DIR: dataDir, TRACKER_PUBLIC_URL: base + '/', TRACKER_LOG_LEVEL: 'warn' },
    stdio: ['ignore', 'inherit', 'inherit'],
  })
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(base + '/health')).ok) break
    } catch {}
    await sleep(50)
  }
  const stop = () => {
    proc.kill()
    rmSync(dataDir, { recursive: true, force: true })
  }
  return { base, stop }
}

/** Registers (or logs in) and returns the session cookie value. */
export async function login(base, email, password = 'e2e-password-1', displayName = email.split('@')[0]) {
  let res = await fetch(base + '/api/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Origin: base },
    body: JSON.stringify({ email, password, display_name: displayName }),
  })
  if (res.status === 409) {
    res = await fetch(base + '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Origin: base },
      body: JSON.stringify({ email, password }),
    })
  }
  if (!res.ok) throw new Error(`login ${email}: ${res.status} ${await res.text()}`)
  const cookie = res.headers.get('set-cookie')?.match(/tracker_session=([^;]+)/)?.[1]
  if (!cookie) throw new Error('no session cookie')
  return { cookie, user: await res.json() }
}

export function apiClient(base, cookie) {
  return async (method, path, body) => {
    const res = await fetch(base + path, {
      method,
      headers: { 'Content-Type': 'application/json', Origin: base, Cookie: `tracker_session=${cookie}` },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${await res.text()}`)
    return res.status === 204 ? null : res.json()
  }
}

export async function seed(api) {
  const p = await api('POST', '/api/projects', { name: 'Apollo' })
  const add = (s) => api('POST', `/api/projects/${p.id}/stories`, s)
  await add({ title: 'Add OAuth support', estimate: 3, section: 'backlog', labels: ['auth'] })
  await add({ title: 'Story drag and drop', estimate: 5, section: 'backlog' })
  await add({ title: 'Velocity chart', estimate: 5, section: 'backlog' })
  await add({ title: 'Email notifications', estimate: 2, section: 'backlog' })
  await add({ title: 'Fix login redirect loop', type: 'bug', section: 'current' })
  const ci = await add({ title: 'Set up CI', type: 'chore', section: 'current' })
  await add({ title: 'Dark mode', section: 'icebox' })
  await add({ title: 'CSV export', estimate: 2, section: 'icebox' })
  await api('PATCH', `/api/stories/${ci.id}`, { state: 'started' })
  return p
}

export async function launchBrowser() {
  return puppeteer.launch({
    executablePath: process.env.CHROME_PATH ?? process.env.PUPPETEER_EXECUTABLE_PATH ?? '/usr/bin/chromium',
    headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage'],
  })
}

/** A logged-in page on the project board; collects console/page errors. */
export async function openBoard(browser, base, cookie, slug, errors) {
  const page = await browser.newPage()
  await page.setViewport({ width: 1400, height: 800 })
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message))
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push('console: ' + m.text())
  })
  await page.setCookie({ name: 'tracker_session', value: cookie, url: base })
  await page.goto(`${base}/#/p/${slug}`, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.live.live', { timeout: 10000 })
  await page.waitForSelector('[data-story-id]')
  return page
}

export const order = (page, section) =>
  page.$$eval(`[data-section="${section}"] [data-story-id] .title`, (els) => els.map((e) => e.textContent))

export async function drag(page, fromSel, x2, y2) {
  const b = await (await page.$(fromSel)).boundingBox()
  const x1 = b.x + b.width / 3, y1 = b.y + b.height / 2
  await page.mouse.move(x1, y1)
  await page.mouse.down()
  await page.mouse.move(x1 + 5, y1 + 5, { steps: 3 })
  await page.mouse.move(x2, y2, { steps: 15 })
  await sleep(250)
  await page.mouse.move(x2 + 2, y2, { steps: 2 })
  await sleep(250)
  await page.mouse.up()
  await sleep(500)
}

export async function dragOnto(page, fromSel, toSel, below = true) {
  const t = await (await page.$(toSel)).boundingBox()
  await drag(page, fromSel, t.x + t.width / 3, below ? t.y + t.height - 3 : t.y + 3)
}

export async function dragToEmptySpace(page, fromSel, section) {
  const b = await (await page.$(`[data-section="${section}"]`)).boundingBox()
  await drag(page, fromSel, b.x + b.width / 2, b.y + b.height - 40)
}

export const storyId = async (page, title) =>
  page.$$eval('[data-story-id]', (els, t) => Number(els.find((e) => e.querySelector('.title')?.textContent === t)?.dataset.storyId), title)

// tiny assertion helper that keeps going and reports at the end
export function checker() {
  const failures = []
  return {
    eq(name, got, want) {
      const ok = JSON.stringify(got) === JSON.stringify(want)
      console.log(`${ok ? 'ok  ' : 'FAIL'} ${name}${ok ? '' : `\n     got  ${JSON.stringify(got)}\n     want ${JSON.stringify(want)}`}`)
      if (!ok) failures.push(name)
    },
    done(errors) {
      if (errors.length) {
        console.log('browser errors:', errors)
        failures.push('browser errors')
      }
      if (failures.length) {
        console.log(`\n${failures.length} failure(s)`)
        process.exit(1)
      }
      console.log('\nall passed')
    },
  }
}
