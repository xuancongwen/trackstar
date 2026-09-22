import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeStory } from '../lib/testing'
import type { Story, User } from '../lib/types'
import StoryRow from './StoryRow.svelte'

const sam: User = { id: 1, email: 'sam@example.com', display_name: 'Sam Wen', is_admin: true, is_active: true }

function setup(story: Story) {
  const handlers = { onopen: vi.fn(), onaction: vi.fn(), onestimate: vi.fn() }
  render(StoryRow, { story, users: new Map([[sam.id, sam]]), ...handlers })
  return handlers
}

describe('StoryRow', () => {
  afterEach(cleanup)

  it('shows title, type, owner and estimate', () => {
    setup(makeStory({ title: 'Add OAuth support', estimate: 3, owner_id: 1, labels: ['auth'] }))
    expect(screen.getByText('Add OAuth support')).toBeTruthy()
    expect(screen.getByText('3')).toBeTruthy()
    expect(screen.getByText('Sam Wen')).toBeTruthy()
    expect(screen.getByText('auth')).toBeTruthy()
  })

  it('walks the workflow one button at a time', async () => {
    const cases: [Story['state'], string, Story['state']][] = [
      ['backlog', 'Start', 'started'],
      ['started', 'Finish', 'finished'],
      ['finished', 'Deliver', 'delivered'],
      ['rejected', 'Restart', 'started'],
    ]
    for (const [state, label, next] of cases) {
      const story = makeStory({ state, section: state === 'backlog' ? 'backlog' : 'current' })
      const { onaction, onopen } = setup(story)
      await fireEvent.click(screen.getByRole('button', { name: label }))
      expect(onaction).toHaveBeenCalledWith(story, next)
      expect(onopen).not.toHaveBeenCalled() // the button must not also open the drawer
      cleanup()
    }
  })

  it('offers accept and reject for delivered stories', async () => {
    const story = makeStory({ state: 'delivered', section: 'current' })
    const { onaction } = setup(story)
    await fireEvent.click(screen.getByRole('button', { name: 'Reject' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Accept' }))
    expect(onaction.mock.calls.map((c) => c[1])).toEqual(['rejected', 'accepted'])
  })

  it('asks for an estimate before an unestimated feature can start', async () => {
    const story = makeStory({ estimate: null })
    const { onestimate } = setup(story)
    expect(screen.queryByRole('button', { name: 'Start' })).toBeNull()
    await fireEvent.click(screen.getByTitle('Estimate 5 points'))
    expect(onestimate).toHaveBeenCalledWith(story, 5)
  })

  it('lets unestimated bugs start right away', () => {
    setup(makeStory({ type: 'bug', estimate: null }))
    expect(screen.getByRole('button', { name: 'Start' })).toBeTruthy()
  })

  it('locks accepted stories: no actions and not draggable', () => {
    setup(makeStory({ state: 'accepted', section: 'current', accepted_at: '2026-01-06T00:00:00Z' }))
    const rowEl = screen.getByRole('button', { name: /Story/ })
    expect(rowEl.hasAttribute('data-no-drag')).toBe(true)
    expect(rowEl.querySelectorAll('button')).toHaveLength(0)
  })

  it('opens on click', async () => {
    const story = makeStory()
    const { onopen } = setup(story)
    await fireEvent.click(screen.getByText(story.title))
    expect(onopen).toHaveBeenCalledWith(story)
  })
})
