// Live updates over Server-Sent Events. The server only says *that* a
// project changed; the board refetches. Events from this tab are ignored
// (the response to its own request already carried the change), bursts are
// coalesced, and every (re)connection triggers one refetch to cover whatever
// happened while the stream was down.
import { CLIENT_ID } from './api'

export type LiveStatus = 'connecting' | 'live' | 'offline'

export interface LiveOptions {
  projectId: number
  /** Called (debounced) when other clients changed the project, with the ids of the stories mentioned. */
  onChange: (storyIds: number[]) => void
  onStatus?: (status: LiveStatus) => void
  /** Milliseconds to coalesce events over. */
  debounce?: number
  clientId?: string
  /** Injectable for tests. */
  EventSource?: typeof EventSource
}

export interface LiveConnection {
  close(): void
}

export function connectLive(opts: LiveOptions): LiveConnection {
  const ES = opts.EventSource ?? globalThis.EventSource
  const me = opts.clientId ?? CLIENT_ID
  const debounce = opts.debounce ?? 150
  let timer: ReturnType<typeof setTimeout> | undefined
  let opened = false
  let pending = new Set<number>()

  const schedule = (storyId?: number) => {
    if (storyId) pending.add(storyId)
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      const ids = [...pending]
      pending = new Set()
      opts.onChange(ids)
    }, debounce)
  }

  opts.onStatus?.('connecting')
  const source = new ES(`/api/projects/${opts.projectId}/events`)
  source.onopen = () => {
    opts.onStatus?.('live')
    // A reconnect may have missed events; the first open just confirms the
    // initial load, which the board does itself.
    if (opened) schedule()
    opened = true
  }
  source.onerror = () => {
    // EventSource reconnects on its own (server-suggested retry: 2s).
    opts.onStatus?.('offline')
  }
  const onEvent = (evt: MessageEvent) => {
    let client = ''
    let storyId: number | undefined
    try {
      const data = JSON.parse(evt.data)
      client = data.client ?? ''
      storyId = data.story_id
    } catch {
      // malformed payload: still refetch, it is cheap
    }
    if (client !== '' && client === me) return
    schedule(storyId)
  }
  source.addEventListener('stories', onEvent)
  source.addEventListener('project', onEvent)

  return {
    close() {
      clearTimeout(timer)
      source.close()
    },
  }
}
