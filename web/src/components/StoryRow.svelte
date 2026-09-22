<script lang="ts">
  import { ESTIMATES, canDrag, needsEstimate, nextActions } from '../lib/board'
  import { initials } from '../lib/format'
  import type { Story, StoryState, User } from '../lib/types'

  interface Props {
    story: Story
    users: Map<number, User>
    selected?: boolean
    busy?: boolean
    /** Just changed by someone else. */
    recent?: boolean
    /** Part of a multi-selection. */
    checked?: boolean
    /** Names of labels that are epics (rendered differently). */
    epicNames?: Set<string>
    readOnly?: boolean
    onopen: (story: Story) => void
    onaction: (story: Story, state: StoryState) => void
    onestimate: (story: Story, points: number) => void
    /** Shift-click: toggle membership in the multi-selection. */
    ontoggle?: (story: Story) => void
  }
  let {
    story,
    users,
    selected = false,
    busy = false,
    recent = false,
    checked = false,
    epicNames = new Set(),
    readOnly = false,
    onopen,
    onaction,
    onestimate,
    ontoggle,
  }: Props = $props()

  const TYPE_GLYPH = { feature: '★', bug: '●', chore: '⚙' } as const

  let owner = $derived(story.owner_id === null ? null : (users.get(story.owner_id) ?? null))
  let actions = $derived(nextActions(story))
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  class="story {story.state}"
  class:selected
  class:recent
  class:checked
  class:locked={!canDrag(story) || readOnly}
  data-story-id={story.id}
  data-no-drag={canDrag(story) && !readOnly ? undefined : ''}
  role="button"
  tabindex="-1"
  aria-label={story.title}
  aria-pressed={checked}
  onclick={(e) => (e.shiftKey && ontoggle ? ontoggle(story) : onopen(story))}
>
  <span class="grip" aria-hidden="true">{canDrag(story) ? '⠿' : '✓'}</span>
  <div class="body">
    <div class="title">{story.title}</div>
    <div class="meta">
      <span class="type {story.type}" title={story.type}>{TYPE_GLYPH[story.type]}</span>
      {story.type}
      {#if owner}· <span title={owner.display_name}>{owner.display_name}</span>{/if}
      {#if story.state !== 'icebox' && story.state !== 'backlog' && story.state !== 'unstarted'}
        · <span class="state-name">{story.state}</span>
      {/if}
      {#if story.comment_count > 0}· <span title="comments">💬 {story.comment_count}</span>{/if}
      {#if story.task_count > 0}
        · <span class="tasks" class:all-done={story.tasks_done === story.task_count} title="tasks done">☑ {story.tasks_done}/{story.task_count}</span>
      {/if}
      {#each story.labels as label (label)}<span class="label" class:epic={epicNames.has(label)}>{label}</span>{/each}
    </div>
  </div>

  <div class="side">
    {#if story.blocked}
      <span class="blocked" title={`Blocked by ${story.blocked_by.map((id) => '#' + id).join(', ')}`}>⛔</span>
    {/if}
    {#if story.estimate !== null}
      <span class="points" title="estimate">{story.estimate}</span>
    {/if}
    {#if owner}<span class="owner" title={owner.display_name}>{initials(owner.display_name)}</span>{/if}
    {#if readOnly}
      <!-- viewers get no actions -->
    {:else if needsEstimate(story) && story.state !== 'accepted'}
      <span class="estimates" role="group" aria-label="Estimate">
        {#each ESTIMATES as pts (pts)}
          <button
            disabled={busy}
            title={`Estimate ${pts} point${pts === 1 ? '' : 's'}`}
            onclick={(e) => {
              e.stopPropagation()
              onestimate(story, pts)
            }}>{pts}</button
          >
        {/each}
      </span>
    {:else}
      {#each actions as action (action.state)}
        <button
          class="action {action.tone}"
          disabled={busy}
          onclick={(e) => {
            e.stopPropagation()
            onaction(story, action.state)
          }}>{action.label}</button
        >
      {/each}
    {/if}
  </div>
</div>

<style>
  .story {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 6px 4px 2px;
    background: var(--row);
    border-bottom: 1px solid var(--border);
    border-left: 3px solid transparent;
    cursor: grab;
    user-select: none;
  }
  .story:hover {
    background: var(--row-hover);
  }
  .story.locked {
    cursor: pointer;
  }
  .story.started,
  .story.finished,
  .story.delivered {
    background: var(--row-started);
  }
  .story.rejected {
    background: var(--row-rejected);
  }
  .story.accepted {
    background: var(--row-accepted);
  }
  .story.recent {
    animation: recent-flash 2.5s ease-out;
  }
  @keyframes recent-flash {
    from {
      background: color-mix(in srgb, var(--accent) 35%, var(--row));
    }
  }
  @media (max-width: 600px) {
    .story {
      padding: 7px 6px 7px 2px;
    }
    .title {
      white-space: normal;
    }
    .meta {
      white-space: normal;
    }
  }
  .story.checked {
    background: color-mix(in srgb, var(--accent) 18%, var(--row));
  }
  .story.checked .grip {
    color: var(--accent);
  }
  .blocked {
    font-size: 11px;
  }
  .tasks.all-done {
    color: var(--accept);
  }
  .label.epic {
    color: var(--epic);
    font-weight: 600;
  }
  .story.selected {
    border-left-color: var(--accent);
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .grip {
    color: var(--muted);
    width: 14px;
    text-align: center;
    flex: none;
  }
  .body {
    flex: 1;
    min-width: 0;
  }
  .title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    color: var(--muted);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .type.feature {
    color: var(--feature);
  }
  .type.bug {
    color: var(--bug);
  }
  .type.chore {
    color: var(--chore);
  }
  .state-name {
    font-weight: 600;
  }
  .label {
    margin-left: 5px;
    color: var(--accept);
  }
  .side {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: none;
  }
  .points {
    min-width: 20px;
    text-align: center;
    font-weight: 700;
    font-size: 11px;
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 0 4px;
  }
  .owner {
    font-size: 10px;
    font-weight: 700;
    color: var(--muted);
  }
  .action {
    font-size: 11px;
    font-weight: 600;
    padding: 1px 8px;
    border-color: transparent;
    color: #fff;
  }
  .action.start {
    background: var(--start);
    color: var(--text);
  }
  .action.finish,
  .action.restart {
    background: var(--finish);
  }
  .action.deliver {
    background: var(--deliver);
  }
  .action.accept {
    background: var(--accept);
  }
  .action.reject {
    background: var(--danger);
  }
</style>
