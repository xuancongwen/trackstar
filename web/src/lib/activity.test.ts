import { describe, expect, it } from 'vitest'
import { feedPhrase, foldCreated, withoutEchoes } from './activity'
import { timeAgo } from './format'
import type { FeedEntry } from './types'

const entry = (over: Partial<FeedEntry>): FeedEntry => ({
  id: 1,
  kind: 'state',
  project_id: 1,
  story_id: 7,
  story_title: 'Add OAuth support',
  user_id: 1,
  old_value: '',
  new_value: '',
  created_at: '2026-01-05T12:00:00Z',
  ...over,
})
const name = (id: string | number) => (Number(id) === 2 ? 'Kim' : `user ${id}`)
const say = (over: Partial<FeedEntry>) => {
  const p = feedPhrase(entry(over), name)
  return `${p.verb} X${p.suffix}`
}

describe('feedPhrase', () => {
  it('reads as a sentence around the story title', () => {
    expect(say({ kind: 'created', new_value: 'backlog' })).toBe('created X in the backlog')
    expect(say({ kind: 'created', new_value: 'current' })).toBe('created X in the current iteration')
    expect(say({ kind: 'state', old_value: 'unstarted', new_value: 'started' })).toBe('started X')
    expect(say({ kind: 'state', old_value: 'delivered', new_value: 'accepted' })).toBe('accepted X')
    expect(say({ kind: 'state', old_value: 'rejected', new_value: 'started' })).toBe('restarted X')
    expect(say({ kind: 'state', old_value: 'started', new_value: 'unstarted' })).toBe('moved X from started back to unstarted')
    expect(say({ kind: 'estimate', new_value: '1' })).toBe('estimated X at 1 point')
    expect(say({ kind: 'estimate', old_value: '1', new_value: '3' })).toBe('estimated X at 3 points')
    expect(say({ kind: 'estimate', old_value: '3', new_value: '' })).toBe('removed the estimate from X')
    expect(say({ kind: 'owner', new_value: '2' })).toBe('assigned X to Kim')
    expect(say({ kind: 'owner', new_value: '1' })).toBe('took X')
    expect(say({ kind: 'owner', old_value: '2', new_value: '' })).toBe('unassigned X')
    expect(say({ kind: 'moved', old_value: 'icebox', new_value: 'backlog' })).toBe('moved X from icebox to backlog')
    expect(say({ kind: 'deleted' })).toBe('deleted X')
    expect(say({ kind: 'comment', body: 'hello' })).toBe('commented on X')
    expect(say({ kind: 'something-new' })).toBe('updated X')
  })
})

describe('withoutEchoes', () => {
  it('drops the self-assignment that comes with starting a story', () => {
    const started = entry({ id: 3, kind: 'state', new_value: 'started' })
    const selfAssigned = entry({ id: 2, kind: 'owner', new_value: '1' })
    expect(withoutEchoes([started, selfAssigned])).toEqual([started])
  })
  it('keeps real assignments', () => {
    const started = entry({ id: 3, kind: 'state', new_value: 'started' })
    const toKim = entry({ id: 2, kind: 'owner', new_value: '2' })
    const laterSelf = entry({ id: 4, kind: 'owner', new_value: '1', created_at: '2026-01-05T13:00:00Z' })
    const otherStory = entry({ id: 5, kind: 'owner', new_value: '1', story_id: 8 })
    expect(withoutEchoes([started, toKim, laterSelf, otherStory])).toEqual([started, toKim, laterSelf, otherStory])
  })
})

describe('foldCreated', () => {
  it('folds a run of creations by one person in one project', () => {
    const created = (id: number, over: Partial<FeedEntry> = {}) => entry({ id, kind: 'created', story_id: id, ...over })
    const lines = foldCreated([
      created(9),
      created(8),
      created(7),
      entry({ id: 6, kind: 'state', new_value: 'started' }),
      created(5),
      created(4, { user_id: 2 }),
      created(3, { user_id: 2, project_id: 2 }),
    ])
    expect(lines.map((l) => [l.id, l.stories])).toEqual([
      [9, 3],
      [6, 1],
      [5, 1],
      [4, 1],
      [3, 1],
    ])
  })
})

describe('timeAgo', () => {
  const now = new Date('2026-01-05T12:00:00Z')
  const ago = (seconds: number) => timeAgo(new Date(now.getTime() - seconds * 1000), now)
  it('scales with the distance', () => {
    expect(ago(0)).toBe('just now')
    expect(ago(59)).toBe('just now')
    expect(ago(60)).toBe('1m ago')
    expect(ago(3599)).toBe('59m ago')
    expect(ago(3600)).toBe('1h ago')
    expect(ago(86399)).toBe('23h ago')
    expect(ago(86400)).toBe('1d ago')
    expect(ago(29 * 86400)).toBe('29d ago')
    expect(ago(31 * 86400)).not.toMatch(/ago/)
  })
  it('never reports the future', () => {
    expect(ago(-120)).toBe('just now')
  })
})
