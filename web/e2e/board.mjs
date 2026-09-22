// Single-user board behaviour: drag/drop (rows and empty column space),
// keyboard, workflow buttons, search, trash + undo, account dialog.
import { apiClient, checker, drag, dragOnto, dragToEmptySpace, launchBrowser, login, openBoard, order, seed, sleep, startTrackstar, storyId } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()
try {
  const { cookie } = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam Wen')
  const api = apiClient(trackstar.base, cookie)
  const project = await seed(api)
  const page = await openBoard(browser, trackstar.base, cookie, project.slug, errors)
  const id = (title) => storyId(page, title)
  const sel = (title) => `[data-story-id="${title}"]`

  t.eq('initial backlog', await order(page, 'backlog'), ['Add OAuth support', 'Story drag and drop', 'Velocity chart', 'Email notifications'])
  t.eq('initial icebox (newest on top)', await order(page, 'icebox'), ['CSV export', 'Dark mode'])
  t.eq('projection markers', await page.$$eval('.marker', (els) => els.map((e) => e.textContent.replace(/\s+/g, ' ').trim())), [
    'Iteration 2 · ' + (await page.$eval('.marker', (e) => e.textContent.match(/· (\w+ \d+)/)[1])) + ' 8 pts',
    (await page.$$eval('.marker', (els) => els[1].textContent.replace(/\s+/g, ' ').trim())),
  ])

  // --- drag and drop
  await dragOnto(page, sel(await id('Add OAuth support')), sel(await id('Velocity chart')))
  t.eq('reorder within backlog', await order(page, 'backlog'), ['Story drag and drop', 'Velocity chart', 'Add OAuth support', 'Email notifications'])
  await dragOnto(page, sel(await id('CSV export')), sel(await id('Story drag and drop')), false)
  t.eq('icebox → backlog before first row', await order(page, 'backlog'), ['CSV export', 'Story drag and drop', 'Velocity chart', 'Add OAuth support', 'Email notifications'])
  await dragToEmptySpace(page, sel(await id('Dark mode')), 'backlog')
  t.eq('drop in empty column space appends', await order(page, 'backlog').then((o) => o.at(-1)), 'Dark mode')
  t.eq('icebox now empty', await order(page, 'icebox'), [])
  await dragToEmptySpace(page, sel(await id('Dark mode')), 'icebox')
  t.eq('drop into an empty column', await order(page, 'icebox'), ['Dark mode'])
  await dragToEmptySpace(page, sel(await id('Set up CI')), 'backlog')
  t.eq('started story cannot leave current', await order(page, 'current'), ['Fix login redirect loop', 'Set up CI'])
  t.eq('…and backlog untouched', await order(page, 'backlog').then((o) => o.length), 5)

  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('[data-story-id]')
  t.eq('server persisted the order', await order(page, 'backlog'), ['CSV export', 'Story drag and drop', 'Velocity chart', 'Add OAuth support', 'Email notifications'])

  // --- keyboard
  await page.keyboard.press('j'); await page.keyboard.press('l'); await page.keyboard.press('j')
  t.eq('j/l/j selects second backlog row', await page.$eval('.story.selected .title', (e) => e.textContent), 'Story drag and drop')
  await page.keyboard.press('Enter'); await page.waitForSelector('.drawer')
  await page.waitForSelector('.timeline .event')
  t.eq('drawer shows creation activity', await page.$eval('.timeline .event', (e) => e.textContent.includes('created the story')), true)
  await page.keyboard.press('Escape')
  t.eq('esc closes drawer', (await page.$('.drawer')) === null, true)
  await page.keyboard.press('c'); await page.waitForSelector('form[aria-label="New story"]')
  await page.keyboard.type('Created from keyboard'); await page.keyboard.press('Enter'); await sleep(400)
  t.eq('c + Enter creates in the selected panel', await order(page, 'backlog').then((o) => o.at(-1)), 'Created from keyboard')
  t.eq('dialog closed after Enter', (await page.$('form[aria-label="New story"]')) === null, true)

  // --- workflow buttons and estimates
  const bug = await id('Fix login redirect loop')
  for (const label of ['Start', 'Finish', 'Deliver', 'Accept']) {
    const btn = await page.evaluateHandle((s, l) => [...document.querySelectorAll(`${s} button`)].find((b) => b.textContent.trim() === l), sel(bug), label)
    await btn.asElement().click(); await sleep(300)
  }
  t.eq('bug accepted via row buttons', await page.$eval(sel(bug), (e) => e.classList.contains('accepted')), true)
  await page.click(`${sel(await id('Created from keyboard'))} button[title="Estimate 3 points"]`); await sleep(300)
  t.eq('estimate from the row', await page.$eval(`${sel(await id('Created from keyboard'))} .points`, (e) => e.textContent), '3')

  // --- search
  await page.keyboard.press('/'); await page.keyboard.type('oauth'); await sleep(600)
  t.eq('search filters rows', [await order(page, 'backlog'), await order(page, 'icebox')], [['Add OAuth support'], []])
  await page.keyboard.press('Escape'); await page.keyboard.press('Escape'); await sleep(300)

  // --- trash and undo
  const victim = await id('Email notifications')
  await page.click(`${sel(victim)} .title`); await page.waitForSelector('.drawer')
  await page.click('.drawer footer button.danger'); await page.click('.drawer footer button.danger'); await sleep(400)
  t.eq('deleted story leaves the board', (await order(page, 'backlog')).includes('Email notifications'), false)
  t.eq('undo offered', await page.$eval('.undo', (e) => e.textContent.includes('Email notifications')), true)
  await page.click('.undo button'); await sleep(400)
  t.eq('undo restores at the bottom', await order(page, 'backlog').then((o) => o.at(-1)), 'Email notifications')
  await page.click(`${sel(victim)} .title`); await page.waitForSelector('.drawer')
  await page.click('.drawer footer button.danger'); await page.click('.drawer footer button.danger'); await sleep(400)
  await page.click('header.topbar button[title="Deleted stories"]'); await page.waitForSelector('section[aria-label="Deleted"]')
  t.eq('trash panel lists it', await page.$$eval('section[aria-label="Deleted"] .title', (els) => els.map((e) => e.textContent)), ['Email notifications'])
  await page.click('section[aria-label="Deleted"] .row > button:not(.link)'); await sleep(400)
  t.eq('restore from trash', (await order(page, 'backlog')).includes('Email notifications'), true)

  // --- account dialog
  await page.click('.menu > button'); await page.click('.menu-items button')
  await page.waitForSelector('form[aria-label="Account"]')
  await page.$eval('form[aria-label="Account"] input[maxlength="100"]', (e) => (e.value = ''))
  await page.type('form[aria-label="Account"] input[maxlength="100"]', 'Samantha')
  await page.click('form[aria-label="Account"] button.primary'); await sleep(400)
  t.eq('rename saved', await page.$eval('form[aria-label="Account"] .ok', (e) => e.textContent), 'Saved.')
  // --- API tokens (same dialog)
  await page.type('form[aria-label="Account"] input[aria-label="Token name"]', 'e2e-script')
  await page.keyboard.press('Enter'); await page.waitForSelector('form[aria-label="Account"] .secret code')
  const secret = await page.$eval('form[aria-label="Account"] .secret code', (e) => e.textContent.trim())
  t.eq('token has the tst_ prefix', secret.startsWith('tst_'), true)
  t.eq('token is listed', await page.$$eval('form[aria-label="Account"] .token-list .name', (els) => els.map((e) => e.textContent)), ['e2e-script'])
  // Bearer requests are made from Node, not the page: the browser logs 4xx
  // responses to the console and the harness treats those as errors.
  const bearer = (path, init = {}) => fetch(trackstar.base + path, { ...init, headers: { Authorization: 'Bearer ' + secret, ...(init.headers ?? {}) } })
  const viaBearer = await bearer('/api/me')
  t.eq('bearer token authenticates as the owner', viaBearer.status === 200 ? (await viaBearer.json()).display_name : viaBearer.status, 'Samantha')
  const mint = await bearer('/api/me/tokens', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'nope' }) })
  t.eq('a token cannot mint tokens', mint.status, 403)
  await page.click('form[aria-label="Account"] .token-list button'); await sleep(300)
  t.eq('token revoked', await page.$$eval('form[aria-label="Account"] .token-list li', (els) => els.length), 0)
  t.eq('revoked token is rejected', (await bearer('/api/me')).status, 401)
  await page.keyboard.press('Escape')
  t.eq('topbar shows the new name', await page.$eval('.menu > button', (e) => e.textContent.trim()), 'Samantha ▾')

  // --- project switcher (the project name in the top bar)
  const zephyr = await api('POST', '/api/projects', { name: 'Zephyr' })
  await page.click('.switcher > button'); await page.waitForSelector('.switcher-items [role="option"]')
  t.eq('switcher lists every project', await page.$$eval('.switcher-items [role="option"]', (els) => els.map((e) => e.textContent.trim())), ['Apollo', 'Zephyr'])
  t.eq('current project is marked', await page.$eval('.switcher-items .current', (e) => e.textContent.trim()), 'Apollo')
  await page.click('.switcher-items [role="option"]:last-of-type')
  await page.waitForFunction((slug) => location.hash === `#/p/${slug}`, {}, zephyr.slug)
  await page.waitForSelector('.live.live')
  t.eq('switching opens the other board', await page.$eval('.switcher > button strong', (e) => e.textContent.trim()), 'Zephyr')
  t.eq('switcher closed after picking', await page.$('.switcher-items'), null)
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
