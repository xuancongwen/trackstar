// The board's filter language, evaluated client-side over the loaded stories.
//
//   oauth                      free text: title or description contains "oauth"
//   "exact phrase"             quoted free text
//   owner:me | owner:sam | owner:none
//   requester:me | requester:kim
//   type:bug        state:started        estimate:3       estimate:none
//   label:auth      epic:auth  (same thing: an epic is a label)
//   is:blocked      is:unestimated       is:blocking      has:tasks
//   -type:chore                          negate any term with a leading dash
//
// Terms are ANDed. Names match display names case-insensitively by prefix,
// so owner:sa matches "Sam Wen".
import type { Story, User } from './types'

export interface Term {
  key: string // '' for free text
  value: string
  negate: boolean
}

export function parseQuery(query: string): Term[] {
  const terms: Term[] = []
  const re = /(-?)(?:([a-z_]+):)?(?:"([^"]*)"|(\S+))/gi
  for (const m of query.matchAll(re)) {
    const value = (m[3] ?? m[4] ?? '').toLowerCase()
    if (value === '' && !m[2]) continue
    terms.push({ key: (m[2] ?? '').toLowerCase(), value, negate: m[1] === '-' })
  }
  return terms
}

export interface FilterContext {
  me: User
  users: Map<number, User>
  /** Ids of stories that block at least one other story. */
  blocking?: Set<number>
}

function userMatches(id: number | null, value: string, ctx: FilterContext): boolean {
  if (value === 'none' || value === 'nobody' || value === 'unassigned') return id === null
  if (id === null) return false
  if (value === 'me') return id === ctx.me.id
  const u = ctx.users.get(id)
  if (!u) return false
  const name = u.display_name.toLowerCase()
  return name.startsWith(value) || name.split(/\s+/).some((part) => part.startsWith(value)) || u.email.toLowerCase().startsWith(value)
}

export function termMatches(story: Story, term: Term, ctx: FilterContext): boolean {
  const v = term.value
  switch (term.key) {
    case '':
      return story.title.toLowerCase().includes(v) || story.description.toLowerCase().includes(v)
    case 'owner':
      return userMatches(story.owner_id, v, ctx)
    case 'requester':
      return userMatches(story.requester_id, v, ctx)
    case 'type':
      return story.type === v
    case 'state':
      return story.state === v || (v === 'done' && story.state === 'accepted') || (v === 'open' && story.state !== 'accepted')
    case 'section':
      return story.section === v
    case 'estimate':
    case 'points':
      return v === 'none' ? story.estimate === null : story.estimate === Number(v)
    case 'label':
    case 'epic':
      return story.labels.some((l) => l === v || l.startsWith(v))
    case 'id':
      return String(story.id) === v.replace(/^#/, '')
    case 'is':
      switch (v) {
        case 'blocked':
          return story.blocked
        case 'blocking':
          return ctx.blocking?.has(story.id) ?? false
        case 'unestimated':
          return story.estimate === null
        case 'estimated':
          return story.estimate !== null
        case 'feature':
        case 'bug':
        case 'chore':
          return story.type === v
        default:
          return false
      }
    case 'has':
      switch (v) {
        case 'tasks':
          return story.task_count > 0
        case 'comments':
          return story.comment_count > 0
        case 'labels':
          return story.labels.length > 0
        case 'owner':
          return story.owner_id !== null
        default:
          return false
      }
    default:
      // Unknown key: treat "key:value" as free text so typos still match something sensible.
      return story.title.toLowerCase().includes(`${term.key}:${v}`)
  }
}

export function matches(story: Story, terms: Term[], ctx: FilterContext): boolean {
  return terms.every((t) => termMatches(story, t, ctx) !== t.negate)
}

/** Stories that block at least one other story in the list. */
export function blockingSet(stories: Story[]): Set<number> {
  const out = new Set<number>()
  for (const s of stories) for (const b of s.blocked_by) out.add(b)
  return out
}

/** Built-in filters offered next to the saved ones. */
export const BUILT_IN_FILTERS: { name: string; query: string }[] = [
  { name: 'My work', query: 'owner:me -state:accepted' },
  { name: 'Unestimated', query: 'is:unestimated type:feature' },
  { name: 'Blocked', query: 'is:blocked' },
  { name: 'Bugs', query: 'type:bug' },
]
