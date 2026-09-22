// The only file that talks to SortableJS.
//
// Sortable reorders DOM nodes itself, which would fight Svelte's keyed each
// blocks. So on drop we put the node back exactly where it was and report
// the intent; the board updates its state and Svelte re-renders the new order.
import Sortable from 'sortablejs'
import type { DropSection } from './types'

export interface DropEvent {
  storyId: number
  from: DropSection
  to: DropSection
  /** Index among the draggable story rows of the target list. */
  index: number
}

export interface SortableListOptions {
  section: DropSection
  /** May the story be dropped into `target`? Checked while dragging. */
  canDrop: (storyId: number, target: DropSection) => boolean
  onDrop: (event: DropEvent) => void
  onDragState?: (dragging: boolean) => void
}

export const ROW_SELECTOR = '[data-story-id]'

export function sortableList(el: HTMLElement, options: SortableListOptions) {
  let opts = options
  let originalNext: Node | null = null

  el.dataset.section = opts.section
  const sortable = Sortable.create(el, {
    group: {
      name: 'stories',
      put: (to, _from, dragged) =>
        opts.canDrop(Number(dragged.dataset.storyId), to.el.dataset.section as DropSection),
    },
    draggable: ROW_SELECTOR,
    filter: '[data-no-drag], button, select, input, textarea, a',
    preventOnFilter: false,
    animation: 120,
    ghostClass: 'drag-ghost',
    chosenClass: 'drag-chosen',
    forceFallback: false,
    // Dropping outside every accepting list (rejected by canDrop, on a header,
    // in the gap between columns) puts the row back instead of leaving it
    // wherever the drag path last passed through.
    revertOnSpill: true,
    onStart(evt) {
      originalNext = evt.item.nextSibling
      opts.onDragState?.(true)
    },
    onEnd(evt) {
      const { item, from, to, oldDraggableIndex, newDraggableIndex } = evt
      opts.onDragState?.(false)
      const moved = from !== to || oldDraggableIndex !== newDraggableIndex
      // Undo Sortable's DOM change; state drives the DOM.
      from.insertBefore(item, originalNext)
      originalNext = null
      if (!moved || newDraggableIndex === undefined) return
      opts.onDrop({
        storyId: Number(item.dataset.storyId),
        from: from.dataset.section as DropSection,
        to: to.dataset.section as DropSection,
        index: newDraggableIndex,
      })
    },
  })

  return {
    update(next: SortableListOptions) {
      opts = next
      el.dataset.section = next.section
    },
    destroy() {
      sortable.destroy()
    },
  }
}
