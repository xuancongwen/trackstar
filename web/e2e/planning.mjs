// Milestone 3: epics, tasks, blockers, filters, multi-select, membership (owners and members).
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

  // --- membership: sam owns the seeded project; kim sees it only once added
  const kimApi = apiClient(trackstar.base, kim.cookie)
  t.eq('creator is the owner', (await api('GET', `/api/projects/${project.id}/members`)).map((m) => m.role), ['owner'])
  t.eq('non-member cannot see the project', await kimApi('GET', `/api/projects/${project.id}`).catch((e) => e.message.includes(' 404 ')), true)
  await api('PUT', `/api/projects/${project.id}/members/${kim.user.id}`, { role: 'member' })
  // Kim gets her own browser context: pages share a cookie jar otherwise, and
  // setting Kim's session cookie would turn Sam's page into Kim's.
  const kimPage = await openBoard(await browser.createBrowserContext(), trackstar.base, kim.cookie, project.slug, errors)
  t.eq('member has + Story', await kimPage.$$eval('header.topbar button', (els) => els.some((b) => b.textContent.includes('+ Story'))), true)
  await kimPage.evaluate(() => [...document.querySelectorAll('header.topbar button')].find((b) => b.textContent.trim() === 'Settings')?.click())
  await kimPage.waitForSelector('form[aria-label="Project settings"]')
  t.eq('member cannot edit settings', await kimPage.$eval('form[aria-label="Project settings"] input', (e) => e.matches(':disabled')), true)
  t.eq('member cannot change members', await kimPage.$$eval('form[aria-label="Project settings"] .members select', (els) => els.length), 0)
  t.eq('member cannot promote', await kimApi('PUT', `/api/projects/${project.id}/members/${kim.user.id}`, { role: 'owner' }).catch((e) => e.message.includes(' 403 ')), true)
  t.eq('last owner cannot leave', await api('DELETE', `/api/projects/${project.id}/members/${sam.user.id}`).catch((e) => e.message.includes(' 422 ')), true)
  await kimPage.keyboard.press('Escape')

  // --- archive (reversible, read-only for everyone) and delete (for good)
  const settingsButton = (pg) => pg.evaluate(() => [...document.querySelectorAll('header.topbar button')].find((b) => b.textContent.trim() === 'Settings')?.click())
  const dangerButton = (pg, label) =>
    pg.evaluate((label) => [...document.querySelectorAll('form[aria-label="Project settings"] fieldset.danger button')].find((b) => b.textContent.trim() === label)?.click(), label)
  const chips = (pg) => pg.$$eval('header.topbar .chip', (els) => els.map((e) => e.textContent.trim()))
  await page.keyboard.press('Escape')
  await settingsButton(page)
  await page.waitForSelector('form[aria-label="Project settings"] fieldset.danger')
  t.eq('member settings have no danger zone', await kimPage.$$eval('fieldset.danger', (els) => els.length), 0)
  await dangerButton(page, 'Archive')
  await page.waitForSelector('form[aria-label="Project settings"]', { hidden: true })
  t.eq('archived chip', (await chips(page)).includes('archived'), true)
  t.eq('owner loses + Story while archived', await page.$$eval('header.topbar button', (els) => els.some((b) => b.textContent.includes('+ Story'))), false)
  await kimPage.waitForFunction(() => [...document.querySelectorAll('header.topbar .chip')].some((e) => e.textContent.trim() === 'archived'), { timeout: 5000 })
  t.eq('member sees the archive live', true, true)
  t.eq('member writes refused', await kimApi('PATCH', `/api/stories/${await id('Dark mode')}`, { title: 'x' }).catch((e) => e.message.includes(' 403 ') && e.message.includes('archived')), true)
  await settingsButton(page)
  await page.waitForSelector('form[aria-label="Project settings"] fieldset.danger')
  await dangerButton(page, 'Unarchive')
  await page.waitForSelector('form[aria-label="Project settings"]', { hidden: true })
  await sleep(200)
  t.eq('unarchived', (await chips(page)).includes('archived'), false)

  await settingsButton(page)
  await page.waitForSelector('form[aria-label="Project settings"] fieldset.danger')
  await dangerButton(page, 'Delete…')
  await page.waitForSelector('fieldset.danger .confirm input')
  t.eq('delete needs the name', await page.$eval('fieldset.danger .confirm button.destructive', (b) => b.disabled), true)
  await page.type('fieldset.danger .confirm input', 'Apollo')
  const errorsBeforeDelete = errors.length
  await dangerButton(page, 'Delete project')
  await page.waitForFunction(() => location.hash === '#/' || location.hash === '')
  await page.waitForSelector('main h1')
  t.eq('back on the project list, project gone', await page.$$eval('main li', (els) => els.length), 0)
  await kimPage.waitForSelector('.load-error', { timeout: 5000 })
  t.eq('member is told the project is gone', (await kimPage.$eval('.load-error', (e) => e.textContent)).includes('no longer available'), true)
  // Kim's board asked for the deleted project once more; those 404s are the point.
  t.eq('only 404s after the delete', errors.slice(errorsBeforeDelete).every((e) => e.includes('404')), true)
  errors.splice(errorsBeforeDelete)
  t.eq('deleted project is gone from the API', await api('GET', `/api/projects/${project.id}`).catch((e) => e.message.includes(' 404 ')), true)
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
