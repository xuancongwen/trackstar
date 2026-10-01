<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import type { AuthorizeInfo, AuthorizeParams, User } from '../lib/types'

  interface Props {
    user: User
    onlogout: () => void
  }
  let { user, onlogout }: Props = $props()

  // The authorization request exactly as the client sent it. The server
  // validates it again when the decision is posted; nothing here is trusted.
  const query = new URLSearchParams(location.search)
  const params: AuthorizeParams = {
    client_id: query.get('client_id') ?? '',
    redirect_uri: query.get('redirect_uri') ?? '',
    response_type: query.get('response_type') ?? '',
    code_challenge: query.get('code_challenge') ?? '',
    code_challenge_method: query.get('code_challenge_method') ?? '',
    state: query.get('state') ?? '',
    resource: query.get('resource') ?? '',
    scope: query.get('scope') ?? '',
  }

  let info = $state<AuthorizeInfo | null>(null)
  let error = $state('')
  let busy = $state(false)

  // replace(), so Back from the client does not land on a spent request.
  const leave = (url: string) => location.replace(url)

  onMount(async () => {
    try {
      const res = await api.authorizeInfo(params)
      if (res.redirect_to) leave(res.redirect_to)
      else info = res
    } catch (err) {
      error = (err as Error).message
    }
  })

  async function decide(approve: boolean) {
    busy = true
    error = ''
    try {
      leave((await api.authorize(params, approve)).redirect_to)
    } catch (err) {
      error = (err as Error).message
      busy = false
    }
  }
</script>

<main>
  <section class="modal" aria-label="Connect an app">
    <h1>Trackstar</h1>
    {#if info}
      <h2>Connect {info.client_name}?</h2>
      <p>
        <strong>{info.client_name}</strong> is asking to act as you in Trackstar. It will be able to read and change
        every project you can reach: stories, comments, priorities.
      </p>
      <p>It cannot change your password, manage accounts or create API tokens.</p>
      <p class="muted small">
        If you approve, you are sent on to <strong>{info.redirect_host}</strong>. The name above is what the app calls
        itself, so approve only if you just started this from an app you trust. You can disconnect it at any time
        under your name ▸ Account.
      </p>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <div class="row-actions">
        <button type="button" disabled={busy} onclick={() => decide(false)}>Deny</button>
        <button type="button" class="primary" disabled={busy} onclick={() => decide(true)}>Approve</button>
      </div>
    {:else if error}
      <h2>Cannot connect this app</h2>
      <p class="error" role="alert">{error}</p>
    {:else}
      <p class="muted">Checking the request…</p>
    {/if}
    <p class="muted small who">
      Signed in as {user.email}.
      <button type="button" class="link" onclick={onlogout}>Use another account</button>
    </p>
  </section>
</main>

<style>
  main {
    min-height: 100%;
    display: grid;
    place-items: center;
    padding: 12px 0;
  }
  section {
    width: min(400px, calc(100vw - 24px));
  }
  h1 {
    margin: 0;
    font-size: 20px;
  }
  p {
    margin: 0;
  }
  strong {
    overflow-wrap: anywhere;
  }
  .small {
    font-size: 12px;
  }
  .who {
    border-top: 1px solid var(--border);
    padding-top: 8px;
  }
</style>
