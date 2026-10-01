// Phone layout (iPhone 13 emulation, touch): tabs, FAB, editor "Move to",
// hold-to-drag reorder, menu, and no horizontal overflow.
import { KnownDevices } from 'puppeteer-core'
import { apiClient, checker, launchBrowser, login, order, seed, sleep, startTrackstar, storyId } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()
try {
  const sam = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam')
  const api = apiClient(trackstar.base, sam.cookie)
  const project = await seed(api)
  const page = await browser.newPage()
  await page.emulate(KnownDevices['iPhone 13'])
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message))
  page.on('console', (m) => m.type() === 'error' && errors.push('console: ' + m.text()))
  await page.setCookie({ name: 'trackstar_session', value: sam.cookie, url: trackstar.base })
  await page.goto(`${trackstar.base}/#/p/${project.slug}`, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.live.live', { timeout: 10000 })
  await page.waitForSelector('[data-story-id]')
  const tab = async (label) => {
    await page.evaluate((l) => [...document.querySelectorAll('.tabs button')].find((b) => b.textContent.trim().startsWith(l)).click(), label)
    await sleep(300)
  }
  const visibleSections = () => page.$$eval('main [data-section], main section[aria-label]', (els) => els.map((e) => e.dataset.section ?? e.getAttribute('aria-label')))
  const id = (title) => storyId(page, title)

  t.eq('no horizontal page overflow', await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
  t.eq('tab strip present, current selected by default', await page.$eval('.tabs button.on', (e) => e.textContent.trim().startsWith('Current')), true)
  t.eq('only the current panel is rendered', await page.$$eval('main .panel, main section', (els) => els.length), 1)
  t.eq('desktop buttons hidden (project switcher and menu remain)', await page.$$eval('header.topbar button', (els) => els.map((b) => b.textContent.trim())), ['Apollo▾', '☰'])

  await tab('Backlog')
  t.eq('backlog tab shows the backlog', await order(page, 'backlog'), ['Add OAuth support', 'Story drag and drop', 'Velocity chart', 'Email notifications'])
  await tab('Icebox')
  t.eq('icebox tab', await order(page, 'icebox'), ['CSV export', 'Dark mode'])

  // FAB creates in the visible section
  await page.tap('.fab')
  await page.waitForSelector('form[aria-label="New story"]')
  t.eq('FAB preselects the visible section', await page.$eval('form[aria-label="New story"] select[aria-label="Panel"]', (e) => e.value), 'icebox')
  await page.type('form[aria-label="New story"] input', 'Made on a phone')
  await page.keyboard.press('Enter')
  await sleep(400)
  t.eq('story created into icebox', (await order(page, 'icebox'))[0], 'Made on a phone')

  // editor expands under the tapped row, Move to → Backlog
  await page.tap(`[data-story-id="${await id('Made on a phone')}"] .title`)
  await page.waitForSelector('.editor')
  t.eq('editor opens right below its row', await page.$eval('.editor', (e) => e.previousElementSibling?.classList.contains('story')), true)
  const targets = await page.$$eval('.move-targets button', (els) => els.map((b) => b.textContent))
  t.eq('move targets offered', targets, ['Backlog', 'Current iteration'])
  await page.evaluate(() => [...document.querySelectorAll('.move-targets button')].find((b) => b.textContent === 'Backlog').click())
  await sleep(500)
  await page.keyboard.press('Escape')
  await tab('Backlog')
  t.eq('moved to the end of backlog', (await order(page, 'backlog')).at(-1), 'Made on a phone')

  // hold-to-drag reorder by touch: drag the first backlog row below the third
  const first = await page.$(`[data-story-id="${await id('Add OAuth support')}"]`)
  const third = await page.$(`[data-story-id="${await id('Velocity chart')}"]`)
  const a = await first.boundingBox()
  const b = await third.boundingBox()
  const x = a.x + 12, y = a.y + a.height / 2
  await page.touchscreen.touchStart(x, y)
  await sleep(300) // hold past the drag delay
  for (let i = 1; i <= 12; i++) await page.touchscreen.touchMove(x, y + ((b.y + b.height - 2 - y) * i) / 12)
  await sleep(200)
  await page.touchscreen.touchEnd()
  await sleep(600)
  t.eq('touch drag reorders', await order(page, 'backlog'), ['Story drag and drop', 'Velocity chart', 'Add OAuth support', 'Email notifications', 'Made on a phone'])

  // a quick swipe must scroll, not drag: touch-move immediately without holding
  const before = await order(page, 'backlog')
  await page.touchscreen.touchStart(x, y)
  for (let i = 1; i <= 6; i++) await page.touchscreen.touchMove(x, y + i * 20)
  await page.touchscreen.touchEnd()
  await sleep(400)
  t.eq('quick swipe does not reorder', await order(page, 'backlog'), before)

  // menu carries the desktop actions
  await page.tap('.menu > button')
  await sleep(200)
  t.eq('menu has project settings', await page.$$eval('.menu-items button', (els) => els.map((b) => b.textContent.trim())), ['Project settings…', 'Account…', 'Users…', 'Sign out'])
  await page.keyboard.press('Escape')

  // the "combine icebox and backlog" project setting drops the Icebox tab (the board picks it up live)
  await tab('Icebox')
  await api('PATCH', `/api/projects/${project.id}`, { combine_icebox_backlog: true })
  await sleep(800)
  t.eq('icebox tab gone', await page.$$eval('.tabs button', (els) => els.map((b) => b.textContent.trim().replace(/\d+$/, ''))), ['Backlog', 'Current', 'Epics', 'Done', 'Trash'])
  t.eq('backlog tab now shows both lists', await page.$$eval('main [data-section]', (els) => els.map((e) => e.dataset.section)), ['backlog', 'icebox'])
  await api('PATCH', `/api/projects/${project.id}`, { combine_icebox_backlog: false })
  await sleep(800)
  t.eq('icebox tab is back', await page.$eval('.tabs button', (b) => b.textContent.trim().startsWith('Icebox')), true)

  await tab('Done')
  t.eq('done tab renders the chart panel', await page.$eval('section[aria-label="Done"]', (e) => e !== null), true)
  await tab('Epics')
  t.eq('epics tab', await page.$eval('section[aria-label="Epics"]', (e) => e !== null), true)
  t.eq('still no horizontal overflow', await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
  t.eq('no floating + on top of the epics form', await page.$('.fab'), null)

  // Nothing pans, zooms or hides unless the user means it to. Checked on the
  // open dialog (if any) and the page: no sideways scrolling, nothing spilling
  // out of a dialog, and no field small enough to make iOS zoom on focus.
  const problems = () =>
    page.evaluate(() => {
      const out = []
      const name = (el) => `${el.tagName.toLowerCase()}.${String(el.className).split(' ')[0]}`
      if (document.documentElement.scrollWidth > window.innerWidth) out.push('page wider than the screen')
      for (const el of document.querySelectorAll('body *')) {
        const scrolls = /auto|scroll/.test(getComputedStyle(el).overflowX) && el.scrollWidth > el.clientWidth + 1
        // The tab strip and the users table scroll sideways on purpose.
        if (scrolls && !el.matches('.tabs, .table-scroll')) out.push(`${name(el)} scrolls sideways`)
      }
      const modal = document.querySelector('.backdrop .modal')
      if (modal) {
        const m = modal.getBoundingClientRect()
        if (m.left < 0 || m.right > window.innerWidth) out.push('dialog wider than the screen')
        for (const el of modal.querySelectorAll('*')) {
          const b = el.getBoundingClientRect()
          if (b.width && (b.left < m.left - 1 || b.right > m.right + 1) && !el.closest('.table-scroll')) {
            out.push(`${name(el)} spills out of the dialog`)
            break
          }
        }
      }
      for (const el of document.querySelectorAll('input:not([type=checkbox]):not([type=radio]), select, textarea')) {
        if (el.getBoundingClientRect().width && parseFloat(getComputedStyle(el).fontSize) < 16) {
          out.push(`${name(el)} is under 16px`)
          break
        }
      }
      return out
    })
  // The dialog's last button can be brought fully on screen.
  const reachable = (label) =>
    page.evaluate((l) => {
      const b = [...document.querySelectorAll('.backdrop .modal button')].find((e) => e.textContent.trim() === l)
      b.scrollIntoView({ block: 'end' })
      const r = b.getBoundingClientRect()
      return r.top >= 0 && r.bottom <= window.innerHeight && r.left >= 0 && r.right <= window.innerWidth
    }, label)
  const openFromMenu = async (item) => {
    await page.tap('.menu > button')
    await sleep(200)
    await page.evaluate((i) => [...document.querySelectorAll('.menu-items button')].find((b) => b.textContent.trim() === i).click(), item)
    await page.waitForSelector('.backdrop .modal')
    await sleep(200)
  }
  const closeDialog = async () => {
    await page.keyboard.press('Escape')
    await page.waitForSelector('.backdrop', { hidden: true })
  }
  const screen = async (width, height) => {
    await page.emulate({ viewport: { width, height, isMobile: true, hasTouch: true, deviceScaleFactor: 2 }, userAgent: KnownDevices['iPhone 13'].userAgent })
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.waitForSelector('[data-story-id]')
  }

  await tab('Current')
  t.eq('board is clean on a phone', await problems(), [])
  await page.tap(`[data-story-id="${await id('Set up CI')}"] .title`)
  await page.waitForSelector('.editor')
  t.eq('story editor is clean', await problems(), [])
  await page.keyboard.press('Escape')
  for (const [item, last] of [['Account…', 'Save'], ['Project settings…', 'Save'], ['Users…', 'Close']]) {
    await openFromMenu(item)
    t.eq(`${item} is clean, ${last} reachable`, [await problems(), await reachable(last)], [[], true])
    await closeDialog()
  }

  // The narrowest phone still sold.
  await screen(320, 568)
  t.eq('board is clean at 320px', await problems(), [])
  await page.tap('.fab')
  await page.waitForSelector('form[aria-label="New story"]')
  t.eq('new story is clean at 320px, Save reachable', [await problems(), await reachable('Save')], [[], true])
  await closeDialog()
  await openFromMenu('Account…')
  t.eq('account is clean at 320px, Save reachable', [await problems(), await reachable('Save')], [[], true])
  await closeDialog()

  // A phone on its side: still the phone layout, but dialogs are taller than the screen.
  await screen(844, 390)
  for (const item of ['Account…', 'Project settings…']) {
    await openFromMenu(item)
    t.eq(`${item} in landscape: clean, Save reachable`, [await problems(), await reachable('Save')], [[], true])
    await closeDialog()
  }
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
