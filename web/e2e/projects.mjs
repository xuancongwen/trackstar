// Projects page: what each project is up to, and the recent-activity feed,
// on a desktop and on a phone.
import { KnownDevices } from 'puppeteer-core'
import { apiClient, checker, launchBrowser, login, seed, sleep, startTrackstar } from './harness.mjs'

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
  const hidden = await apiClient(trackstar.base, kim.cookie)('POST', '/api/projects', { name: "Kim's own" })
  await apiClient(trackstar.base, kim.cookie)('POST', `/api/projects/${hidden.id}/stories`, { title: 'Not for Sam' })

  const stories = await api('GET', `/api/projects/${project.id}/stories`)
  const id = (title) => stories.find((s) => s.title === title).id
  // The feed's clock has one-second resolution; space the changes out so
  // their order is the order they were made in.
  const step = async (fn) => {
    await sleep(1100)
    await fn()
  }
  await step(() => api('PATCH', `/api/stories/${id('Add OAuth support')}`, { state: 'started' }))
  await step(() => api('PATCH', `/api/stories/${id('Set up CI')}`, { state: 'finished' }))
  await step(() => api('PATCH', `/api/stories/${id('Set up CI')}`, { state: 'delivered' }))
  await step(() => api('PATCH', `/api/stories/${id('Velocity chart')}`, { owner_id: kim.user.id }))
  const longWord = 'https://example.com/' + 'a-very-long-unbroken-url/'.repeat(8)
  await step(() => api('POST', `/api/stories/${id('Add OAuth support')}/comments`, { body: `Check the wording. ${longWord}` }))

  const open = async (device) => {
    const page = await browser.newPage()
    if (device) await page.emulate(device)
    else await page.setViewport({ width: 1200, height: 900 })
    page.on('pageerror', (e) => errors.push('pageerror: ' + e.message))
    page.on('console', (m) => m.type() === 'error' && errors.push('console: ' + m.text()))
    await page.setCookie({ name: 'trackstar_session', value: sam.cookie, url: trackstar.base })
    await page.goto(trackstar.base + '/', { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('section[aria-label="Recent activity"] li')
    return page
  }
  const text = (el) => el.innerText.replace(/\s+/g, ' ').trim()

  const page = await open(null)
  const rows = await page.$$eval('main > ul li a', (els) => els.map((a) => [a.querySelector('strong').textContent, a.querySelector('.summary').textContent, a.getAttribute('href')]))
  t.eq('each project says what is going on in it', rows, [
    ['Apollo', '1 in progress · 1 to accept · just now', '#/p/apollo'],
    ['Quiet', 'no activity yet', '#/p/quiet'],
  ])
  const feed = await page.$$eval('.feed li a', (els) => els.map((a) => [...a.querySelectorAll('.what, .where')].map((e) => e.innerText.replace(/\s+/g, ' ').trim())))
  t.eq('feed is newest first and reads as sentences', feed, [
    ['Sam commented on Add OAuth support', 'Apollo · just now'],
    ['Sam assigned Velocity chart to Kim', 'Apollo · just now'],
    ['Sam delivered Set up CI', 'Apollo · just now'],
    ['Sam finished Set up CI', 'Apollo · just now'],
    ['Sam started Add OAuth support', 'Apollo · just now'],
    // seed: started "Set up CI" (the self-assignment that came with it is
    // not repeated), after creating eight stories in one go
    ['Sam started Set up CI', 'Apollo · just now'],
    ['Sam created 8 stories', 'Apollo · just now'],
  ])
  t.eq('a comment shows what was said', await page.$eval('.feed .quote', (e) => e.textContent.startsWith('Check the wording.') && e.textContent.endsWith('…')), true)
  t.eq('nothing from a project Sam cannot see', await page.$eval('main', (e) => e.innerText.includes("Kim's own") || e.innerText.includes('Not for Sam')), false)
  await page.click('.feed li a')
  await page.waitForSelector('[data-story-id]')
  t.eq('a feed entry opens its project', await page.evaluate(() => location.hash), '#/p/apollo')
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
    await phone.close()
  }
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
