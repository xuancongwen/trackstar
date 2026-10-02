// Projects page: what each project is up to, on a desktop and on a phone.
import { KnownDevices } from 'puppeteer-core'
import { apiClient, checker, launchBrowser, login, seed, startTrackstar } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()
try {
  // The first account is the administrator and sees every project; Sam and
  // Kim are ordinary users, so visibility is part of what is checked.
  await login(trackstar.base, 'root@example.com', 'e2e-password-1', 'Root')
  const sam = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam')
  const kim = await login(trackstar.base, 'kim@example.com', 'e2e-password-1', 'Kim')
  const api = apiClient(trackstar.base, sam.cookie)
  const project = await seed(api) // 8 stories created in Apollo, "Set up CI" started
  await api('POST', '/api/projects', { name: 'Quiet' })
  await api('POST', '/api/projects', { name: 'Aardvark' }) // first by name, but nothing happens in it
  const hidden = await apiClient(trackstar.base, kim.cookie)('POST', '/api/projects', { name: "Kim's own" })
  await apiClient(trackstar.base, kim.cookie)('POST', `/api/projects/${hidden.id}/stories`, { title: 'Not for Sam' })

  const stories = await api('GET', `/api/projects/${project.id}/stories`)
  const id = (title) => stories.find((s) => s.title === title).id
  await api('PATCH', `/api/stories/${id('Add OAuth support')}`, { state: 'started' })
  await api('PATCH', `/api/stories/${id('Set up CI')}`, { state: 'finished' })
  await api('PATCH', `/api/stories/${id('Set up CI')}`, { state: 'delivered' })
  await api('PATCH', `/api/stories/${id('Velocity chart')}`, { owner_id: kim.user.id })
  await api('POST', `/api/stories/${id('Add OAuth support')}/comments`, { body: 'Check the wording.' })

  const open = async (device) => {
    const page = await browser.newPage()
    if (device) await page.emulate(device)
    else await page.setViewport({ width: 1200, height: 900 })
    page.on('pageerror', (e) => errors.push('pageerror: ' + e.message))
    page.on('console', (m) => m.type() === 'error' && errors.push('console: ' + m.text()))
    await page.setCookie({ name: 'trackstar_session', value: sam.cookie, url: trackstar.base })
    await page.goto(trackstar.base + '/', { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('main > ul li .summary')
    return page
  }

  const page = await open(null)
  const rows = await page.$$eval('main > ul li a', (els) => els.map((a) => [a.querySelector('strong').textContent, a.querySelector('.summary').textContent, a.getAttribute('href')]))
  t.eq('each project says what is going on in it, the busiest first', rows, [
    ['Apollo', '1 in progress · 1 to accept · just now', '#/p/apollo'],
    ['Aardvark', 'no activity yet', '#/p/aardvark'],
    ['Quiet', 'no activity yet', '#/p/quiet'],
  ])
  // 8 created, two stories started (each also assigns its owner), then
  // finished, delivered, assigned and commented on.
  const sparks = await page.$$eval('main > ul li svg.spark', (els) =>
    els.map((e) => [e.getAttribute('aria-label'), e.querySelectorAll('rect:not(.hit)').length, e.querySelectorAll('rect:not(.hit):not(.none)').length, e.querySelector('rect.hit:last-of-type title').textContent.replace(/^.*: /, '')]),
  )
  t.eq('a project with stories charts its last 30 days, today at the right', sparks, [['16 changes in the last 30 days', 30, 1, '16 changes']])
  t.eq('no activity feed', await page.$('section[aria-label="Recent activity"]'), null)
  t.eq('nothing from a project Sam cannot see', await page.$eval('main', (e) => e.innerText.includes("Kim's own") || e.innerText.includes('Not for Sam')), false)

  // The order is the reader's choice, and the browser remembers it.
  const names = () => page.$$eval('main > ul li a strong', (els) => els.map((e) => e.textContent))
  const sort = 'select[aria-label="Sort projects"]'
  t.eq('sorted by activity unless asked otherwise', await page.$eval(sort, (e) => e.value), 'activity')
  await page.select(sort, 'name')
  t.eq('by name, A to Z', await names(), ['Aardvark', 'Apollo', 'Quiet'])
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('main > ul li .summary')
  t.eq('the choice survives a reload', [await page.$eval(sort, (e) => e.value), await names()], ['name', ['Aardvark', 'Apollo', 'Quiet']])
  await page.select(sort, 'activity')
  t.eq('and back to the busiest first', await names(), ['Apollo', 'Aardvark', 'Quiet'])

  // Search narrows the list as you type; Enter opens a project only when it is the one match.
  await page.keyboard.press('/')
  t.eq('/ focuses the search box', await page.evaluate(() => document.activeElement?.getAttribute('aria-label')), 'Search projects')
  await page.keyboard.type('A')
  t.eq('typing filters by name, ignoring case', await names(), ['Apollo', 'Aardvark'])
  await page.keyboard.press('Enter')
  t.eq('Enter does nothing while several projects match', await page.evaluate(() => location.hash), '')
  await page.keyboard.type('zz')
  t.eq('nothing matches', [await names(), await page.$eval('main [role="status"]', (e) => e.textContent)], [[], 'No project matches “Azz”.'])
  await page.keyboard.press('Enter')
  t.eq('Enter does nothing when nothing matches', await page.evaluate(() => location.hash), '')
  await page.keyboard.press('Escape')
  t.eq('Esc clears the search', [await page.$eval('input.search', (e) => e.value), (await names()).length], ['', 3])
  await page.keyboard.press('/')
  await page.keyboard.type('uie')
  t.eq('one project left', await names(), ['Quiet'])
  await page.keyboard.press('Enter')
  // Until the board has replaced the page; going back sooner would find the search still filled in.
  await page.waitForFunction(() => location.hash === '#/p/quiet' && !document.querySelector('[aria-label="Search projects"]'))
  t.eq('Enter opens the only match', await page.evaluate(() => location.hash), '#/p/quiet')
  await page.goto(trackstar.base + '/#/', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('main > ul li .summary')

  // The account menu is on this page too, not only on a board.
  const menuItems = async (p) => {
    await p.click('header .menu > button')
    return p.$$eval('header [role="menuitem"]', (els) => els.map((e) => e.textContent))
  }
  t.eq('the menu offers the account and signing out', await menuItems(page), ['Account…', 'Sign out'])
  await page.keyboard.press('Escape')
  t.eq('Esc closes the menu', await page.$('header .menu-items'), null)
  await menuItems(page)
  await page.click('header [role="menuitem"]')
  await page.waitForSelector('form[aria-label="Account"]')
  const nameInput = 'form[aria-label="Account"] label.field input'
  await page.$eval(nameInput, (e) => (e.focus(), e.select()))
  await page.type(nameInput, 'Samantha')
  await page.click('form[aria-label="Account"] button.primary')
  await page.waitForFunction(() => document.querySelector('header .who').textContent === 'Samantha')
  // A personal default for the projects you create, set in the same dialog.
  await page.evaluate(() => [...document.querySelectorAll('form[aria-label="Account"] label.check')].find((l) => l.textContent.includes('Combine icebox')).querySelector('input').click())
  await page.click('form[aria-label="Account"] button.primary')
  await page.waitForFunction(() => document.querySelector('form[aria-label="Account"] [role="status"]')?.textContent === 'Saved.')
  t.eq('the preference is stored on the account', (await api('GET', '/api/me')).default_combine_icebox_backlog, true)
  t.eq('a project created afterwards starts combined', (await api('POST', '/api/projects', { name: 'Combined' })).combine_icebox_backlog, true)
  t.eq('existing projects keep their setting', (await api('GET', '/api/projects/apollo')).combine_icebox_backlog, false)
  await api('DELETE', '/api/projects/combined')
  await page.keyboard.press('Escape')
  t.eq('Esc closes the account dialog', await page.$('form[aria-label="Account"]'), null)
  await api('PATCH', '/api/me', { display_name: 'Sam' })

  await page.click('main > ul li a')
  await page.waitForSelector('[data-story-id]')
  t.eq('a project row opens its board', await page.evaluate(() => location.hash), '#/p/apollo')
  await page.close()

  for (const device of ['iPhone 13', 'iPhone SE']) {
    const phone = await open(KnownDevices[device])
    const fit = await phone.evaluate(() => {
      const spills = [...document.querySelectorAll('main *')].filter((e) => {
        const b = e.getBoundingClientRect()
        return b.width > 0 && (b.right > window.innerWidth + 1 || b.left < -1)
      })
      const small = [...document.querySelectorAll('input')].filter((e) => parseFloat(getComputedStyle(e).fontSize) < 16)
      return [document.documentElement.scrollWidth <= window.innerWidth, spills.length, small.length]
    })
    t.eq(`${device}: no sideways scroll, nothing off screen, no zooming input`, fit, [true, 0, 0])
    await phone.click('header .menu > button')
    await phone.click('header [role="menuitem"]')
    await phone.waitForSelector('form[aria-label="Account"]')
    const reach = await phone.$eval('form[aria-label="Account"]', (f) => {
      const b = f.getBoundingClientRect()
      return [b.left >= 0 && b.right <= window.innerWidth, f.scrollWidth <= f.clientWidth]
    })
    t.eq(`${device}: the account dialog opens and fits`, reach, [true, true])
    await phone.close()
  }
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
