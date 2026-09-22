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

export const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}
