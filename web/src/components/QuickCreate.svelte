<script lang="ts">
  import { ESTIMATES, SECTION_TITLES, canEstimate } from '../lib/board'
  import type { DropSection, NewStory, Project, StoryType } from '../lib/types'

  interface Props {
    section: DropSection
    /** Decides whether bugs and chores get the point scale. */
    project?: Pick<Project, 'estimate_bugs_and_chores'> | null
    oncreate: (story: NewStory, keepOpen: boolean) => Promise<void>
    onclose: () => void
  }
  let { section, project = null, oncreate, onclose }: Props = $props()

  let title = $state('')
  let type = $state<StoryType>('feature')
  let estimate = $state<number | null>(null)
  // svelte-ignore state_referenced_locally
  let target = $state<DropSection>(section)
  let error = $state('')
  let busy = $state(false)
  let titleInput: HTMLInputElement

  let pointable = $derived(canEstimate(type, project))

  // keepOpen (Shift+Enter or the second button) keeps the dialog for rapid entry.
  async function save(keepOpen: boolean) {
    if (!title.trim() || busy) return
    busy = true
    error = ''
    try {
      await oncreate({ title, type, estimate: pointable ? estimate : null, section: target }, keepOpen)
      title = ''
      titleInput?.focus()
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <form
    class="modal"
    aria-label="New story"
    onsubmit={(e) => {
      e.preventDefault()
      save(false)
    }}
  >
    <h2>New story</h2>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      bind:this={titleInput}
      bind:value={title}
      placeholder="As a user, I can…"
      maxlength="500"
      autofocus
      onkeydown={(e) => {
        if (e.key === 'Enter' && e.shiftKey) {
          e.preventDefault()
          save(true)
        }
      }}
    />
    <div class="options">
      <select bind:value={type} aria-label="Type">
        <option value="feature">★ Feature</option>
        <option value="bug">● Bug</option>
        <option value="chore">⚙ Chore</option>
      </select>
      <select bind:value={target} aria-label="Panel">
        {#each ['icebox', 'backlog', 'current'] as const as s (s)}
          <option value={s}>{SECTION_TITLES[s]}</option>
        {/each}
      </select>
      {#if pointable}
        <span class="estimates" role="group" aria-label="Estimate">
          {#each ESTIMATES as pts (pts)}
            <button type="button" class:active={estimate === pts} onclick={() => (estimate = estimate === pts ? null : pts)}>
              {pts}
            </button>
          {/each}
        </span>
      {/if}
    </div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row-actions">
      <span class="muted"><kbd>Enter</kbd> save · <kbd>Shift+Enter</kbd> save &amp; add another</span>
      <button type="button" onclick={onclose}>Cancel</button>
      <button type="button" disabled={busy || !title.trim()} onclick={() => save(true)}>Save &amp; add another</button>
      <button class="primary" disabled={busy || !title.trim()}>Save</button>
    </div>
  </form>
</div>

<style>
  .options {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  p {
    margin: 0;
  }
  .row-actions .muted {
    margin-right: auto;
    font-size: 11px;
  }
</style>
