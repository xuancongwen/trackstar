<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { timeAgo } from '../lib/format'
  import type { Project, ProjectStats, User } from '../lib/types'
  import AccountDialog from './AccountDialog.svelte'
  import ActivitySpark from './ActivitySpark.svelte'
  import UsersDialog from './UsersDialog.svelte'

  let { user, onlogout, onuserchanged }: { user: User; onlogout: () => void; onuserchanged: (user: User) => void } = $props()

  let menuOpen = $state(false)
  let showAccount = $state(false)
  let showUsers = $state(false)

  // How the active projects are ordered. A habit of whoever is looking, so it
  // stays in this browser instead of on the account.
  type SortBy = 'activity' | 'name'
  const SORT_KEY = 'trackstar.projects.sort'
  function storedSort(): SortBy {
    try {
      return localStorage.getItem(SORT_KEY) === 'name' ? 'name' : 'activity'
    } catch {
      return 'activity'
    }
  }
  let sortBy = $state<SortBy>(storedSort())
  function onSortChange() {
    try {
      localStorage.setItem(SORT_KEY, sortBy)
    } catch {
      // Private browsing: the choice lasts until the page is left.
    }
  }

  let projects = $state<Project[]>([])
  let byName = $derived(projects.filter((p) => !p.archived_at).sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })))
  // Busiest first: the sort is stable, so equally busy (or equally quiet)
  // projects stay in name order.
  let active = $derived(sortBy === 'name' ? byName : [...byName].sort((a, b) => recentChanges(b) - recentChanges(a)))
  let archived = $derived(projects.filter((p) => p.archived_at))

  // Typing narrows both lists by name; Enter opens the only project left.
  let search = $state('')
  let searchInput = $state<HTMLInputElement>()
  let needle = $derived(search.trim().toLowerCase())
  const matching = (list: Project[]) => (needle ? list.filter((p) => p.name.toLowerCase().includes(needle)) : list)
  let shownActive = $derived(matching(active))
  let shownArchived = $derived(matching(archived))
  let loaded = $state(false)
  let name = $state('')
  let error = $state('')

  // The overview: what is going on in each project. It is decoration on the
  // list, so the page works without it.
  let stats = $state(new Map<number, ProjectStats>())
  let userList = $state<User[]>([])
  const recentChanges = (p: Project) => (stats.get(p.id)?.activity ?? []).reduce((a, b) => a + b, 0)

  onMount(async () => {
    const overview = Promise.all([api.overview(), api.users()])
      .then(([o, us]) => {
        stats = new Map(o.projects.map((s) => [s.project_id, s]))
        userList = us
      })
      .catch(() => {})
    try {
      projects = await api.projects()
    } catch (err) {
      error = (err as Error).message
    }
    await overview
    loaded = true
  })

  /** "2 in progress · 1 to accept · 3h ago", leaving out what is zero. */
  function summary(p: Project): string {
    const s = stats.get(p.id)
    if (!s) return ''
    const parts: string[] = []
    if (s.in_progress > 0) parts.push(`${s.in_progress} in progress`)
    if (s.to_accept > 0) parts.push(`${s.to_accept} to accept`)
    if (s.last_activity_at) parts.push(timeAgo(s.last_activity_at))
    return parts.join(' · ')
  }

  async function create(event: SubmitEvent) {
    event.preventDefault()
    error = ''
    try {
      const p = await api.createProject({ name })
      location.hash = `#/p/${p.slug}`
    } catch (err) {
      error = (err as Error).message
    }
  }

  /** The users dialog lists people, so a changed display name has to reach it too. */
  function accountSaved(u: User) {
    onuserchanged(u)
    userList = userList.map((x) => (x.id === u.id ? u : x))
  }

  function onSearchKeydown(event: KeyboardEvent) {
    if (event.key !== 'Enter') return
    const found = [...shownActive, ...shownArchived]
    if (needle && found.length === 1) location.hash = `#/p/${found[0].slug}`
  }

  function onKeydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement
    const typing = target.matches('input, textarea, select') || target.isContentEditable
    if (event.key === '/' && !typing && !showAccount && !showUsers && !event.ctrlKey && !event.metaKey && !event.altKey) {
      searchInput?.focus()
      event.preventDefault()
      return
    }
    if (event.key !== 'Escape') return
    if (menuOpen) menuOpen = false
    else if (showAccount) showAccount = false
    else if (showUsers) showUsers = false
    else if (search || target === searchInput) {
      search = ''
      searchInput?.blur()
    } else return
    event.preventDefault()
  }
</script>

<svelte:window onkeydown={onKeydown} />

