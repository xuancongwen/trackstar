<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import type { ApiToken, CreatedToken, User } from '../lib/types'

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

  // Personal API tokens (for scripts and MCP clients). The secret is shown
  // once, right after creation, and never again.
  let tokens = $state<ApiToken[]>([])
  let tokenName = $state('')
  let tokenExpiry = $state('0')
  let newToken = $state<CreatedToken | null>(null)
  let tokenError = $state('')
  let copied = $state(false)

  onMount(async () => {
    try {
      tokens = await api.tokens()
    } catch (err) {
      tokenError = (err as Error).message
    }
  })

  async function createToken() {
    tokenError = ''
    copied = false
    try {
      newToken = await api.createToken(tokenName, Number(tokenExpiry))
      tokens = [newToken, ...tokens]
      tokenName = ''
    } catch (err) {
      tokenError = (err as Error).message
    }
  }

  async function revokeToken(id: number) {
    tokenError = ''
    try {
      await api.revokeToken(id)
      tokens = tokens.filter((t) => t.id !== id)
      if (newToken?.id === id) newToken = null
    } catch (err) {
      tokenError = (err as Error).message
    }
  }

  async function copyToken() {
    if (!newToken) return
    try {
      await navigator.clipboard.writeText(newToken.token)
      copied = true
    } catch {
      copied = false
    }
  }

  function when(iso: string | null): string {
    if (!iso) return 'never'
    return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
  }

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
    <fieldset class="tokens">
      <legend>API tokens</legend>
      <p class="muted small">
        A token acts as you for scripts and MCP clients: send it as <code>Authorization: Bearer …</code>.
        Tokens cannot change passwords or manage accounts.
      </p>
      <div class="new-token">
        <input
          placeholder="Token name, e.g. claude-code"
          bind:value={tokenName}
          maxlength="100"
          aria-label="Token name"
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              if (tokenName.trim()) createToken()
            }
          }}
        />
        <select bind:value={tokenExpiry} aria-label="Token expiry">
          <option value="0">No expiry</option>
          <option value="30">30 days</option>
          <option value="90">90 days</option>
          <option value="365">1 year</option>
        </select>
        <button type="button" onclick={createToken} disabled={!tokenName.trim()}>Create</button>
      </div>
      {#if newToken}
        <div class="reveal" role="status">
          <p class="small">Copy your new token now. It will not be shown again.</p>
          <div class="secret">
            <code>{newToken.token}</code>
            <button type="button" class="link" onclick={copyToken}>{copied ? 'Copied' : 'Copy'}</button>
          </div>
        </div>
      {/if}
      {#if tokenError}<p class="error" role="alert">{tokenError}</p>{/if}
      {#if tokens.length}
        <ul class="token-list">
          {#each tokens as t (t.id)}
            <li>
              <span class="name">{t.name}</span>
              <span class="muted small">
                created {when(t.created_at)} · last used {when(t.last_used_at)}{t.expires_at ? ` · expires ${when(t.expires_at)}` : ''}
              </span>
              <button type="button" class="link danger" onclick={() => revokeToken(t.id)}>Revoke</button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted small">No tokens yet.</p>
      {/if}
    </fieldset>
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
  .small {
    font-size: 12px;
  }
  .tokens code {
    font-size: 12px;
  }
  .new-token {
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 6px;
  }
  .reveal {
    display: grid;
    gap: 4px;
    padding: 8px;
    border-radius: 4px;
    background: var(--row);
  }
  .secret {
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
  }
  .secret code {
    word-break: break-all;
    user-select: all;
  }
  .token-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  .token-list li {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0 8px;
    align-items: center;
  }
  .token-list .name {
    font-weight: 600;
  }
  .token-list .muted {
    grid-column: 1;
  }
  .token-list button {
    grid-row: 1 / span 2;
    grid-column: 2;
  }
</style>
