// Fixtures shared by the unit tests.
import type { Story } from './types'

let nextId = 1
export function makeStory(partial: Partial<Story> = {}): Story {
  const id = partial.id ?? nextId++
  return {
    id,
    project_id: 1,
    title: `Story ${id}`,
    description: '',
    type: 'feature',
    state: 'backlog',
    section: 'backlog',
    estimate: 1,
    position: id * 1000,
    requester_id: 1,
    owner_id: null,
    labels: [],
    comment_count: 0,
    created_at: '2026-01-05T00:00:00Z',
    updated_at: '2026-01-05T00:00:00Z',
    accepted_at: null,
    ...partial,
  }
}
