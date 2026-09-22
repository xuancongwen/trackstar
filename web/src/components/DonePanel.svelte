<script lang="ts">
  import { formatRange } from '../lib/format'
  import { totalPoints } from '../lib/board'
  import type { Iteration, Story, StoryState, User } from '../lib/types'
  import IterationChart from './IterationChart.svelte'
  import StoryRow from './StoryRow.svelte'

  interface Props {
    iterations: Iteration[]
    stories: Story[]
    users: Map<number, User>
    selectedId: number | null
    velocity: number | null
    onopen: (story: Story) => void
  }
  let { iterations, stories, users, selectedId, velocity, onopen }: Props = $props()

  const PAGE = 5
  let shown = $state(PAGE)

  // Completed iterations, newest first, each with the stories accepted in it.
  let completed = $derived(iterations.filter((it) => !it.current).reverse())
  let groups = $derived(
    completed
      .slice(0, shown)
      .map((it) => ({
        iteration: it,
        stories: stories.filter((s) => s.accepted_at !== null && s.accepted_at >= it.start_at && s.accepted_at < it.end_at),
      })),
  )

  const noop = (_story: Story, _value: StoryState | number) => {}
</script>

<section class="panel" aria-label="Done">
  <header>
    <h2>Done</h2>
    <span class="summary">{totalPoints(stories)} pts · {stories.length} stories</span>
  </header>
  <div class="scroll">
    <IterationChart {iterations} {velocity} />
    {#each groups as group (group.iteration.number)}
      <div class="marker">
        <span>Iteration {group.iteration.number} · {formatRange(group.iteration.start_at, group.iteration.end_at)}</span>
        <span>{group.iteration.points} pts</span>
      </div>
      {#each group.stories as story (story.id)}
        <StoryRow {story} {users} selected={story.id === selectedId} {onopen} onaction={noop} onestimate={noop} />
      {/each}
    {:else}
      <p class="muted">No completed iterations yet.</p>
    {/each}
    {#if completed.length > shown}
      <p><button onclick={() => (shown += PAGE)}>Show {Math.min(PAGE, completed.length - shown)} older iterations</button></p>
    {/if}
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
    padding: 6px 8px;
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
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
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
  p {
    text-align: center;
    padding: 12px;
    margin: 0;
  }
</style>
