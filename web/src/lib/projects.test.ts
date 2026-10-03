import { describe, expect, it } from 'vitest'
import { starredFirst } from './projects'
import type { Project } from './types'

const project = (name: string, starred = false) => ({ id: name.length, name, starred }) as Project

describe('starredFirst', () => {
  it('moves starred projects up and keeps the order within each group', () => {
    const list = [project('a'), project('b', true), project('c'), project('d', true)]
    expect(starredFirst(list).map((p) => p.name)).toEqual(['b', 'd', 'a', 'c'])
  })
})
