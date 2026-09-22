<script lang="ts">
  import { formatDayTime } from '../lib/format'
  import type { Story } from '../lib/types'

  interface Props {
    stories: Story[]
    onrestore: (story: Story) => void
    onopen: (story: Story) => void
  }
  let { stories, onrestore, onopen }: Props = $props()
</script>

<section class="panel" aria-label="Deleted">
  <header>
    <h2>Deleted</h2>
    <span class="summary">{stories.length} stories · kept 30 days</span>
  </header>
  <div class="scroll">
    {#each stories as story (story.id)}
      <div class="row">
        <button class="link title" onclick={() => onopen(story)}>{story.title}</button>
        <span class="muted">{story.deleted_at ? formatDayTime(story.deleted_at) : ''}</span>
        <button onclick={() => onrestore(story)}>Restore</button>
      </div>
    {:else}
      <p class="muted">Nothing in the trash.</p>
    {/each}
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
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-bottom: 1px solid var(--border);
    background: var(--row);
    font-size: 12px;
  }
  .title {
    flex: 1;
    text-align: left;
    color: inherit;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row .muted {
    font-size: 11px;
    white-space: nowrap;
  }
  p {
    text-align: center;
    padding: 12px;
    margin: 0;
  }
</style>
