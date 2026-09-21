import { beforeEach, describe, expect, it, vi } from 'vitest'

// Capture the options our adapter hands to SortableJS so the tests can play
// the part of the library: move the DOM node, then fire onStart/onEnd.
const created: { el: HTMLElement; options: any }[] = []
vi.mock('sortablejs', () => ({
  default: {
    create: (el: HTMLElement, options: any) => {
      created.push({ el, options })
      return { destroy: vi.fn() }
    },
  },
}))

import { sortableList, type DropEvent } from './dragdrop'

function list(section: string, ids: number[]): HTMLElement {
  const el = document.createElement('div')
  for (const id of ids) {
    const row = document.createElement('div')
    row.dataset.storyId = String(id)
    el.append(row, document.createComment(`anchor-${id}`)) // Svelte leaves anchors between rows
  }
  document.body.append(el)
  return el
}

const rowIds = (el: HTMLElement) => [...el.querySelectorAll<HTMLElement>('[data-story-id]')].map((r) => Number(r.dataset.storyId))
const row = (el: HTMLElement, id: number) => el.querySelector<HTMLElement>(`[data-story-id="${id}"]`)!

describe('sortableList', () => {
  let drops: DropEvent[]
  let dragStates: boolean[]
  let backlog: HTMLElement
  let icebox: HTMLElement
  const canDrop = vi.fn((_id: number, _target: string) => true)

  beforeEach(() => {
    created.length = 0
    document.body.innerHTML = ''
    drops = []
    dragStates = []
    canDrop.mockClear()
    backlog = list('backlog', [1, 2, 3])
    icebox = list('icebox', [7])
    for (const [el, section] of [[backlog, 'backlog'], [icebox, 'icebox']] as const) {
      sortableList(el, { section, canDrop, onDrop: (e) => drops.push(e), onDragState: (d) => dragStates.push(d) })
    }
  })

  /** Simulates SortableJS dragging `id` to `index` of `to`. */
  function drag(id: number, from: HTMLElement, to: HTMLElement, index: number) {
    const options = created.find((c) => c.el === from)!.options
    const item = row(from, id)
    const oldDraggableIndex = rowIds(from).indexOf(id)
    options.onStart({ item })
    item.remove()
    const rows = to.querySelectorAll('[data-story-id]')
    to.insertBefore(item, rows[index] ?? null)
    options.onEnd({ item, from, to, oldDraggableIndex, newDraggableIndex: index })
  }

  it('reports a reorder and restores the DOM so Svelte stays in charge', () => {
    const before = backlog.innerHTML
    drag(3, backlog, backlog, 0)

    expect(drops).toEqual([{ storyId: 3, from: 'backlog', to: 'backlog', index: 0 }])
    expect(backlog.innerHTML).toBe(before) // node back in place, anchors untouched
    expect(dragStates).toEqual([true, false])
  })

  it('reports a move between sections and restores both lists', () => {
    const before = [backlog.innerHTML, icebox.innerHTML]
    drag(7, icebox, backlog, 1)

    expect(drops).toEqual([{ storyId: 7, from: 'icebox', to: 'backlog', index: 1 }])
    expect([backlog.innerHTML, icebox.innerHTML]).toEqual(before)
  })

  it('stays silent when the story is dropped where it started', () => {
    drag(2, backlog, backlog, 1)
    expect(drops).toEqual([])
    expect(rowIds(backlog)).toEqual([1, 2, 3])
  })

  it('asks canDrop with the dragged story and the target section', () => {
    const { options } = created.find((c) => c.el === backlog)!
    canDrop.mockReturnValueOnce(false)
    expect(options.group.put({ el: backlog }, { el: icebox }, row(icebox, 7))).toBe(false)
    expect(canDrop).toHaveBeenCalledWith(7, 'backlog')
    expect(options.group.put({ el: backlog }, { el: icebox }, row(icebox, 7))).toBe(true)
  })

  it('only lets story rows be dragged, never markers or buttons', () => {
    const { options } = created[0]
    expect(options.draggable).toBe('[data-story-id]')
    expect(options.filter).toContain('[data-no-drag]')
    expect(options.filter).toContain('button')
  })
})
