import { describe, expect, it } from 'vitest'
import { storyRefs } from './links'

describe('storyRefs', () => {
  it('finds bracketed story references between plain text', () => {
    expect(storyRefs('This was done in [#439], see ([#12]).')).toEqual([
      { text: 'This was done in ' },
      { text: '#439', storyId: 439 },
      { text: ', see (' },
      { text: '#12', storyId: 12 },
      { text: ').' },
    ])
  })

  it('takes references at the start and end of the text', () => {
    expect(storyRefs('[#1] and [#2]')).toEqual([{ text: '#1', storyId: 1 }, { text: ' and ' }, { text: '#2', storyId: 2 }])
  })

  it('leaves bare and malformed hashes as text', () => {
    for (const text of ["let's do #2", '#439', '[#]', '[#12a]', '[ #12 ]', '&#39;']) {
      expect(storyRefs(text)).toEqual([{ text }])
    }
  })
})
