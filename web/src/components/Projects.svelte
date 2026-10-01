<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { feedPhrase, foldCreated, withoutEchoes, type FeedLine } from '../lib/activity'
  import { timeAgo } from '../lib/format'
  import type { Project, ProjectStats, User } from '../lib/types'
  import AccountDialog from './AccountDialog.svelte'
  import UsersDialog from './UsersDialog.svelte'

  let { user, onlogout, onuserchanged }: { user: User; onlogout: () => void; onuserchanged: (user: User) => void } = $props()

  let menuOpen = $state(false)
  let showAccount = $state(false)
  let showUsers = $state(false)

  let projects = $state<Project[]>([])
  let active = $derived(projects.filter((p) => !p.archived_at))
  let archived = $derived(projects.filter((p) => p.archived_at))
  let loaded = $state(false)
  let name = $state('')
  let error = $state('')

  // The overview: what is going on in each project and what happened lately.
  // It is decoration on the list, so the page works without it.
  let stats = $state(new Map<number, ProjectStats>())
  const FEED_LINES = 20
  let feed = $state<FeedLine[]>([])
  let userList = $state<User[]>([])
  let users = $derived(new Map(userList.map((u) => [u.id, u])))
  let projectsById = $derived(new Map(projects.map((p) => [p.id, p])))
  const userName = (id: string | number) => users.get(Number(id))?.display_name ?? 'Someone'

  onMount(async () => {
    const overview = Promise.all([api.overview(), api.users()])
      .then(([o, us]) => {
        stats = new Map(o.projects.map((s) => [s.project_id, s]))
        userList = us
        feed = foldCreated(withoutEchoes(o.activity)).slice(0, FEED_LINES)
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

  const excerpt = (body: string) => (body.length > 140 ? body.slice(0, 140).trimEnd() + '…' : body)

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

  /** The feed names people, so a changed display name has to reach it too. */
  function accountSaved(u: User) {
    onuserchanged(u)
    userList = userList.map((x) => (x.id === u.id ? u : x))
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return
    if (menuOpen) menuOpen = false
    else if (showAccount) showAccount = false
    else if (showUsers) showUsers = false
    else return
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
  <h1>Projects</h1>
  {#if loaded && projects.length === 0}
    <p class="muted">No projects yet. Create the first one below.</p>
  {:else if loaded && active.length === 0}
    <p class="muted">No active projects. Create one below, or unarchive one from its settings.</p>
  {/if}
  <ul>
    {#each active as p (p.id)}
      <li>
        <a href={`#/p/${p.slug}`}>
          <strong>{p.name}</strong>
          <span class="muted summary">{loaded ? summary(p) || 'no activity yet' : ''}</span>
        </a>
      </li>
    {/each}
  </ul>
  {#if archived.length > 0}
    <details class="archived">
      <summary>Archived ({archived.length})</summary>
      <ul>
        {#each archived as p (p.id)}
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

  {#if feed.length > 0}
    <section aria-label="Recent activity">
      <h2>Recent activity</h2>
      <ol class="feed">
        {#each feed as e (e.kind === 'comment' ? `c${e.id}` : `a${e.id}`)}
          {@const project = projectsById.get(e.project_id)}
          {@const phrase = feedPhrase(e, userName)}
          <li>
            <a href={project ? `#/p/${project.slug}` : '#/'}>
              <span class="what">
                <strong>{userName(e.user_id)}</strong>
                {#if e.stories > 1}
                  created <span class="story">{e.stories} stories</span>
                {:else}
                  {phrase.verb}
                  <span class="story">{e.story_title}</span>{phrase.suffix}
                {/if}
              </span>
              {#if e.kind === 'comment' && e.body}<span class="quote muted">{excerpt(e.body)}</span>{/if}
              <span class="muted where">{project?.name ?? 'Project'} · {timeAgo(e.created_at)}</span>
            </a>
          </li>
        {/each}
      </ol>
    </section>
  {/if}
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
  h1 {
    font-size: 18px;
  }
  h2 {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
    margin: 28px 0 6px;
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
  li a strong,
  .story,
  .quote {
    overflow-wrap: anywhere;
  }
  .summary {
    flex: none;
    text-align: right;
    font-size: 12px;
  }
  .feed {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0;
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: hidden;
  }
  .feed li + li {
    border-top: 1px solid var(--border);
  }
  .feed a {
    display: grid;
    gap: 2px;
    padding: 8px 12px;
    border: 0;
    border-radius: 0;
  }
  .story {
    font-weight: 600;
  }
  .quote {
    border-left: 2px solid var(--border);
    padding-left: 8px;
  }
  .where {
    font-size: 12px;
  }
  /* A phone has no room for the name and the summary side by side. */
  @media (max-width: 480px) {
    ul li a {
      flex-direction: column;
      gap: 2px;
    }
    .summary {
      text-align: left;
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
