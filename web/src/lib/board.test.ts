import { describe, expect, it } from 'vitest'
import {
  acceptedThisIteration,
  applyMove,
  canDrop,
  moveRequest,
  nextActions,
  projectBacklog,
  sectionStories,
} from './board'
import { makeStory } from './testing'
import type { Story } from './types'

const ids = (stories: Story[]) => stories.map((s) => s.id)

describe('sectionStories', () => {
  it('orders by position and leaves accepted stories out', () => {
    const stories = [
      makeStory({ id: 1, section: 'current', state: 'started', position: 300 }),
      makeStory({ id: 2, section: 'current', state: 'accepted', position: 100, accepted_at: '2026-01-06T00:00:00Z' }),
      makeStory({ id: 3, section: 'current', state: 'unstarted', position: 200 }),
      makeStory({ id: 4, section: 'backlog', position: 50 }),
    ]
    expect(ids(sectionStories(stories, 'current'))).toEqual([3, 1])
    expect(ids(acceptedThisIteration(stories))).toEqual([2])
    expect(ids(sectionStories(stories, 'icebox'))).toEqual([])
  })
})

describe('moveRequest', () => {
  it('names both neighbours for a drop in the middle', () => {
    expect(moveRequest([1, 2, 3], 9, 'backlog', 1)).toEqual({ section: 'backlog', prev_id: 1, next_id: 2 })
  })

  it('handles top, bottom and empty lists', () => {
    expect(moveRequest([1, 2], 9, 'icebox', 0)).toEqual({ section: 'icebox', prev_id: null, next_id: 1 })
    expect(moveRequest([1, 2], 9, 'icebox', 2)).toEqual({ section: 'icebox', prev_id: 2, next_id: null })
    expect(moveRequest([], 9, 'current', 0)).toEqual({ section: 'current', prev_id: null, next_id: null })
  })

  it('ignores the dragged story when reordering within a list', () => {
    // [1, 2, 3] → drag 1 to the end: Sortable reports index 2.
    expect(moveRequest([1, 2, 3], 1, 'backlog', 2)).toEqual({ section: 'backlog', prev_id: 3, next_id: null })
    // drag 3 to the top
    expect(moveRequest([1, 2, 3], 3, 'backlog', 0)).toEqual({ section: 'backlog', prev_id: null, next_id: 1 })
    // drag 1 down one slot
    expect(moveRequest([1, 2, 3], 1, 'backlog', 1)).toEqual({ section: 'backlog', prev_id: 2, next_id: 3 })
  })

  it('clamps an out-of-range index', () => {
    expect(moveRequest([1, 2], 9, 'backlog', 99)).toEqual({ section: 'backlog', prev_id: 2, next_id: null })
  })
})

describe('applyMove', () => {
  const stories = () => [
    makeStory({ id: 1, position: 1000 }),
    makeStory({ id: 2, position: 2000 }),
    makeStory({ id: 3, position: 3000 }),
    makeStory({ id: 4, section: 'icebox', state: 'icebox', position: 1000 }),
  ]

  it('reorders within a section without touching state', () => {
    const next = applyMove(stories(), 3, 'backlog', 1)
    expect(ids(sectionStories(next, 'backlog'))).toEqual([1, 3, 2])
    expect(next.find((s) => s.id === 3)!.state).toBe('backlog')
  })

  it('moves across sections and updates state and section together', () => {
    const next = applyMove(stories(), 4, 'backlog', 3)
    expect(ids(sectionStories(next, 'backlog'))).toEqual([1, 2, 3, 4])
    expect(ids(sectionStories(next, 'icebox'))).toEqual([])
    expect(next.find((s) => s.id === 4)).toMatchObject({ state: 'backlog', section: 'backlog' })

    const current = applyMove(next, 4, 'current', 0)
    expect(current.find((s) => s.id === 4)).toMatchObject({ state: 'unstarted', section: 'current' })
  })

  it('does not mutate its input and refuses illegal drops', () => {
    const before = stories()
    const snapshot = JSON.stringify(before)
    applyMove(before, 3, 'backlog', 0)
    expect(JSON.stringify(before)).toBe(snapshot)

    const wip = [makeStory({ id: 7, section: 'current', state: 'started' })]
    expect(applyMove(wip, 7, 'backlog', 0)).toBe(wip)
  })
})

