<script lang="ts">
  import { api } from '../lib/api'
  import { WEEKDAYS } from '../lib/format'
  import type { Member, Project, Role, User } from '../lib/types'

  interface Props {
    project: Project
    /** Owners and administrators may change settings and members; others only look. */
    canManage: boolean
    users: User[]
    members: Member[]
    onmembers: (members: Member[]) => void
    onsaved: (project: Project) => void
    /** The project is gone; the caller leaves the board. */
    ondeleted: () => void
    onclose: () => void
  }
  let { project, canManage, users, members, onmembers, onsaved, ondeleted, onclose }: Props = $props()

  let memberError = $state('')
  const roleOf = (userId: number): Role | '' => members.find((m) => m.user_id === userId)?.role ?? ''
  async function setRole(userId: number, role: Role | '') {
    memberError = ''
    try {
      if (role === '') {
        await api.removeMember(project.id, userId)
        onmembers(members.filter((m) => m.user_id !== userId))
      } else {
        onmembers(await api.setMember(project.id, userId, role))
      }
    } catch (err) {
      memberError = (err as Error).message
    }
  }
  // Managers pick from every active user; everyone else just sees who is in.
  let listed = $derived(canManage ? users.filter((u) => u.is_active || roleOf(u.id) !== '') : users.filter((u) => roleOf(u.id) !== ''))

  // svelte-ignore state_referenced_locally
  let form = $state({
    name: project.name,
    description: project.description,
    iteration_length_days: project.iteration_length_days,
    iteration_start_weekday: project.iteration_start_weekday,
    velocity_window: project.velocity_window,
    estimate_bugs_and_chores: project.estimate_bugs_and_chores,
  })
  // Unticking the option removes the points bugs and chores already have.
  let droppingPoints = $derived(project.estimate_bugs_and_chores && !form.estimate_bugs_and_chores)
  let error = $state('')

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    error = ''
    try {
      onsaved(await api.updateProject(project.id, form))
    } catch (err) {
      error = (err as Error).message
    }
  }

  // Danger zone: archiving flips read-only and is reversible; deleting is
  // not, so it asks for the project's name first.
  let dangerError = $state('')
  let confirmingDelete = $state(false)
  let confirmName = $state('')
  let busy = $state(false)
  let archived = $derived(project.archived_at != null)

  async function toggleArchive() {
    dangerError = ''
    busy = true
    try {
      onsaved(archived ? await api.unarchiveProject(project.id) : await api.archiveProject(project.id))
    } catch (err) {
      dangerError = (err as Error).message
    } finally {
      busy = false
    }
  }

  async function deleteProject() {
    if (confirmName.trim() !== project.name) return
    dangerError = ''
    busy = true
    try {
      await api.deleteProject(project.id)
      ondeleted()
    } catch (err) {
      dangerError = (err as Error).message
      busy = false
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <form class="modal" onsubmit={submit} aria-label="Project settings">
    <h2>Project settings</h2>
    <fieldset class="plain" disabled={!canManage}>
      <label class="field"><span>Name</span><input bind:value={form.name} required maxlength="100" /></label>
      <label class="field"><span>Description</span><textarea bind:value={form.description} rows="2"></textarea></label>
      <div class="grid">
        <label class="field">
          <span>Iteration length</span>
          <select bind:value={form.iteration_length_days}>
            {#each [7, 14, 21, 28] as days (days)}
              <option value={days}>{days / 7} week{days > 7 ? 's' : ''}</option>
            {/each}
          </select>
        </label>
        <label class="field">
          <span>Iterations start on</span>
          <select bind:value={form.iteration_start_weekday}>
            {#each WEEKDAYS as name, i (i)}<option value={i}>{name}</option>{/each}
          </select>
        </label>
        <label class="field">
          <span>Velocity window</span>
          <select bind:value={form.velocity_window}>
            {#each [1, 2, 3, 4, 5, 6, 8, 12] as n (n)}<option value={n}>last {n} iteration{n > 1 ? 's' : ''}</option>{/each}
          </select>
        </label>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={form.estimate_bugs_and_chores} />
        <span>Allow points on bugs and chores</span>
      </label>
      <p class="muted">
        {droppingPoints
          ? 'Points already on bugs and chores will be removed, and their history recounted.'
          : 'Off: only features are estimated. On: bugs and chores may carry points too, and they count towards velocity.'}
      </p>
    </fieldset>
    <p class="muted">
      {canManage
        ? 'Changing the iteration schedule renumbers past iterations; accepted stories keep their dates.'
        : 'Only a project owner or an administrator can change these settings.'}
    </p>

    <fieldset>
      <legend>Members</legend>
      <p class="muted">
        {#if members.length === 0}
          No members: every signed-in user can see and edit this project. An administrator can add an owner to make it members-only.
        {:else if canManage}
          Members-only. Owners manage members and settings; administrators always have access.
        {:else}
          Members-only. Ask an owner to add someone.
        {/if}
      </p>
      <div class="members">
        {#each listed as u (u.id)}
          <label>
            <span>{u.display_name}{u.is_admin ? ' (admin)' : ''}</span>
            {#if canManage}
              <select value={roleOf(u.id)} onchange={(e) => setRole(u.id, e.currentTarget.value as Role | '')}>
                <option value="">—</option>
                <option value="member">member</option>
                <option value="owner">owner</option>
              </select>
            {:else}
              <span class="muted">{roleOf(u.id)}</span>
            {/if}
          </label>
        {/each}
      </div>
      {#if memberError}<p class="error" role="alert">{memberError}</p>{/if}
    </fieldset>
    {#if canManage}
      <fieldset class="danger">
        <legend>Archive or delete</legend>
        <div class="danger-row">
          <p class="muted">
            {archived
              ? 'Archived: nobody can change stories. Unarchive to resume work.'
              : 'Archiving keeps everything readable but stops all changes. It can be undone.'}
          </p>
          <button type="button" onclick={toggleArchive} disabled={busy}>{archived ? 'Unarchive' : 'Archive'}</button>
        </div>
        <div class="danger-row">
          <p class="muted">Deleting removes the project and all of its stories, epics, comments and history. This cannot be undone.</p>
          {#if !confirmingDelete}
            <button type="button" class="destructive" onclick={() => (confirmingDelete = true)} disabled={busy}>Delete…</button>
          {/if}
        </div>
        {#if confirmingDelete}
          <div class="confirm">
            <label class="field">
              <span>Type <b>{project.name}</b> to confirm</span>
              <!-- svelte-ignore a11y_autofocus -->
              <input bind:value={confirmName} autofocus placeholder={project.name} onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), deleteProject())} />
            </label>
            <div class="row-actions">
              <button type="button" onclick={() => ((confirmingDelete = false), (confirmName = ''))}>Keep project</button>
              <button type="button" class="destructive" onclick={deleteProject} disabled={busy || confirmName.trim() !== project.name}>
                Delete project
              </button>
            </div>
          </div>
        {/if}
        {#if dangerError}<p class="error" role="alert">{dangerError}</p>{/if}
      </fieldset>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row-actions">
      <button type="button" onclick={onclose}>{canManage ? 'Cancel' : 'Close'}</button>
      {#if canManage}<button class="primary">Save</button>{/if}
    </div>
  </form>
</div>

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  p {
    margin: 0;
    font-size: 12px;
  }
  fieldset {
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 8px 10px 10px;
    display: grid;
    gap: 6px;
  }
  fieldset.plain {
    border: 0;
    padding: 0;
    margin: 0;
    min-width: 0;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
  }
  .check input {
    margin: 0;
  }
  legend {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  @media (max-width: 600px) {
    .grid,
    .members {
      grid-template-columns: 1fr !important;
    }
  }
  .members {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 4px 12px;
    max-height: 180px;
    overflow-y: auto;
  }
  .members label {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }
  fieldset.danger {
    border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
  }
  .danger-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
  }
  .danger-row button {
    flex-shrink: 0;
  }
  .confirm {
    display: grid;
    gap: 6px;
    padding-top: 4px;
  }
  button.destructive {
    color: var(--danger);
    border-color: currentColor;
  }
  button.destructive:disabled {
    opacity: 0.5;
  }
</style>
