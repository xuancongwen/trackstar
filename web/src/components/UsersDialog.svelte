<script lang="ts">
  import { api } from '../lib/api'
  import type { User } from '../lib/types'

  interface Props {
    me: User
    users: User[]
    onchanged: (users: User[]) => void
    onclose: () => void
  }
  let { me, users, onchanged, onclose }: Props = $props()

  let error = $state('')
  let resetting = $state<number | null>(null)
  let newPassword = $state('')
  let notice = $state('')

  async function update(u: User, input: Parameters<typeof api.updateUser>[1]) {
    error = ''
    try {
      const updated = await api.updateUser(u.id, input)
      onchanged(users.map((x) => (x.id === u.id ? updated : x)))
    } catch (err) {
      error = (err as Error).message
    }
  }

  async function resetPassword(u: User) {
    error = ''
    try {
      await api.setUserPassword(u.id, newPassword)
      notice = `Password for ${u.display_name} changed; they were signed out everywhere.`
      resetting = null
      newPassword = ''
    } catch (err) {
      error = (err as Error).message
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <div class="modal wide" role="dialog" aria-label="Users">
    <h2>Users</h2>
    <p class="muted">
      Deactivated users cannot sign in but stay attached to their stories. New accounts are created by
      registration (TRACKER_ALLOW_REGISTRATION).
    </p>
    <table>
      <thead>
        <tr><th>Name</th><th>Email</th><th>Role</th><th>Status</th><th></th></tr>
      </thead>
      <tbody>
        {#each users as u (u.id)}
          <tr class:inactive={!u.is_active}>
            <td>{u.display_name}{u.id === me.id ? ' (you)' : ''}</td>
            <td class="muted">{u.email}</td>
            <td>
              <button class="link" onclick={() => update(u, { is_admin: !u.is_admin })}>
                {u.is_admin ? 'admin' : 'member'} ⇄
              </button>
            </td>
            <td>
              <button class="link" class:danger={u.is_active} onclick={() => update(u, { is_active: !u.is_active })}>
                {u.is_active ? 'deactivate' : 'reactivate'}
              </button>
            </td>
            <td>
              {#if resetting === u.id}
                <form
                  class="inline"
                  onsubmit={(e) => {
                    e.preventDefault()
                    resetPassword(u)
                  }}
                >
                  <!-- svelte-ignore a11y_autofocus -->
                  <input type="text" bind:value={newPassword} minlength="8" maxlength="72" placeholder="new password" autofocus />
                  <button class="primary" disabled={newPassword.length < 8}>Set</button>
                  <button type="button" onclick={() => (resetting = null)}>Cancel</button>
                </form>
              {:else}
                <button class="link" onclick={() => (resetting = u.id)}>reset password</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if notice}<p class="ok" role="status">{notice}</p>{/if}
    <div class="row-actions"><button onclick={onclose}>Close</button></div>
  </div>
</div>

<style>
  .wide {
    width: min(760px, calc(100vw - 24px));
  }
  p {
    margin: 0;
    font-size: 12px;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  th {
    text-align: left;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
    padding: 4px 6px;
  }
  td {
    padding: 5px 6px;
    border-top: 1px solid var(--border);
    vertical-align: middle;
  }
  tr.inactive td {
    opacity: 0.55;
  }
  .link.danger {
    color: var(--danger);
  }
  .inline {
    display: flex;
    gap: 4px;
  }
  .inline input {
    width: 150px;
    padding: 2px 6px;
  }
  .ok {
    color: var(--accept);
  }
</style>
