<script lang="ts">
  import { api } from '../lib/api'
  import type { User } from '../lib/types'

  interface Props {
    user: User
    onsaved: (user: User) => void
    onclose: () => void
  }
  let { user, onsaved, onclose }: Props = $props()

  // svelte-ignore state_referenced_locally
  let displayName = $state(user.display_name)
  let currentPassword = $state('')
  let newPassword = $state('')
  let error = $state('')
  let saved = $state('')

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    error = ''
    saved = ''
    const input: Parameters<typeof api.updateMe>[0] = {}
    if (displayName.trim() !== user.display_name) input.display_name = displayName
    if (newPassword) {
      input.current_password = currentPassword
      input.new_password = newPassword
    }
    if (Object.keys(input).length === 0) return onclose()
    try {
      const u = await api.updateMe(input)
      onsaved(u)
      currentPassword = newPassword = ''
      saved = input.new_password ? 'Saved. Other devices were signed out.' : 'Saved.'
    } catch (err) {
      error = (err as Error).message
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <form class="modal" onsubmit={submit} aria-label="Account">
    <h2>Account</h2>
    <p class="muted">{user.email}{user.is_admin ? ' · administrator' : ''}</p>
    <label class="field"><span>Display name</span><input bind:value={displayName} required maxlength="100" /></label>
    <fieldset>
      <legend>Change password</legend>
      <label class="field">
        <span>Current password</span>
        <input type="password" bind:value={currentPassword} autocomplete="current-password" />
      </label>
      <label class="field">
        <span>New password (8–72 characters)</span>
        <input type="password" bind:value={newPassword} minlength="8" maxlength="72" autocomplete="new-password" />
      </label>
    </fieldset>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if saved}<p class="ok" role="status">{saved}</p>{/if}
    <div class="row-actions">
      <button type="button" onclick={onclose}>Close</button>
      <button class="primary">Save</button>
    </div>
  </form>
</div>

<style>
  p {
    margin: 0;
  }
  fieldset {
    border: 1px solid var(--border);
    border-radius: 4px;
    display: grid;
    gap: 8px;
    padding: 8px 10px 10px;
  }
  legend {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .ok {
    color: var(--accept);
  }
</style>
