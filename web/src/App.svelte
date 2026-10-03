<script lang="ts">
  import { onMount } from 'svelte'
  import { api, ApiError } from './lib/api'
  import { setIterationTimeZone } from './lib/format'
  import type { User } from './lib/types'
  import Board from './components/Board.svelte'
  import Consent from './components/Consent.svelte'
  import Login from './components/Login.svelte'
  import Projects from './components/Projects.svelte'

  let user = $state<User | null>(null)
  let booting = $state(true)
  let bootError = $state('')
  let slug = $state<string | null>(null)
  let linkError = $state('')

  // Routes: #/ (projects), #/p/<slug> (board) and #/p/<slug>/s/<id> (board
  // with a story open, handled by the board). Hash routing needs no server
  // cooperation and keeps the app dependency-free.
  //
  // #/s/<id> links a story without naming its project, which is what story
  // references in text use; it is looked up and replaced by the board route.
  // The current board stays up meanwhile, so a link within the project does
  // not reload it.
  function route() {
    linkError = ''
    const storyId = location.hash.match(/^#\/s\/(\d+)$/)?.[1]
    if (storyId) resolveStoryLink(Number(storyId))
    else slug = location.hash.match(/^#\/p\/([^/]+)/)?.[1] ?? null
  }
  async function resolveStoryLink(id: number) {
    try {
      const story = await api.story(id)
      const project = await api.project(story.project_id)
      location.replace(`#/p/${project.slug}/s/${id}`)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) user = null
      else linkError = `Story #${id} is not available`
    }
  }
  // Routing needs a session, to look up story links.
  let signedIn = $derived(user !== null)
  $effect(() => {
    if (signedIn) route()
  })

  // The one path route: an MCP client sends the browser to /oauth/authorize
  // to ask for access. Signing in first keeps the URL, so the request
  // survives the login form.
  const authorizing = location.pathname === '/oauth/authorize'

  onMount(() => {
    const onHash = () => {
      if (user) route()
    }
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
    if (!authorizing) location.hash = '#/'
  }
</script>

{#if linkError}
  <p class="link-error error" role="alert">{linkError} <button class="link" onclick={() => (linkError = '')}>dismiss</button></p>
{/if}

{#if booting}
  <p class="boot muted">Loading…</p>
{:else if bootError}
  <p class="boot error">Cannot reach the server: {bootError}</p>
{:else if !user}
  <Login onlogin={(u) => (user = u)} />
{:else if authorizing}
  <Consent {user} onlogout={logout} />
{:else if slug}
  {#key slug}
    <Board {slug} {user} onlogout={logout} onunauthorized={() => (user = null)} onuserchanged={(u) => (user = u)} />
  {/key}
{:else}
  <Projects {user} onlogout={logout} onuserchanged={(u) => (user = u)} />
{/if}

<style>
  .link-error {
    position: fixed;
    z-index: 100;
    top: 8px;
    left: 50%;
    transform: translateX(-50%);
    margin: 0;
    padding: 6px 12px;
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  .boot {
    padding: 40px;
    text-align: center;
  }
</style>
