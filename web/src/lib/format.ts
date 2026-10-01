// Iteration boundaries are midnights in the server's TRACKSTAR_TIMEZONE, so
// days are formatted in that zone; clock times use the viewer's own zone.
let day = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' })
const dayTime = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })

export function setIterationTimeZone(timeZone: string) {
  try {
    day = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', timeZone })
  } catch {
    // unknown zone name in this browser: keep UTC
  }
}

export const formatDay = (value: string | Date) => day.format(new Date(value))
export const formatDayTime = (value: string | Date) => dayTime.format(new Date(value))

/** "Sep 21 – Sep 27" for a half-open [start, end) range. */
export function formatRange(startAt: string, endAt: string): string {
  return `${formatDay(startAt)} – ${formatDay(new Date(new Date(endAt).getTime() - 1000))}`
}

/** "just now", "5m ago", "3h ago", "2d ago", then the date once it is over a month old. */
export function timeAgo(value: string | Date, now: Date = new Date()): string {
  const seconds = Math.max(0, (now.getTime() - new Date(value).getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  if (seconds < 30 * 86400) return `${Math.floor(seconds / 86400)}d ago`
  return formatDay(value)
}

export const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}