describe('canDrop', () => {
  it('keeps work in progress in the current iteration and freezes accepted stories', () => {
    expect(canDrop(makeStory({ state: 'icebox', section: 'icebox' }), 'current')).toBe(true)
    expect(canDrop(makeStory({ state: 'unstarted', section: 'current' }), 'icebox')).toBe(true)
    for (const state of ['started', 'finished', 'delivered', 'rejected'] as const) {
      expect(canDrop(makeStory({ state, section: 'current' }), 'backlog')).toBe(false)
      expect(canDrop(makeStory({ state, section: 'current' }), 'current')).toBe(true)
    }
    expect(canDrop(makeStory({ state: 'accepted', section: 'current' }), 'current')).toBe(false)
  })
})

describe('nextActions', () => {
  it('follows the Pivotal workflow', () => {
    const labels = (state: Story['state']) => nextActions(makeStory({ state })).map((a) => `${a.label}→${a.state}`)
    expect(labels('icebox')).toEqual(['Start→started'])
    expect(labels('backlog')).toEqual(['Start→started'])
    expect(labels('unstarted')).toEqual(['Start→started'])
    expect(labels('started')).toEqual(['Finish→finished'])
    expect(labels('finished')).toEqual(['Deliver→delivered'])
    expect(labels('delivered')).toEqual(['Accept→accepted', 'Reject→rejected'])
    expect(labels('rejected')).toEqual(['Restart→started'])
    expect(labels('accepted')).toEqual([])
  })
})

describe('projectBacklog', () => {
  const current = { number: 12, end_at: '2026-03-30T00:00:00Z' }
  const describeRows = (rows: ReturnType<typeof projectBacklog>) =>
    rows.map((r) => (r.kind === 'marker' ? `#${r.number}(${r.points})` : r.story.title))

  it('starts a new iteration when the next story would exceed the velocity', () => {
    const backlog = [
      makeStory({ title: 'A', estimate: 3 }),
      makeStory({ title: 'B', estimate: 5 }),
      makeStory({ title: 'C', estimate: 2 }),
      makeStory({ title: 'D', estimate: 3 }),
    ]
    expect(describeRows(projectBacklog(backlog, 10, current, 7))).toEqual(['#13(10)', 'A', 'B', 'C', '#14(3)', 'D'])
  })

  it('dates the projected iterations from the end of the current one', () => {
    const backlog = [makeStory({ estimate: 8 }), makeStory({ estimate: 8 })]
    const markers = projectBacklog(backlog, 8, current, 14).filter((r) => r.kind === 'marker')
    expect(markers.map((m) => m.startAt.toISOString().slice(0, 10))).toEqual(['2026-03-30', '2026-04-13'])
  })

  it('gives an oversized story its own iteration and ignores unpointed work', () => {
    const backlog = [
      makeStory({ title: 'Small', estimate: 1 }),
      makeStory({ title: 'Huge', estimate: 8 }),
      makeStory({ title: 'Bug', type: 'bug', estimate: null }),
      makeStory({ title: 'Chore', type: 'chore', estimate: 3 }),
      makeStory({ title: 'Next', estimate: 2 }),
    ]
    expect(describeRows(projectBacklog(backlog, 5, current, 7))).toEqual([
      '#13(1)', 'Small', '#14(8)', 'Huge', 'Bug', 'Chore', '#15(2)', 'Next',
    ])
  })

  it('returns nothing for an empty backlog and survives zero velocity', () => {
    expect(projectBacklog([], 10, current, 7)).toEqual([])
    const rows = projectBacklog([makeStory({ estimate: 1 }), makeStory({ estimate: 1 })], 0, current, 7)
    expect(rows.filter((r) => r.kind === 'marker')).toHaveLength(2)
  })
})
