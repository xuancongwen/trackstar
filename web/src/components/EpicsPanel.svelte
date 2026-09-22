<script lang="ts">
  import { api } from '../lib/api'
  import type { Epic } from '../lib/types'

  interface Props {
    projectId: number
    epics: Epic[]
    /** Currently filtered epic name, if any. */
    active: string | null
    canWrite: boolean
    onchanged: () => void
    onselect: (name: string | null) => void
  }
  let { projectId, epics, active, canWrite, onchanged, onselect }: Props = $props()

  let newName = $state('')
  let editing = $state<Epic | null>(null)
  let editName = $state('')
  let editDescription = $state('')
  let error = $state('')

  async function create(event: SubmitEvent) {
    event.preventDefault()
    if (!newName.trim()) return
    error = ''
    try {
      await api.createEpic(projectId, { name: newName })
      newName = ''
      onchanged()
    } catch (err) {
      error = (err as Error).message
    }
  }

  function startEdit(e: Epic) {
    editing = e
    editName = e.name
    editDescription = e.description
  }

  async function saveEdit(event: SubmitEvent) {
    event.preventDefault()
    if (!editing) return
    error = ''
    try {
      await api.updateEpic(editing.id, { name: editName, description: editDescription })
      if (active === editing.name) onselect(editName.trim().toLowerCase())
      editing = null
      onchanged()
    } catch (err) {
      error = (err as Error).message
    }
  }

  async function demote(e: Epic) {
    error = ''
    try {
      await api.demoteEpic(e.id)
      if (active === e.name) onselect(null)
      editing = null
      onchanged()
    } catch (err) {
      error = (err as Error).message
    }
  }

  const pct = (e: Epic) => (e.total_points > 0 ? Math.round((e.accepted_points / e.total_points) * 100) : 0)
</script>

<section class="panel" aria-label="Epics">
  <header>
    <h2>Epics</h2>
    <span class="summary">{epics.length}</span>
    {#if active}<button class="clear" onclick={() => onselect(null)} title="Show all stories">show all</button>{/if}
  </header>
  <div class="scroll">
    {#each epics as e (e.id)}
      {#if editing?.id === e.id}
        <form class="edit" onsubmit={saveEdit}>
          <input bind:value={editName} maxlength="50" required aria-label="Epic name" />
          <textarea bind:value={editDescription} rows="3" placeholder="What is this epic about?"></textarea>
          <div class="row-actions">
            <button type="button" class="danger link" onclick={() => demote(e)} title="Keeps the label on stories">Remove epic</button>
            <span class="spacer"></span>
            <button type="button" onclick={() => (editing = null)}>Cancel</button>
            <button class="primary">Save</button>
          </div>
        </form>
      {:else}
        <div class="epic" class:active={active === e.name}>
          <button class="name" onclick={() => onselect(active === e.name ? null : e.name)} title={e.description || 'Filter the board to this epic'}>
            {e.name}
          </button>
          <span class="points" title="accepted / total feature points">{e.accepted_points}/{e.total_points}</span>
          {#if canWrite}<button class="link edit-btn" onclick={() => startEdit(e)} aria-label={`Edit epic ${e.name}`}>✎</button>{/if}
          <div class="bar" title={`${pct(e)}% of points accepted · ${e.accepted_count}/${e.story_count} stories`}>
            <div style:width={`${pct(e)}%`}></div>
          </div>
          {#if e.description}<p class="muted">{e.description}</p>{/if}
        </div>
      {/if}
    {:else}
      <p class="hint muted">No epics yet. An epic is a label with a progress bar: add one below and give stories that label.</p>
    {/each}
  </div>
  {#if canWrite}
    <form class="new" onsubmit={create}>
      <input bind:value={newName} placeholder="New epic name" maxlength="50" aria-label="New epic name" />
      <button class="primary" disabled={!newName.trim()}>Add</button>
    </form>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
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
  }
  .clear {
    background: transparent;
    color: inherit;
    border-color: rgba(255, 255, 255, 0.35);
    padding: 0 7px;
    font-size: 11px;
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
  }
  .epic {
    display: grid;
    grid-template-columns: 1fr auto auto;
    align-items: center;
    gap: 2px 6px;
    padding: 6px 8px;
    border-bottom: 1px solid var(--border);
    background: var(--row);
  }
  .epic.active {
    box-shadow: inset 3px 0 0 var(--accent);
    background: var(--row-hover);
  }
  .name {
    text-align: left;
    border: 0;
    background: none;
    padding: 0;
    font-weight: 600;
    color: var(--epic);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .points {
    font-size: 11px;
    font-weight: 600;
  }
  .edit-btn {
    font-size: 12px;
  }
  .bar {
    grid-column: 1 / -1;
    height: 5px;
    border-radius: 3px;
    background: var(--border);
    overflow: hidden;
  }
  .bar div {
    height: 100%;
    background: var(--accept);
  }
  .epic p {
    grid-column: 1 / -1;
    margin: 0;
    font-size: 11px;
    white-space: pre-wrap;
  }
  .edit {
    display: grid;
    gap: 6px;
    padding: 8px;
    border-bottom: 1px solid var(--border);
  }
  .edit .spacer {
    flex: 1;
  }
  .edit textarea {
    resize: vertical;
  }
  .new {
    display: flex;
    gap: 6px;
    padding: 6px 8px;
    border-top: 1px solid var(--border);
  }
  .new input {
    flex: 1;
    min-width: 0;
  }
  .hint {
    margin: 0;
    padding: 12px;
    font-size: 12px;
    text-align: center;
  }
  .error {
    margin: 0;
    padding: 4px 8px;
    font-size: 12px;
  }
</style>
