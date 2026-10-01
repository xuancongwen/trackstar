import type { FeedEntry } from './types'

/** How a feed entry reads: "<actor> <verb> <story title><suffix>". */
export interface FeedPhrase {
  verb: string
  suffix: string
}

const STATE_VERBS: Record<string, string> = {
  started: 'started',
  finished: 'finished',
  delivered: 'delivered',
  accepted: 'accepted',
  rejected: 'rejected',
}

/** Words for one feed entry. name resolves a user id (as stored: a string) to a display name. */
export function feedPhrase(e: FeedEntry, name: (id: string | number) => string): FeedPhrase {
  switch (e.kind) {
    case 'comment':
      return { verb: 'commented on', suffix: '' }
    case 'created':
      return { verb: 'created', suffix: e.new_value ? ` in the ${e.new_value === 'current' ? 'current iteration' : e.new_value}` : '' }
    case 'state':
      if (e.old_value === 'rejected' && e.new_value === 'started') return { verb: 'restarted', suffix: '' }
      if (STATE_VERBS[e.new_value]) return { verb: STATE_VERBS[e.new_value], suffix: '' }
      return { verb: 'moved', suffix: ` from ${e.old_value} back to ${e.new_value}` }
    case 'estimate':
      if (e.new_value === '') return { verb: 'removed the estimate from', suffix: '' }
      return { verb: 'estimated', suffix: ` at ${e.new_value} point${e.new_value === '1' ? '' : 's'}` }
    case 'owner':
      if (e.new_value === '') return { verb: 'unassigned', suffix: '' }
      if (e.new_value === String(e.user_id)) return { verb: 'took', suffix: '' }
      return { verb: 'assigned', suffix: ` to ${name(e.new_value)}` }
    case 'moved':
      return { verb: 'moved', suffix: ` from ${e.old_value} to ${e.new_value}` }
    case 'deleted':
      return { verb: 'deleted', suffix: '' }
    case 'restored':
      return { verb: 'restored', suffix: '' }
    case 'reopened':
      return { verb: 'reopened', suffix: '' }
    default:
      return { verb: 'updated', suffix: '' }
  }
}

/**
 * Drops the entries that only repeat their neighbour: starting a story
 * assigns it to whoever started it, which is recorded as a second change in
 * the same moment.
 */
export function withoutEchoes(entries: FeedEntry[]): FeedEntry[] {
  return entries.filter(
    (e) =>
      !(
        e.kind === 'owner' &&
        e.new_value === String(e.user_id) &&
        entries.some((o) => o.kind === 'state' && o.story_id === e.story_id && o.user_id === e.user_id && o.created_at === e.created_at)
      ),
  )
}

/** A feed entry as shown; stories > 1 means it stands for that many stories created in one go. */
export type FeedLine = FeedEntry & { stories: number }

/**
 * Folds a run of stories created by the same person in the same project into
 * one line ("created 8 stories"), so an import or an agent filing a backlog
 * does not push everything else out of the feed.
 */
export function foldCreated(entries: FeedEntry[]): FeedLine[] {
  const out: FeedLine[] = []
  for (const e of entries) {
    const last = out[out.length - 1]
    if (e.kind === 'created' && last?.kind === 'created' && last.user_id === e.user_id && last.project_id === e.project_id) last.stories++
    else out.push({ ...e, stories: 1 })
  }
  return out
}
