// Two browsers on one project: changes propagate live and never disturb a drag.
import { apiClient, checker, drag, dragOnto, launchBrowser, login, openBoard, order, seed, sleep, startTrackstar, storyId } from './harness.mjs'

const t = checker()
const errors = []
const trackstar = await startTrackstar()
const browser = await launchBrowser()
try {
  const sam = await login(trackstar.base, 'sam@example.com', 'e2e-password-1', 'Sam')
  const kim = await login(trackstar.base, 'kim@example.com', 'e2e-password-1', 'Kim')
  const api = apiClient(trackstar.base, sam.cookie)
  const project = await seed(api)
  await api('PUT', `/api/projects/${project.id}/members/${kim.user.id}`, { role: 'member' }) // sam owns it; kim must be let in
  const A = await openBoard(browser, trackstar.base, sam.cookie, project.slug, errors)
  // Own context for B: pages in one context share cookies, so B's session would replace A's.
  const B = await openBoard(await browser.createBrowserContext(), trackstar.base, kim.cookie, project.slug, errors)
  const sel = async (page, title) => `[data-story-id="${await storyId(page, title)}"]`

  async function waitFor(page, section, want, limit = 3000) {
    const t0 = Date.now()
    while (Date.now() - t0 < limit) {
      if (JSON.stringify(await order(page, section)) === JSON.stringify(want)) return Date.now() - t0
      await sleep(25)
    }
    return -1
  }

  let fetchesB = 0
  B.on('request', (r) => r.url().includes('/api/projects/1/stories') && fetchesB++)
  await dragOnto(A, await sel(A, 'Add OAuth support'), await sel(A, 'Velocity chart'))
  const ms = await waitFor(B, 'backlog', await order(A, 'backlog'))
  t.eq('B follows a drag in A', ms >= 0, true)
  console.log(`     (synced in ${ms} ms, ${fetchesB} list fetch)`)
  t.eq('B fetched the list once', fetchesB, 1)
  t.eq('changed row flashes in B', await B.$eval(await sel(B, 'Add OAuth support'), (e) => e.classList.contains('recent')), true)

  let fetchesA = 0
  A.on('request', (r) => r.url().includes('/api/projects/1/stories') && fetchesA++)
  await A.click(`${await sel(A, 'Fix login redirect loop')} button`); await sleep(600)
  t.eq('A ignores the echo of its own change', fetchesA, 0)
  t.eq('B sees the started bug', await B.$eval(await sel(B, 'Fix login redirect loop'), (e) => e.classList.contains('started')), true)

  // B is mid-drag while A finishes a story: B is not disturbed, syncs after the drop.
  const b = await (await B.$(await sel(B, 'CSV export'))).boundingBox()
  await B.mouse.move(b.x + 30, b.y + 10); await B.mouse.down(); await B.mouse.move(b.x + 40, b.y + 60, { steps: 5 })
  await A.click(`${await sel(A, 'Set up CI')} button`); await sleep(500)
  t.eq('B not updated mid-drag', await B.$eval(await sel(B, 'Set up CI'), (e) => e.classList.contains('finished')), false)
  await B.mouse.move(b.x + 40, b.y + 20, { steps: 5 }); await B.mouse.up(); await sleep(700)
  t.eq('B catches up after the drop', await B.$eval(await sel(B, 'Set up CI'), (e) => e.classList.contains('finished')), true)

  await A.keyboard.press('c'); await A.waitForSelector('form[aria-label="New story"]')
  await A.keyboard.type('Live-created story'); await A.keyboard.press('Enter')
  await B.waitForFunction(() => [...document.querySelectorAll('.title')].some((e) => e.textContent === 'Live-created story'), { timeout: 3000 })
  t.eq('story created in A appears in B', true, true)

  // Deleting in A removes from B; A's undo brings it back in B.
  await A.click(`${await sel(A, 'Live-created story')} .title`); await A.waitForSelector('.drawer')
  await A.click('.drawer footer button.danger'); await A.click('.drawer footer button.danger'); await sleep(600)
  t.eq('deletion propagates', (await order(B, 'backlog')).includes('Live-created story'), false)
  await A.click('.undo button'); await sleep(600)
  t.eq('undo propagates', (await order(B, 'backlog')).includes('Live-created story'), true)
} finally {
  await browser.close()
  trackstar.stop()
}
t.done(errors)
