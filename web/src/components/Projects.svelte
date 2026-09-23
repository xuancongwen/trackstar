<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { WEEKDAYS } from '../lib/format'
  import type { Project, User } from '../lib/types'

  let { user, onlogout }: { user: User; onlogout: () => void } = $props()

  let projects = $state<Project[]>([])
  let active = $derived(projects.filter((p) => !p.archived_at))
  let archived = $derived(projects.filter((p) => p.archived_at))
  let loaded = $state(false)
  let name = $state('')
  let error = $state('')

  onMount(async () => {
    try {
      projects = await api.projects()
    } catch (err) {
      error = (err as Error).message
    }
    loaded = true
  })

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
</script>

<header>
  <strong>Trackstar</strong>
  <span class="spacer"></span>
  <span class="muted">{user.display_name}</span>
  <button onclick={onlogout}>Sign out</button>
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
          <span class="muted">
            {p.iteration_length_days / 7}-week iterations starting {WEEKDAYS[p.iteration_start_weekday]}
          </span>
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
</main>

<style>
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    background: var(--panel-head);
    color: var(--panel-head-text);
  }
  header .muted {
    color: inherit;
    opacity: 0.75;
  }
  .spacer {
    flex: 1;
  }
  main {
    max-width: 560px;
    margin: 28px auto;
    padding: 0 14px;
  }
  h1 {
    font-size: 18px;
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
