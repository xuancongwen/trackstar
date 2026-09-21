import { describe, expect, it } from 'vitest'
import { formatRange, initials, setIterationTimeZone } from './format'

describe('formatRange', () => {
  it('shows the inclusive last day, in the iteration time zone', () => {
    setIterationTimeZone('UTC')
    expect(formatRange('2026-09-21T00:00:00Z', '2026-09-28T00:00:00Z')).toMatch(/21.*27/)
    setIterationTimeZone('America/Los_Angeles')
    expect(formatRange('2026-09-21T07:00:00Z', '2026-09-28T07:00:00Z')).toMatch(/21.*27/)
    setIterationTimeZone('UTC')
  })
})

describe('initials', () => {
  it('handles one, two and many names', () => {
    expect([initials('Sam'), initials('Sam Wen'), initials('Ana de la Cruz'), initials(' ')]).toEqual(['SA', 'SW', 'AC', '?'])
  })
})
