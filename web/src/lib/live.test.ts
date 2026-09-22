import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectLive, type LiveStatus } from './live'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  private listeners = new Map<string, ((e: MessageEvent) => void)[]>()
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn])
  }
  close() {
    this.closed = true
  }
  emit(type: string, data: unknown) {
    for (const fn of this.listeners.get(type) ?? []) fn({ data: JSON.stringify(data) } as MessageEvent)
  }
}

describe('connectLive', () => {
  let onChange: ReturnType<typeof vi.fn<(ids: number[]) => void>>
  let statuses: LiveStatus[]

  beforeEach(() => {
    vi.useFakeTimers()
    FakeEventSource.instances = []
    onChange = vi.fn<(ids: number[]) => void>()
    statuses = []
  })
  afterEach(() => vi.useRealTimers())

  const connect = () =>
    connectLive({
      projectId: 7,
      clientId: 'me',
      onChange,
      onStatus: (s) => statuses.push(s),
      EventSource: FakeEventSource as unknown as typeof EventSource,
    })

  it('subscribes to the project stream and coalesces bursts', () => {
    connect()
    const es = FakeEventSource.instances[0]
    expect(es.url).toBe('/api/projects/7/events')
    es.onopen?.()
    expect(statuses).toEqual(['connecting', 'live'])

    es.emit('stories', { client: 'someone-else', story_id: 1 })
    es.emit('stories', { client: 'someone-else', story_id: 2 })
    es.emit('project', {})
    expect(onChange).not.toHaveBeenCalled()
    vi.advanceTimersByTime(200)
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith([1, 2])
  })

  it('ignores echoes of this tab but not events without a client', () => {
    connect()
    const es = FakeEventSource.instances[0]
    es.emit('stories', { client: 'me', story_id: 1 })
    vi.advanceTimersByTime(200)
    expect(onChange).not.toHaveBeenCalled()
    es.emit('stories', { story_id: 1 })
    vi.advanceTimersByTime(200)
    expect(onChange).toHaveBeenCalledTimes(1)
  })

  it('refetches after a reconnect, not after the first open', () => {
    connect()
    const es = FakeEventSource.instances[0]
    es.onopen?.()
    vi.advanceTimersByTime(200)
    expect(onChange).not.toHaveBeenCalled()

    es.onerror?.()
    es.onopen?.()
    vi.advanceTimersByTime(200)
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(statuses).toEqual(['connecting', 'live', 'offline', 'live'])
  })

  it('close() stops the stream and any pending refetch', () => {
    const live = connect()
    const es = FakeEventSource.instances[0]
    es.emit('stories', { client: 'other' })
    live.close()
    vi.advanceTimersByTime(500)
    expect(es.closed).toBe(true)
    expect(onChange).not.toHaveBeenCalled()
  })
})
