import { describe, expect, it } from 'vitest'
import { blockingSet, matches, parseQuery } from './filter'
import { makeStory } from './testing'
import type { User } from './types'

const sam: User = { id: 1, email: 'sam@example.com', display_name: 'Sam Wen', is_admin: true, is_active: true }
const kim: User = { id: 2, email: 'kim@example.com', display_name: 'Kim Lee', is_admin: false, is_active: true }
const ctx = { me: sam, users: new Map([[1, sam], [2, kim]]) }
const run = (query: string, story: Parameters<typeof makeStory>[0]) => matches(makeStory(story), parseQuery(query), ctx)

describe('parseQuery', () => {
  it('splits keyed terms, quoted phrases and negations', () => {
    expect(parseQuery('owner:me  -type:chore "add oauth" label:auth misc')).toEqual([
      { key: 'owner', value: 'me', negate: false },
      { key: 'type', value: 'chore', negate: true },
      { key: '', value: 'add oauth', negate: false },
      { key: 'label', value: 'auth', negate: false },
      { key: '', value: 'misc', negate: false },
    ])
    expect(parseQuery('   ')).toEqual([])
  })
})

describe('matches', () => {
  it('free text searches title and description', () => {
    expect(run('oauth', { title: 'Add OAuth support' })).toBe(true)
    expect(run('oauth', { title: 'x', description: 'uses OAuth 2' })).toBe(true)
    expect(run('"add oauth"', { title: 'Add OAuth support' })).toBe(true)
    expect(run('oauth sso', { title: 'Add OAuth support' })).toBe(false)
  })

  it('matches people by me, prefix, part of the name, or none', () => {
    expect(run('owner:me', { owner_id: 1 })).toBe(true)
    expect(run('owner:me', { owner_id: 2 })).toBe(false)
    expect(run('owner:kim', { owner_id: 2 })).toBe(true)
    expect(run('owner:lee', { owner_id: 2 })).toBe(true)
    expect(run('owner:none', { owner_id: null })).toBe(true)
    expect(run('requester:sam', { requester_id: 1 })).toBe(true)
  })

  it('handles type, state, estimate, label and is:/has:', () => {
    expect(run('type:bug', { type: 'bug' })).toBe(true)
    expect(run('state:started', { state: 'started', section: 'current' })).toBe(true)
    expect(run('state:open', { state: 'accepted' })).toBe(false)
    expect(run('estimate:3', { estimate: 3 })).toBe(true)
    expect(run('estimate:none', { estimate: null })).toBe(true)
    expect(run('label:au', { labels: ['auth'] })).toBe(true)
    expect(run('epic:auth', { labels: ['auth'] })).toBe(true)
    expect(run('is:blocked', { blocked: true })).toBe(true)
    expect(run('is:unestimated', { estimate: null })).toBe(true)
    expect(run('has:tasks', { task_count: 2 })).toBe(true)
    expect(run('id:#7', { id: 7 })).toBe(true)
  })

  it('negates and ANDs terms', () => {
    expect(run('owner:me -state:accepted', { owner_id: 1, state: 'started', section: 'current' })).toBe(true)
    expect(run('owner:me -state:accepted', { owner_id: 1, state: 'accepted', section: 'current' })).toBe(false)
    expect(run('type:bug label:auth', { type: 'bug', labels: [] })).toBe(false)
  })

  it('knows which stories are blocking others', () => {
    const stories = [makeStory({ id: 1, blocked_by: [2, 3] }), makeStory({ id: 2 }), makeStory({ id: 3 }), makeStory({ id: 4 })]
    const blocking = blockingSet(stories)
    expect([...blocking].sort()).toEqual([2, 3])
    expect(matches(stories[1], parseQuery('is:blocking'), { ...ctx, blocking })).toBe(true)
    expect(matches(stories[3], parseQuery('is:blocking'), { ...ctx, blocking })).toBe(false)
  })
})
