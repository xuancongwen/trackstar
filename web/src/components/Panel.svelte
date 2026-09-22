<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { BacklogRow } from '../lib/board'
  import { SECTION_TITLES } from '../lib/board'
  import { sortableList, type DropEvent } from '../lib/dragdrop'
  import { formatDay } from '../lib/format'
  import type { DropSection, Story, StoryState, User } from '../lib/types'
  import StoryRow from './StoryRow.svelte'

  interface Props {
    section: DropSection
    rows: BacklogRow[]
    users: Map<number, User>
    selectedId: number | null
    busyIds: Set<number>
    summary?: string
    /** Rendered above the sortable list (accepted stories, progress…). */
    top?: Snippet
    dragDisabled?: boolean
    /** A drag is in progress somewhere on the board. */
    dragging?: boolean
    canDrop: (storyId: number, target: DropSection) => boolean
    ondrop: (event: DropEvent) => void
    ondragstate: (dragging: boolean) => void
    onadd: (section: DropSection) => void
    onopen: (story: Story) => void
    onaction: (story: Story, state: StoryState) => void
    onestimate: (story: Story, points: number) => void
  }
  let {
    section,
    rows,
    users,
    selectedId,
    busyIds,
    summary = '',
    top,
    dragDisabled = false,
    dragging = false,
    canDrop,
    ondrop,
    ondragstate,
    onadd,
    onopen,
    onaction,
    onestimate,
  }: Props = $props()

  let storyCount = $derived(rows.filter((r) => r.kind === 'story').length)
</script>

<section class="panel" aria-label={SECTION_TITLES[section]}>
  <header>
    <h2>{SECTION_TITLES[section]}</h2>
    <span class="summary">{summary}</span>
    <button class="add" title={`Add story to ${SECTION_TITLES[section]}`} onclick={() => onadd(section)}>+</button>
  </header>

  <div class="scroll">
    {@render top?.()}
    <!-- The list fills the rest of the column, so dropping anywhere below the
         last row appends to the end. -->
    <div
      class="list"
      class:empty={storyCount === 0}
      class:dragging
      use:sortableList={{
        section,
        canDrop: (id, target) => !dragDisabled && canDrop(id, target),
        onDrop: ondrop,
        onDragState: ondragstate,
      }}
    >
      {#each rows as row (row.key)}
        {#if row.kind === 'marker'}
          <div class="marker" data-no-drag>
            <span>Iteration {row.number} · {formatDay(row.startAt)}</span>
            <span>{row.points} pts</span>
          </div>
        {:else}
          <StoryRow
            story={row.story}
            {users}
            selected={row.story.id === selectedId}
            busy={busyIds.has(row.story.id)}
            {onopen}
            {onaction}
            {onestimate}
          />
        {/if}
      {/each}
      {#if storyCount === 0}
        <p class="hint muted" data-no-drag>
          {dragDisabled ? 'No matching stories.' : 'Nothing here. Drag a story in or press +.'}
        </p>
      {/if}
    </div>
  </div>
</section>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: hidden;
  }
  header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    background: var(--panel-head);
    color: var(--panel-head-text);
  }
  h2 {
    margin: 0;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .summary {
    flex: 1;
    font-size: 11px;
    opacity: 0.8;
    text-align: right;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .add {
    padding: 0 7px;
    font-weight: 700;
    background: transparent;
    color: inherit;
    border-color: rgba(255, 255, 255, 0.35);
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }
  .list {
    flex: 1;
    min-height: 56px;
    border-radius: 4px;
    outline: 2px dashed transparent;
    outline-offset: -4px;
    transition: outline-color 120ms;
  }
  .list.dragging {
    outline-color: var(--border);
  }
  .marker {
    display: flex;
    justify-content: space-between;
    padding: 2px 8px;
    font-size: 11px;
    font-weight: 600;
    color: var(--muted);
    background: var(--bg);
    border-bottom: 1px solid var(--border);
  }
  .hint {
    margin: 0;
    padding: 4px 10px 12px;
    text-align: center;
    font-size: 12px;
  }
</style>
