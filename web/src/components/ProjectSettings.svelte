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
    onclose: () => void
  }
  let { project, canManage, users, members, onmembers, onsaved, onclose }: Props = $props()

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
  })
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
</style>
