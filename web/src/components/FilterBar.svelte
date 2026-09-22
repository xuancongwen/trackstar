<script lang="ts">
  import { api } from '../lib/api'
  import { BUILT_IN_FILTERS } from '../lib/filter'
  import type { SavedFilter } from '../lib/types'

  interface Props {
    projectId: number
    query: string
    saved: SavedFilter[]
    onquery: (query: string) => void
    onsavedchanged: (saved: SavedFilter[]) => void
    /** Receives the input element so the board can focus it with "/" */
    oninput?: (el: HTMLInputElement) => void
  }
  let { projectId, query, saved, onquery, onsavedchanged, oninput }: Props = $props()

  let open = $state(false)
  let saving = $state(false)
  let saveName = $state('')
  let error = $state('')
  let input = $state<HTMLInputElement>()

  $effect(() => {
    if (input) oninput?.(input)
  })

  async function save(event: SubmitEvent) {
    event.preventDefault()
    error = ''
    try {
      const f = await api.saveFilter(projectId, saveName, query)
      onsavedchanged([...saved.filter((s) => s.name !== f.name), f].sort((a, b) => a.name.localeCompare(b.name)))
      saving = false
      saveName = ''
    } catch (err) {
      error = (err as Error).message
    }
  }

  async function remove(f: SavedFilter) {
    try {
      await api.deleteFilter(f.id)
      onsavedchanged(saved.filter((s) => s.id !== f.id))
    } catch (err) {
      error = (err as Error).message
    }
  }

  function pick(q: string) {
    onquery(q)
    open = false
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape' && open) {
      open = false
      saving = false
    }
  }}
/>

<div class="filter">
  <input
    class="search"
    type="search"
    placeholder="Filter: text, owner:me, type:bug, label:x  ( / )"
    bind:this={input}
    value={query}
    oninput={(e) => onquery(e.currentTarget.value)}
    onkeydown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
    aria-label="Filter stories"
  />
  <button class:on={open} onclick={() => (open = !open)} aria-haspopup="menu" aria-expanded={open} title="Saved filters">▾</button>
  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="menu-backdrop" onclick={() => (open = false)}></div>
    <div class="menu" role="menu">
      {#each BUILT_IN_FILTERS as f (f.name)}
        <button role="menuitem" onclick={() => pick(f.query)}><span>{f.name}</span><code>{f.query}</code></button>
      {/each}
      {#if saved.length}<div class="sep"></div>{/if}
      {#each saved as f (f.id)}
        <div class="saved">
          <button role="menuitem" onclick={() => pick(f.query)}><span>{f.name}</span><code>{f.query}</code></button>
          <button class="link" onclick={() => remove(f)} aria-label={`Delete filter ${f.name}`}>✕</button>
        </div>
      {/each}
      <div class="sep"></div>
      {#if saving}
        <form onsubmit={save}>
          <!-- svelte-ignore a11y_autofocus -->
          <input bind:value={saveName} placeholder="Name" maxlength="50" required autofocus />
          <button class="primary" disabled={!query.trim()}>Save</button>
        </form>
      {:else}
        <button role="menuitem" disabled={!query.trim()} onclick={() => (saving = true)}>Save current filter…</button>
      {/if}
      {#if error}<p class="error">{error}</p>{/if}
      <p class="help">
        Terms AND together. <code>owner:me</code> <code>requester:kim</code> <code>type:bug</code> <code>state:started</code>
        <code>estimate:3</code> <code>label:auth</code> <code>is:blocked</code> <code>is:unestimated</code> <code>has:tasks</code>
        <code>-type:chore</code> <code>"exact phrase"</code>
      </p>
    </div>
  {/if}
</div>

<style>
  .filter {
    position: relative;
    display: flex;
    gap: 2px;
  }
  .search {
    width: min(320px, 34vw);
    padding: 2px 8px;
    color: var(--text);
  }
  .filter > button {
    padding: 0 7px;
    background: transparent;
    color: inherit;
    border-color: rgba(255, 255, 255, 0.3);
  }
  .filter > button.on {
    background: var(--accent);
    color: var(--accent-text);
  }
  .menu-backdrop {
    position: fixed;
    inset: 0;
    z-index: 25;
  }
  .menu {
    position: absolute;
    left: 0;
    top: calc(100% + 4px);
    z-index: 26;
    min-width: 340px;
    max-width: 90vw;
    background: var(--panel);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: var(--shadow);
    padding: 4px;
    display: grid;
    gap: 2px;
  }
  .menu button[role='menuitem'] {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    text-align: left;
    border: 0;
    color: var(--text);
    padding: 5px 8px;
    width: 100%;
  }
  .menu button[role='menuitem']:hover {
    background: var(--row-hover);
  }
  .menu code {
    font-size: 11px;
    color: var(--muted);
  }
  .saved {
    display: flex;
    align-items: center;
  }
  .saved .link {
    padding: 0 8px;
    color: var(--muted);
  }
  .sep {
    border-top: 1px solid var(--border);
    margin: 3px 0;
  }
  form {
    display: flex;
    gap: 6px;
    padding: 2px 4px;
  }
  form input {
    flex: 1;
  }
  .help {
    margin: 4px 8px 4px;
    font-size: 11px;
    color: var(--muted);
    line-height: 1.8;
  }
  .help code {
    background: var(--row);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 0 4px;
    margin-right: 2px;
  }
  .error {
    margin: 0 8px;
    font-size: 12px;
  }
</style>
