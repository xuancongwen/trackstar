<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import type { User } from '../lib/types'

  let { onlogin }: { onlogin: (user: User) => void } = $props()

  let mode = $state<'login' | 'register'>('login')
  let allowRegistration = $state(false)
  let email = $state('')
  let password = $state('')
  let displayName = $state('')
  let error = $state('')
  let busy = $state(false)

  onMount(async () => {
    try {
      allowRegistration = (await api.config()).allow_registration
    } catch {
      allowRegistration = false
    }
  })

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    busy = true
    error = ''
    try {
      onlogin(mode === 'login' ? await api.login(email, password) : await api.register(email, password, displayName))
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<main>
  <form class="modal" onsubmit={submit}>
    <h1>Trackstar</h1>
    {#if mode === 'register'}
      <label class="field">
        <span>Name</span>
        <input bind:value={displayName} autocomplete="name" placeholder="Sam" />
      </label>
    {/if}
    <label class="field">
      <span>Email</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input type="email" bind:value={email} required autocomplete="email" autofocus />
    </label>
    <label class="field">
      <span>Password</span>
      <input
        type="password"
        bind:value={password}
        required
        minlength={mode === 'register' ? 8 : undefined}
        autocomplete={mode === 'login' ? 'current-password' : 'new-password'}
      />
    </label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button class="primary" disabled={busy}>{mode === 'login' ? 'Sign in' : 'Create account'}</button>
    {#if allowRegistration}
      <button type="button" class="link" onclick={() => (mode = mode === 'login' ? 'register' : 'login')}>
        {mode === 'login' ? 'Create an account' : 'I already have an account'}
      </button>
    {/if}
  </form>
</main>

<style>
  main {
    height: 100%;
    display: grid;
    place-items: center;
  }
  form {
    width: min(340px, calc(100vw - 24px));
  }
  h1 {
    margin: 0 0 4px;
    font-size: 20px;
  }
  p {
    margin: 0;
  }
</style>
