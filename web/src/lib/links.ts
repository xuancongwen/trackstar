// Story references in free text: "[#439]" links to story 439. The brackets
// keep everyday text like "let's do #2" from turning into links.
const STORY_REF = /\[#(\d+)\]/g

export type TextPart = { text: string; storyId?: number }

/** Splits text into plain runs and story references ("#439"), in order. */
export function storyRefs(text: string): TextPart[] {
  const parts: TextPart[] = []
  let last = 0
  for (const m of text.matchAll(STORY_REF)) {
    if (m.index > last) parts.push({ text: text.slice(last, m.index) })
    parts.push({ text: `#${m[1]}`, storyId: Number(m[1]) })
    last = m.index + m[0].length
  }
  if (last < text.length) parts.push({ text: text.slice(last) })
  return parts
}

/** A link to a story that works from anywhere; the app finds its project. */
export const storyHref = (id: number) => `#/s/${id}`