<header>
  <strong>Trackstar</strong>
  <span class="spacer"></span>
  <div class="menu">
    <button class:on={menuOpen} onclick={() => (menuOpen = !menuOpen)} aria-haspopup="menu" aria-expanded={menuOpen} aria-label="Menu">
      <span class="who">{user.display_name}</span> ▾
    </button>
    {#if menuOpen}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div class="menu-backdrop" onclick={() => (menuOpen = false)}></div>
      <div class="menu-items" role="menu">
        <button role="menuitem" onclick={() => ((showAccount = true), (menuOpen = false))}>Account…</button>
        {#if user.is_admin}
          <button role="menuitem" onclick={() => ((showUsers = true), (menuOpen = false))}>Users…</button>
        {/if}
        <button role="menuitem" onclick={onlogout}>Sign out</button>
      </div>
    {/if}
  </div>
</header>

<main>
  <div class="head">
    <h1>Projects</h1>
    {#if active.length > 1}
      <select bind:value={sortBy} onchange={onSortChange} aria-label="Sort projects">
        <option value="activity">Most active first</option>
        <option value="name">By name</option>
      </select>
    {/if}
    {#if projects.length > 0}
      <input
        class="search"
        type="search"
        bind:value={search}
        bind:this={searchInput}
        onkeydown={onSearchKeydown}
        placeholder="Search projects  ( / )"
        aria-label="Search projects"
        enterkeyhint="go"
      />
    {/if}
  </div>
  {#if loaded && projects.length === 0}
    <p class="muted">No projects yet. Create the first one below.</p>
  {:else if loaded && needle && shownActive.length + shownArchived.length === 0}
    <p class="muted" role="status">No project matches “{search.trim()}”.</p>
  {:else if loaded && active.length === 0}
    <p class="muted">No active projects. Create one below, or unarchive one from its settings.</p>
  {/if}
  <!-- Not before the overview is in: the order depends on it. -->
  <ul>
    {#each loaded ? shownActive : [] as p (p.id)}
      <li>
        <a href={`#/p/${p.slug}`}>
          <strong>{p.name}</strong>
          <span class="muted summary">{summary(p) || 'no activity yet'}</span>
          {#if stats.get(p.id)}<ActivitySpark days={stats.get(p.id)!.activity} />{/if}
        </a>
      </li>
    {/each}
  </ul>
  {#if shownArchived.length > 0}
    <!-- A search that finds an archived project should not leave it folded away. -->
    <details class="archived" open={needle !== ''}>
      <summary>Archived ({shownArchived.length})</summary>
      <ul>
        {#each shownArchived as p (p.id)}
          <li>
            <a href={`#/p/${p.slug}`}>
              <strong>{p.name}</strong>
              <span class="muted">read-only</span>
            </a>
          </li>
        {/each}
      </ul>
    </details>
  {/if}

  <form onsubmit={create}>
    <input bind:value={name} placeholder="New project name" required maxlength="100" />
    <button class="primary">Create project</button>
  </form>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</main>

{#if showAccount}
  <AccountDialog {user} onsaved={accountSaved} onclose={() => (showAccount = false)} />
{/if}
{#if showUsers}
  <UsersDialog me={user} users={userList} onchanged={(list) => (userList = list)} onclose={() => (showUsers = false)} />
{/if}

<style>
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    background: var(--panel-head);
    color: var(--panel-head-text);
  }
  .spacer {
    flex: 1;
  }
  .menu {
    position: relative;
    min-width: 0;
  }
  .menu > button {
    display: inline-flex;
    gap: 4px;
    max-width: 100%;
    background: transparent;
    border-color: rgba(255, 255, 255, 0.3);
  }
  .menu > button.on {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-text);
  }
  .who {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .menu-backdrop {
    position: fixed;
    inset: 0;
    z-index: 25;
  }
  .menu-items {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 26;
    display: grid;
    min-width: 150px;
    background: var(--panel);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: var(--shadow);
    padding: 4px;
  }
  .menu-items button {
    text-align: left;
    border: 0;
    color: var(--text);
    padding: 6px 10px;
  }
  .menu-items button:hover {
    background: var(--row-hover);
  }
  main {
    max-width: 560px;
    margin: 28px auto;
    padding: 0 14px calc(28px + env(safe-area-inset-bottom, 0px));
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 8px;
  }
  h1 {
    flex: 1;
    font-size: 18px;
  }
  .search {
    flex: 0 1 220px;
    min-width: 0;
  }
  ul {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  li a {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: var(--row);
    border: 1px solid var(--border);
    border-radius: 6px;
    text-decoration: none;
    color: inherit;
  }
  li a:hover {
    background: var(--row-hover);
  }
  li a strong {
    flex: 1;
    overflow-wrap: anywhere;
  }
  .summary {
    flex: none;
    text-align: right;
    font-size: 12px;
  }
  /* A phone has no room for the name and the summary side by side. */
  @media (max-width: 480px) {
    li a {
      flex-direction: column;
      align-items: flex-start;
      gap: 2px;
    }
    .summary {
      text-align: left;
    }
    .search {
      flex-basis: 100%;
    }
  }
  details.archived {
    margin-top: 16px;
  }
  details.archived summary {
    cursor: pointer;
    color: var(--muted);
    font-size: 13px;
    margin-bottom: 6px;
  }
  details.archived a {
    opacity: 0.75;
  }
  form {
    display: flex;
    gap: 8px;
    margin-top: 16px;
  }
  form input {
    flex: 1;
  }
</style>
