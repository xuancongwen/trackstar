<script lang="ts">
  import { onMount } from 'svelte'
  import { api, ApiError } from './lib/api'
  import { setIterationTimeZone } from './lib/format'
  import type { User } from './lib/types'
  import Board from './components/Board.svelte'
  import Login from './components/Login.svelte'
  import Projects from './components/Projects.svelte'

  let user = $state<User | null>(null)
  let booting = $state(true)
  let bootError = $state('')
  let hash = $state(location.hash)

  // Routes: #/ (projects) and #/p/<slug> (board). Hash routing needs no
  // server cooperation and keeps the app dependency-free.
  let slug = $derived(hash.match(/^#\/p\/([^/]+)/)?.[1] ?? null)

  onMount(() => {
    const onHash = () => (hash = location.hash)
    window.addEventListener('hashchange', onHash)
    api
      .config()
      .then((c) => setIterationTimeZone(c.timezone))
      .catch(() => {})
    api
      .me()
      .then((u) => (user = u))
      .catch((err) => {
        if (!(err instanceof ApiError && err.status === 401)) bootError = String(err.message ?? err)
      })
      .finally(() => (booting = false))
    return () => window.removeEventListener('hashchange', onHash)
  })

  async function logout() {
    await api.logout().catch(() => {})
    user = null
    location.hash = '#/'
  }
</script>

{#if booting}
  <p class="boot muted">Loading…</p>
{:else if bootError}
  <p class="boot error">Cannot reach the server: {bootError}</p>
{:else if !user}
  <Login onlogin={(u) => (user = u)} />
{:else if slug}
  {#key slug}
    <Board {slug} {user} onlogout={logout} onunauthorized={() => (user = null)} onuserchanged={(u) => (user = u)} />
  {/key}
{:else}
  <Projects {user} onlogout={logout} />
{/if}

<style>
  .boot {
    padding: 40px;
    text-align: center;
  }
</style>
