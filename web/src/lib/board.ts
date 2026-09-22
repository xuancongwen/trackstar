// Pure board logic: grouping, drag/drop rules, workflow actions and the
// backlog projection. No DOM, no network — everything here is unit tested.
import type { DropSection, MoveRequest, Story, StoryState } from './types'

export const ESTIMATES = [0, 1, 2, 3, 5, 8] as const

export const SECTION_TITLES: Record<DropSection | 'done', string> = {
  icebox: 'Icebox',
  backlog: 'Backlog',
  current: 'Current iteration',
  done: 'Done',
}

const IN_PROGRESS: StoryState[] = ['started', 'finished', 'delivered', 'rejected']

/** Orderable stories of a section, in priority order. Accepted stories are frozen and excluded. */
export function sectionStories(stories: Story[], section: DropSection): Story[] {
  return stories
    .filter((s) => s.section === section && s.state !== 'accepted')
    .sort((a, b) => a.position - b.position || a.id - b.id)
}

/** Stories accepted during the current iteration, oldest first. */
export function acceptedThisIteration(stories: Story[]): Story[] {
  return stories
    .filter((s) => s.section === 'current' && s.state === 'accepted')
    .sort((a, b) => (a.accepted_at ?? '').localeCompare(b.accepted_at ?? ''))
}

/** Points that count towards velocity: estimated features only. */
export function storyPoints(story: Story): number {
  return story.type === 'feature' ? (story.estimate ?? 0) : 0
}

export function totalPoints(stories: Story[]): number {
  return stories.reduce((sum, s) => sum + storyPoints(s), 0)
}

export function needsEstimate(story: Story): boolean {
  return story.type === 'feature' && story.estimate === null
}

export function canDrag(story: Story): boolean {
  return story.state !== 'accepted'
}

/** Mirrors the server rule: work in progress stays in the current iteration. */
export function canDrop(story: Story, target: DropSection): boolean {
  if (!canDrag(story)) return false
  if (IN_PROGRESS.includes(story.state)) return target === 'current'
  return true
}

/** The state a story takes when dropped into another section. */
export function entryState(section: DropSection): StoryState {
  return section === 'current' ? 'unstarted' : section
}

/**
 * Translates "story dropped at index" into the neighbour-based request the
 * API expects. `orderedIds` is the target section as currently displayed;
 * `newIndex` is the index among draggable rows after the drop.
 */
export function moveRequest(orderedIds: number[], movedId: number, section: DropSection, newIndex: number): MoveRequest {
  const ids = orderedIds.filter((id) => id !== movedId)
  const index = Math.max(0, Math.min(newIndex, ids.length))
  return { section, prev_id: ids[index - 1] ?? null, next_id: ids[index] ?? null }
}

/** Optimistic version of the move so the row lands instantly; the server response replaces it. */
export function applyMove(stories: Story[], movedId: number, section: DropSection, newIndex: number): Story[] {
  const moved = stories.find((s) => s.id === movedId)
  if (!moved || !canDrop(moved, section)) return stories

  const target = sectionStories(stories, section).filter((s) => s.id !== movedId)
  const index = Math.max(0, Math.min(newIndex, target.length))
  const prev = target[index - 1]?.position
  const next = target[index]?.position
  let position: number
  if (prev === undefined && next === undefined) position = 0
  else if (prev === undefined) position = next! - 1
  else if (next === undefined) position = prev + 1
  else position = (prev + next) / 2

  return stories.map((s) =>
    s.id === movedId
      ? { ...s, section, position, state: s.section === section ? s.state : entryState(section) }
      : s,
  )
}

export interface Action {
  label: string
  state: StoryState
  tone: 'start' | 'finish' | 'deliver' | 'accept' | 'reject' | 'restart'
}

/** The workflow buttons shown on a story row. */
export function nextActions(story: Story): Action[] {
  switch (story.state) {
    case 'icebox':
    case 'backlog':
    case 'unstarted':
      return [{ label: 'Start', state: 'started', tone: 'start' }]
    case 'started':
      return [{ label: 'Finish', state: 'finished', tone: 'finish' }]
    case 'finished':
      return [{ label: 'Deliver', state: 'delivered', tone: 'deliver' }]
    case 'delivered':
      return [
        { label: 'Accept', state: 'accepted', tone: 'accept' },
        { label: 'Reject', state: 'rejected', tone: 'reject' },
      ]
    case 'rejected':
      return [{ label: 'Restart', state: 'started', tone: 'restart' }]
    default:
      return []
  }
}

export type BacklogRow =
  | { kind: 'marker'; key: string; number: number; startAt: Date; points: number }
  | { kind: 'story'; key: string; story: Story }

/**
 * Splits the ordered backlog into projected iterations. Each iteration takes
 * stories until the next one would push it past the velocity; a story larger
 * than the velocity gets an iteration of its own. Markers carry the planned
 * points so the header can show "Iteration 14 · 9 pts".
 */
export function projectBacklog(
  backlog: Story[],
  velocity: number,
  current: { number: number; end_at: string },
  iterationLengthDays: number,
): BacklogRow[] {
  const capacity = Math.max(1, velocity)
  const rows: BacklogRow[] = []
  let points = 0 // planned points of the iteration being filled
  let offset = 0
  let marker: Extract<BacklogRow, { kind: 'marker' }> | undefined

  for (const story of backlog) {
    const pts = storyPoints(story)
    // Unpointed work never opens an iteration; it rides along with its neighbours.
    if (!marker || (pts > 0 && points > 0 && points + pts > capacity)) {
      offset += 1
      const startAt = new Date(current.end_at)
      startAt.setUTCDate(startAt.getUTCDate() + (offset - 1) * iterationLengthDays)
      // Display-only date: nudge past midnight so a DST shift in between cannot flip the day.
      startAt.setUTCHours(startAt.getUTCHours() + 2)
      marker = { kind: 'marker', key: `marker-${current.number + offset}`, number: current.number + offset, startAt, points: 0 }
      rows.push(marker)
      points = 0
    }
    points += pts
    marker.points = points
    rows.push({ kind: 'story', key: `story-${story.id}`, story })
  }
  return rows
}

/**
 * Ids of the checked stories in board order (icebox, backlog, current; each
 * by position), so a bulk move keeps their relative order.
 */
export function orderedSelection(stories: Story[], checked: Set<number>): number[] {
  const out: number[] = []
  for (const section of ['icebox', 'backlog', 'current'] as const) {
    for (const s of sectionStories(stories, section)) if (checked.has(s.id)) out.push(s.id)
  }
  return out
}

/** Bulk version of moveRequest: the target list minus every moved story. */
export function bulkMoveRequest(orderedIds: number[], movedIds: number[], section: DropSection, newIndex: number): MoveRequest {
  const moving = new Set(movedIds)
  const ids = orderedIds.filter((id) => !moving.has(id))
  const index = Math.max(0, Math.min(newIndex, ids.length))
  return { section, prev_id: ids[index - 1] ?? null, next_id: ids[index] ?? null }
}
