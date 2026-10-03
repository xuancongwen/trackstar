// Story links: #<id> references in descriptions and comments, #/s/<id> URLs.
import { apiClient, checker, launchBrowser, login, openBoard, seed, sleep, startTrackstar } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()
try {
  const sam = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam')
  const api = apiClient(trackstar.base, sam.cookie)
  const project = await seed(api)
  const stories = await api('GET', `/api/projects/${project.id}/stories`)
  const byTitle = (title) => stories.find((s) => s.title === title)
  const oauth = byTitle('Add OAuth support')
  const ci = byTitle('Set up CI')
  const dark = byTitle('Dark mode')
  const other = await api('POST', '/api/projects', { name: 'Gemini' })
  const elsewhere = await api('POST', `/api/projects/${other.id}/stories`, { title: 'Launch pad', section: 'backlog' })
  await api('PATCH', `/api/stories/${oauth.id}`, { description: `This was done in [#${ci.id}].\nSee also [#${elsewhere.id}], but let's do #2 first.` })
  await api('POST', `/api/stories/${ci.id}/comments`, { body: `Follow-up in [#${dark.id}]` })

  const page = await openBoard(browser, trackstar.base, sam.cookie, project.slug, errors)
  const openId = () => page.$eval('.editor header .id', (a) => Number(a.textContent.slice(1))).catch(() => null)
  const waitOpen = (id) => page.waitForFunction((id) => document.querySelector('.editor header .id')?.textContent === `#${id}`, { timeout: 5000 }, id)
  const hash = () => page.evaluate(() => location.hash)

  // --- references in the description
  await page.click(`[data-story-id="${oauth.id}"] .title`)
  await page.waitForSelector('.editor .description')
  t.eq('url names the open story', await hash(), `#/p/${project.slug}/s/${oauth.id}`)
  t.eq(
    'description references are links',
    await page.$$eval('.editor .description a', (as) => as.map((a) => [a.textContent, a.getAttribute('href'), a.title])),
    [
      [`#${ci.id}`, `#/s/${ci.id}`, 'Set up CI'],
      [`#${elsewhere.id}`, `#/s/${elsewhere.id}`, ''],
    ],
  )
  await page.click('.editor .description a')
  await waitOpen(ci.id)
  t.eq('link opens the referenced story', await hash(), `#/p/${project.slug}/s/${ci.id}`)

  // --- references in comments
  await page.waitForSelector('.editor article p a')
  t.eq('comment reference is a link', await page.$eval('.editor article p a', (a) => a.textContent), `#${dark.id}`)
  await page.click('.editor article p a')
  await waitOpen(dark.id)
  t.eq('icebox story opened from a comment', await openId(), dark.id)

  // --- back goes to the previous story
  await page.goBack()
  await waitOpen(ci.id)
  t.eq('back reopens the previous story', await hash(), `#/p/${project.slug}/s/${ci.id}`)

  // --- collapsing drops the story from the url
  await page.keyboard.press('Escape')
  await sleep(200)
  t.eq('collapse clears the story from the url', await hash(), `#/p/${project.slug}`)

  // --- editing the description: click to edit, blur to save and show links again
  await page.click(`[data-story-id="${oauth.id}"] .title`)
  await page.waitForSelector('.editor .description')
  await page.click('.editor .description')
  await page.waitForSelector('.editor textarea[aria-label="Description"]:focus')
  await page.keyboard.press('End')
  await page.keyboard.type(` Blocks [#${dark.id}]`)
  await page.click('.editor .title') // blur
  await page.waitForSelector('.editor .description')
  await sleep(300)
  t.eq('edited description renders its new link', await page.$$eval('.editor .description a', (as) => as.length), 3)
  t.eq('description saved', (await api('GET', `/api/stories/${oauth.id}`)).description.endsWith(`Blocks [#${dark.id}]`), true)

  // --- a link into another project switches boards
  await page.click(`.editor .description a[href="#/s/${elsewhere.id}"]`)
  await page.waitForFunction((slug) => location.hash.startsWith(`#/p/${slug}/`), { timeout: 5000 }, other.slug)
  await waitOpen(elsewhere.id)
  t.eq('cross-project link opens the other board', await page.evaluate(() => document.title.startsWith("Gemini")), true)

  // --- a fresh page on a short link
  const fresh = await browser.newPage()
  await fresh.setViewport({ width: 1400, height: 800 })
  fresh.on('pageerror', (e) => errors.push('pageerror: ' + e.message))
  await fresh.setCookie({ name: 'trackstar_session', value: sam.cookie, url: trackstar.base })
  await fresh.goto(`${trackstar.base}/#/s/${ci.id}`, { waitUntil: 'domcontentloaded' })
  await fresh.waitForFunction((id) => document.querySelector('.editor header .id')?.textContent === `#${id}`, { timeout: 5000 }, ci.id)
  t.eq('short link resolves to the board route', await fresh.evaluate(() => location.hash), `#/p/${project.slug}/s/${ci.id}`)

  // --- a trashed story opens in the trash panel
  await fresh.waitForSelector('.live.live', { timeout: 10000 })
  await api('DELETE', `/api/stories/${dark.id}`)
  await fresh.waitForFunction((id) => !document.querySelector(`[data-story-id="${id}"]`), { timeout: 5000 }, dark.id)
  await fresh.evaluate((id) => (location.hash = `#/s/${id}`), dark.id)
  await fresh.waitForSelector('section[aria-label="Deleted"]', { timeout: 5000 })
  await fresh.waitForFunction((id) => document.querySelector('.editor header .id')?.textContent === `#${id}`, { timeout: 5000 }, dark.id)
  t.eq('trashed story opens in the trash panel', await fresh.$eval('section[aria-label="Deleted"] .editor header .id', (a) => a.textContent), `#${dark.id}`)

  // --- an unknown story
  const before = errors.length
  await fresh.evaluate(() => (location.hash = '#/s/99999'))
  await fresh.waitForSelector('.link-error', { timeout: 5000 })
  t.eq('unknown story is reported', await fresh.$eval('.link-error', (e) => e.textContent.includes('#99999')), true)
  errors.splice(before) // the 404 for the unknown story
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
