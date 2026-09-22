// Milestone 3: epics, tasks, blockers, filters, multi-select, membership.
import { apiClient, checker, dragOnto, launchBrowser, login, openBoard, order, seed, sleep, startTrackstar, storyId } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()
try {
  const sam = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam')
  const kim = await login(trackstar.base, 'kim@example.com', 'e2e-password-1', 'Kim')
  const api = apiClient(trackstar.base, sam.cookie)
  const project = await seed(api)
  const page = await openBoard(browser, trackstar.base, sam.cookie, project.slug, errors)
  const id = (title) => storyId(page, title)
  const sel = async (title) => `[data-story-id="${await id(title)}"]`
  const visibleTitles = () => page.$$eval('[data-section] [data-story-id] .title', (els) => els.map((e) => e.textContent))

  // --- epics
  await page.keyboard.press('e')
  await page.waitForSelector('section[aria-label="Epics"]')
  await page.type('section[aria-label="Epics"] input[aria-label="New epic name"]', 'Auth')
  await page.keyboard.press('Enter')
  await page.waitForSelector('section[aria-label="Epics"] .epic')
  t.eq('epic created', await page.$eval('section[aria-label="Epics"] .epic .name', (e) => e.textContent.trim()), 'auth')
  // story with label "auth" (seeded) counts toward it
  t.eq('epic progress from labels', await page.$eval('section[aria-label="Epics"] .epic .points', (e) => e.textContent), '0/3')
  await page.click('section[aria-label="Epics"] .epic .name')
  await sleep(200)
  t.eq('epic filters the board', await visibleTitles(), ['Add OAuth support'])
  t.eq('epic label styled', await page.$eval('.label.epic', (e) => e.textContent), 'auth')
  await page.click('section[aria-label="Epics"] .clear')
  await sleep(200)
  t.eq('show all clears the epic filter', (await visibleTitles()).length, 8)

  // --- filter language (local, instant)
  await page.keyboard.press('/')
  await page.keyboard.type('type:bug')
  await sleep(150)
  t.eq('type:bug', await visibleTitles(), ['Fix login redirect loop'])
  await page.$eval('input[aria-label="Filter stories"]', (e) => (e.value = ''))
  await page.type('input[aria-label="Filter stories"]', 'owner:me')
  await sleep(150)
  t.eq('owner:me (Set up CI was started by sam)', await visibleTitles(), ['Set up CI'])
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  await sleep(150)
  t.eq('esc clears the filter', (await visibleTitles()).length, 8)
  // saved filter via the menu
  await page.type('input[aria-label="Filter stories"]', 'is:unestimated')
  await page.click('.filter > button')
  await page.click('.filter .menu button[role="menuitem"]:last-of-type') // "Save current filter…"
  await page.type('.filter .menu form input', 'Needs points')
  await page.keyboard.press('Enter')
  await sleep(300)
  t.eq('filter saved', await page.$$eval('.filter .menu .saved span', (els) => els.map((e) => e.textContent)), ['Needs points'])
  await page.keyboard.press('Escape'); await page.keyboard.press('Escape'); await sleep(150)

  // --- tasks and blockers in the drawer
  await page.click(`${await sel('Story drag and drop')} .title`)
  await page.waitForSelector('.drawer')
  await page.type('input[aria-label="New task"]', 'write the adapter')
  await page.keyboard.press('Enter')
  await page.type('input[aria-label="New task"]', 'test it')
  await page.keyboard.press('Enter')
  await sleep(300)
  await page.click('.drawer .tasks li input[type=checkbox]')
  await sleep(300)
  t.eq('tasks added and one done', await page.$eval('.drawer .tasks', (e) => [...e.querySelectorAll('li')].map((li) => li.classList.contains('done'))), [true, false])
  await page.select('select[aria-label="Add blocker"]', String(await id('Velocity chart')))
  await sleep(400)
  t.eq('blocker listed', await page.$eval('.drawer .blockers li span', (e) => e.textContent.includes('Velocity chart')), true)
  await page.keyboard.press('Escape')
  await sleep(200)
  t.eq('row shows task count', await page.$eval(`${await sel('Story drag and drop')} .tasks`, (e) => e.textContent.trim()), '☑ 1/2')
  t.eq('row shows blocked marker', (await page.$(`${await sel('Story drag and drop')} .blocked`)) !== null, true)
  await page.type('input[aria-label="Filter stories"]', 'is:blocked')
  await sleep(150)
  t.eq('is:blocked filter', await visibleTitles(), ['Story drag and drop'])
  await page.keyboard.press('Escape'); await sleep(150)

  // --- multi-select: x on two rows, drag one → both move together
  const before = await order(page, 'backlog')
  await page.click(`${await sel(before[0])} .title`); await page.keyboard.press('Escape') // focus the first backlog row
  await page.keyboard.press('x') // keyboard toggle
  await page.keyboard.down('Shift'); await page.click(`${await sel(before[1])} .title`); await page.keyboard.up('Shift') // shift-click toggle
  t.eq('two rows checked', await page.$$eval('.story.checked', (els) => els.length), 2)
  t.eq('selection chip', await page.$eval('.chip.selection', (e) => e.textContent.trim()), '2 selected')
  await dragOnto(page, await sel(before[0]), await sel(before[3]))
  t.eq('dragging a checked row moves the set', await order(page, 'backlog'), [before[2], before[3], before[0], before[1]])
  t.eq('selection cleared after move', await page.$$eval('.story.checked', (els) => els.length), 0)
  // Shift+B moves the focused story to the end of backlog
  await page.click(`${await sel('Dark mode')} .title`); await page.keyboard.press('Escape')
  await page.keyboard.down('Shift'); await page.keyboard.press('B'); await page.keyboard.up('Shift')
  await sleep(500)
  t.eq('Shift+B appends to backlog', (await order(page, 'backlog')).at(-1), 'Dark mode')

  // --- done panel chart renders
  await page.click('header.topbar button:not(.on)[class=""]').catch(() => {})
  await page.evaluate(() => [...document.querySelectorAll('header.topbar button')].find((b) => b.textContent.trim() === 'Done')?.click())
  await page.waitForSelector('section[aria-label="Done"]')
  t.eq('iteration chart drawn', (await page.$('section[aria-label="Done"] svg.chart rect.bar')) !== null, true)

  // --- membership: sam member, kim viewer → kim's board is read-only
  await api('PUT', `/api/projects/${project.id}/members/${sam.user.id}`, { role: 'member' })
  await api('PUT', `/api/projects/${project.id}/members/${kim.user.id}`, { role: 'viewer' })
  const kimPage = await openBoard(browser, trackstar.base, kim.cookie, project.slug, errors)
  t.eq('viewer sees read-only chip', (await kimPage.$('.chip')) !== null && (await kimPage.$eval('.chip', (e) => e.textContent)) === 'read-only', true)
  t.eq('viewer has no action buttons on rows', await kimPage.$$eval('[data-story-id] button', (els) => els.length), 0)
  t.eq('viewer has no + Story', await kimPage.$$eval('header.topbar button', (els) => els.some((b) => b.textContent.includes('+ Story'))), false)
  await api('DELETE', `/api/projects/${project.id}/members/${kim.user.id}`)
  await api('DELETE', `/api/projects/${project.id}/members/${sam.user.id}`)
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
