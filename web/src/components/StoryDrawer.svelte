<script lang="ts">
  import { api } from '../lib/api'
  import { ESTIMATES, SECTION_TITLES, canDrop, nextActions } from '../lib/board'
  import { formatDayTime } from '../lib/format'
  import type { Activity, Comment, DropSection, Story, StoryPatch, StoryType, Task, User } from '../lib/types'

  interface Props {
    story: Story
    users: Map<number, User>
    me: User
    /** All loaded stories, to name blockers and offer them in the picker. */
    stories?: Story[]
    readOnly?: boolean
    onpatch: (id: number, patch: StoryPatch) => Promise<boolean>
    /** Append the story to a section; shown for sections it may move to. */
    onmove?: (id: number, section: DropSection) => void
    ondelete: (id: number) => Promise<void>
    onrestore: (id: number) => Promise<void>
    oncommented: (id: number, delta: number) => void
    onclose: () => void
  }
  let { story, users, me, stories = [], readOnly = false, onmove, onpatch, ondelete, onrestore, oncommented, onclose }: Props = $props()

  // Text fields are edited locally and saved on blur; they re-sync whenever
  // the story changes underneath (server response, background refresh).
  let title = $state('')
  let description = $state('')
  let labels = $state('')
  $effect(() => {
    title = story.title
    description = story.description
    labels = story.labels.join(', ')
  })

  let comments = $state<Comment[]>([])
  let activity = $state<Activity[]>([])
  let tasks = $state<Task[]>([])
  let newTask = $state('')
  let blockerPick = $state('')
  let commentBody = $state('')
  let confirmDelete = $state(false)
  let error = $state('')

  // Re-read comments and history whenever the story changes (own edits,
  // live updates from others) — the detail endpoint is cheap.
  $effect(() => {
    const id = story.id
    void story.updated_at
    api
      .story(id)
      .then((d) => {
        if (d.id !== story.id) return
        comments = d.comments
        activity = d.activity
        tasks = d.tasks
      })
      .catch((err) => (error = err.message))
  })
  $effect(() => {
    void story.id
    confirmDelete = false
  })

  let trashed = $derived(story.deleted_at != null)
  let frozen = $derived(trashed || readOnly)
  let locked = $derived(story.state === 'accepted' || frozen)
  let moveTargets = $derived(
    (['icebox', 'backlog', 'current'] as DropSection[]).filter((s) => s !== story.section && canDrop(story, s)),
  )
  let blockerTitle = (id: number) => stories.find((s) => s.id === id)?.title ?? `#${id}`
  let blockerCandidates = $derived(
    stories
      .filter((s) => s.id !== story.id && s.state !== 'accepted' && !story.blocked_by.includes(s.id))
      .sort((a, b) => a.title.localeCompare(b.title)),
  )

  async function addTask(event: SubmitEvent) {
    event.preventDefault()
    const description = newTask.trim()
    if (!description) return
    newTask = '' // clear right away so fast typing of the next task is not wiped
    try {
      const created = await api.addTask(story.id, description)
      tasks = [...tasks, created] // append after the await: a second add may have landed meanwhile
    } catch (err) {
      newTask = description
      error = (err as Error).message
    }
  }
  async function toggleTask(t: Task) {
    try {
      const updated = await api.updateTask(t.id, { done: !t.done })
      tasks = tasks.map((x) => (x.id === t.id ? updated : x))
    } catch (err) {
      error = (err as Error).message
    }
  }
  async function removeTask(t: Task) {
    try {
      await api.deleteTask(t.id)
      tasks = tasks.filter((x) => x.id !== t.id)
    } catch (err) {
      error = (err as Error).message
    }
  }
  async function moveTask(t: Task, delta: number) {
    const index = tasks.findIndex((x) => x.id === t.id) + delta
    if (index < 0 || index >= tasks.length) return
    try {
      await api.updateTask(t.id, { position: index })
      const next = tasks.filter((x) => x.id !== t.id)
      next.splice(index, 0, t)
      tasks = next
    } catch (err) {
      error = (err as Error).message
    }
  }
  function addBlocker() {
    const id = Number(blockerPick)
    blockerPick = ''
    if (id) onpatch(story.id, { blocked_by: [...story.blocked_by, id] })
  }
  function removeBlocker(id: number) {
    onpatch(story.id, { blocked_by: story.blocked_by.filter((b) => b !== id) })
  }
  let userList = $derived([...users.values()].filter((u) => u.is_active || u.id === story.owner_id || u.id === story.requester_id))

  // Comments and history interleaved by time.
  type Entry = { kind: 'comment'; at: string; comment: Comment } | { kind: 'activity'; at: string; activity: Activity }
  let timeline = $derived(
    [
      ...comments.map((c): Entry => ({ kind: 'comment', at: c.created_at, comment: c })),
      ...activity.map((a): Entry => ({ kind: 'activity', at: a.created_at, activity: a })),
    ].sort((a, b) => a.at.localeCompare(b.at)),
  )

  const name = (id: string | number | null | undefined) =>
    id === '' || id == null ? 'nobody' : (users.get(Number(id))?.display_name ?? `user ${id}`)
  function describe(a: Activity): string {
    switch (a.kind) {
      case 'created':
        return `created the story in ${a.new_value}`
      case 'title':
        return `renamed “${a.old_value}” to “${a.new_value}”`
      case 'type':
        return `changed type ${a.old_value} → ${a.new_value}`
      case 'estimate':
        return a.new_value === '' ? 'removed the estimate' : `estimated ${a.new_value} point${a.new_value === '1' ? '' : 's'}${a.old_value ? ` (was ${a.old_value})` : ''}`
      case 'owner':
        return `assigned ${name(a.new_value)}${a.old_value ? ` (was ${name(a.old_value)})` : ''}`
      case 'state':
        return `${a.old_value} → ${a.new_value}`
      case 'moved':
        return `moved from ${a.old_value} to ${a.new_value}`
      case 'deleted':
        return 'deleted the story'
      case 'restored':
        return 'restored the story'
      case 'reopened':
        return 'reopened the accepted story'
      default:
        return `${a.kind} ${a.old_value} → ${a.new_value}`
    }
  }

  function saveTitle() {
    if (title.trim() && title.trim() !== story.title) onpatch(story.id, { title })
    else title = story.title
  }
  function saveDescription() {
    if (description !== story.description) onpatch(story.id, { description })
  }
  function saveLabels() {
    const next = labels
      .split(',')
      .map((l) => l.trim().toLowerCase())
      .filter(Boolean)
    if (next.join(',') !== story.labels.join(',')) onpatch(story.id, { labels: next })
  }

  async function addComment(event: SubmitEvent) {
    event.preventDefault()
    const body = commentBody.trim()
    if (!body) return
    commentBody = ''
    try {
      const created = await api.addComment(story.id, body)
      comments = [...comments, created]
      oncommented(story.id, 1)
    } catch (err) {
      commentBody = body
      error = (err as Error).message
    }
  }

  async function removeComment(comment: Comment) {
    try {
      await api.deleteComment(comment.id)
      comments = comments.filter((c) => c.id !== comment.id)
      oncommented(story.id, -1)
    } catch (err) {
      error = (err as Error).message
    }
  }
</script>

<aside class="drawer" aria-label="Story details">
  <header>
    <span class="muted">#{story.id} · {trashed ? 'deleted' : story.state}</span>
    <span class="spacer"></span>
    {#if readOnly}
      <span class="muted">read-only</span>
    {:else if trashed}
      <button class="primary" onclick={() => onrestore(story.id)}>Restore</button>
    {:else}
      {#each nextActions(story) as action (action.state)}
        <button class="primary" onclick={() => onpatch(story.id, { state: action.state })}>{action.label}</button>
      {/each}
      {#if story.state === 'started'}
        <button onclick={() => onpatch(story.id, { state: 'unstarted' })} title="Back to unstarted">Unstart</button>
      {/if}
      {#if story.state === 'accepted' && me.is_admin}
        <button onclick={() => onpatch(story.id, { state: 'delivered' })} title="Admin: undo acceptance (affects velocity)">Reopen</button>
      {/if}
    {/if}
    <button onclick={onclose} title="Close (Esc)" aria-label="Close">✕</button>
  </header>

  <div class="content">
    <input
      class="title"
      aria-label="Title"
      disabled={frozen}
      bind:value={title}
      onblur={saveTitle}
      onkeydown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
    />

    <div class="grid">
      <label class="field">
        <span>Type</span>
        <select
          value={story.type}
          disabled={locked}
          onchange={(e) => onpatch(story.id, { type: e.currentTarget.value as StoryType })}
        >
          <option value="feature">★ Feature</option>
          <option value="bug">● Bug</option>
          <option value="chore">⚙ Chore</option>
        </select>
      </label>
      <label class="field">
        <span>Owner</span>
        <select
          value={story.owner_id ?? ''}
          disabled={frozen}
          onchange={(e) => onpatch(story.id, { owner_id: e.currentTarget.value ? Number(e.currentTarget.value) : null })}
        >
          <option value="">Unassigned</option>
          {#each userList as u (u.id)}<option value={u.id}>{u.display_name}</option>{/each}
        </select>
      </label>
      <label class="field">
        <span>Requester</span>
        <select
          value={story.requester_id}
          disabled={frozen}
          onchange={(e) => onpatch(story.id, { requester_id: Number(e.currentTarget.value) })}
        >
          {#each userList as u (u.id)}<option value={u.id}>{u.display_name}</option>{/each}
        </select>
      </label>
    </div>

    {#if onmove && !frozen && moveTargets.length}
      <div class="field">
        <span>Move to</span>
        <span class="move-targets">
          {#each moveTargets as s (s)}
            <button onclick={() => onmove(story.id, s)}>{SECTION_TITLES[s]}</button>
          {/each}
        </span>
      </div>
    {/if}

    <div class="field">
      <span>Estimate</span>
      <span class="estimates" role="group" aria-label="Estimate">
        {#each ESTIMATES as pts (pts)}
          <button
            class:active={story.estimate === pts}
            disabled={locked}
            onclick={() => onpatch(story.id, { estimate: story.estimate === pts ? null : pts })}>{pts}</button
          >
        {/each}
      </span>
    </div>

    <label class="field">
      <span>Description</span>
      <textarea bind:value={description} onblur={saveDescription} rows="7" placeholder="Add a description…" disabled={frozen}></textarea>
    </label>

    <label class="field">
      <span>Labels</span>
      <input
        bind:value={labels}
        disabled={frozen}
        onblur={saveLabels}
        onkeydown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
        placeholder="comma, separated"
      />
    </label>

    <section>
      <h3>Tasks {#if tasks.length}<span class="muted">{tasks.filter((t) => t.done).length}/{tasks.length}</span>{/if}</h3>
      <ul class="tasks">
        {#each tasks as t (t.id)}
          <li class:done={t.done}>
            <input type="checkbox" checked={t.done} disabled={frozen} onchange={() => toggleTask(t)} aria-label={t.description} />
            <span>{t.description}</span>
            {#if !frozen}
              <button class="link" onclick={() => moveTask(t, -1)} aria-label="Move task up">↑</button>
              <button class="link" onclick={() => moveTask(t, 1)} aria-label="Move task down">↓</button>
              <button class="link" onclick={() => removeTask(t)} aria-label="Delete task">✕</button>
            {/if}
          </li>
        {/each}
      </ul>
      {#if !frozen}
        <form class="inline" onsubmit={addTask}>
          <input bind:value={newTask} placeholder="Add a task…" maxlength="500" aria-label="New task" />
          <button disabled={!newTask.trim()}>Add</button>
        </form>
      {/if}
    </section>

    <section>
      <h3>Blocked by {#if story.blocked}<span class="blocked-tag">blocked</span>{/if}</h3>
      <ul class="blockers">
        {#each story.blocked_by as id (id)}
          {@const b = stories.find((s) => s.id === id)}
          <li class:cleared={b?.state === 'accepted' || !b}>
            <span>#{id} {blockerTitle(id)}{b?.state === 'accepted' ? ' (accepted)' : ''}</span>
            {#if !frozen}<button class="link" onclick={() => removeBlocker(id)} aria-label={`Remove blocker #${id}`}>✕</button>{/if}
          </li>
        {/each}
      </ul>
      {#if !frozen && blockerCandidates.length}
        <select bind:value={blockerPick} onchange={addBlocker} aria-label="Add blocker">
          <option value="">Add a blocking story…</option>
          {#each blockerCandidates as s (s.id)}<option value={s.id}>#{s.id} {s.title}</option>{/each}
        </select>
      {/if}
    </section>

    <section>
      <h3>Activity</h3>
      <ol class="timeline">
        {#each timeline as entry (entry.kind + (entry.kind === 'comment' ? entry.comment.id : entry.activity.id))}
          {#if entry.kind === 'comment'}
            <li>
              <article>
                <div class="comment-head">
                  <strong>{name(entry.comment.user_id)}</strong>
                  <span class="muted">{formatDayTime(entry.comment.created_at)}</span>
                  {#if entry.comment.user_id === me.id && !trashed}
                    <button class="link" onclick={() => removeComment(entry.comment)}>delete</button>
                  {/if}
                </div>
                <p>{entry.comment.body}</p>
              </article>
            </li>
          {:else}
            <li class="event muted">
              <strong>{name(entry.activity.user_id)}</strong>
              {describe(entry.activity)}
              <span class="when">{formatDayTime(entry.activity.created_at)}</span>
            </li>
          {/if}
        {/each}
      </ol>
      {#if !frozen}
      <form onsubmit={addComment}>
        <textarea
          bind:value={commentBody}
          rows="2"
          placeholder="Add a comment…  (Ctrl+Enter to post)"
          onkeydown={(e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) e.currentTarget.form?.requestSubmit()
          }}
        ></textarea>
        <button disabled={!commentBody.trim()}>Post comment</button>
      </form>
      {/if}
    </section>

    {#if error}<p class="error" role="alert">{error}</p>{/if}

    <footer>
      <span class="muted">
        Requested {formatDayTime(story.created_at)}
        {#if story.accepted_at}· accepted {formatDayTime(story.accepted_at)}{/if}
      </span>
      {#if readOnly}
        <span class="muted">You have read-only access to this project.</span>
      {:else if trashed}
        <span class="muted">In the trash; purged 30 days after deletion.</span>
      {:else if confirmDelete}
        <button class="danger" onclick={() => ondelete(story.id)}>Move to trash</button>
        <button onclick={() => (confirmDelete = false)}>Keep</button>
      {:else}
        <button class="danger" onclick={() => (confirmDelete = true)}>Delete story</button>
      {/if}
    </footer>
  </div>
</aside>

<style>
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(460px, 100vw);
    background: var(--panel);
    border-left: 1px solid var(--border);
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    z-index: 20;
  }
  header {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
  }
  .spacer {
    flex: 1;
  }
  .content {
    flex: 1;
    overflow-y: auto;
    padding: 12px;
    display: grid;
    gap: 12px;
    align-content: start;
  }
  .title {
    font-size: 15px;
    font-weight: 600;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .move-targets {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  @media (max-width: 600px) {
    .drawer {
      width: 100vw;
      border-left: 0;
    }
    .grid {
      grid-template-columns: 1fr;
    }
    header {
      flex-wrap: wrap;
    }
  }
  select,
  textarea {
    width: 100%;
  }
  textarea {
    resize: vertical;
  }
  h3 {
    margin: 0 0 6px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .tasks,
  .blockers {
    list-style: none;
    margin: 0 0 6px;
    padding: 0;
    display: grid;
    gap: 2px;
  }
  .tasks li,
  .blockers li {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }
  .tasks li span,
  .blockers li span {
    flex: 1;
  }
  .tasks li.done span {
    text-decoration: line-through;
    color: var(--muted);
  }
  .blockers li.cleared span {
    color: var(--muted);
  }
  .tasks .link,
  .blockers .link {
    color: var(--muted);
    font-size: 11px;
  }
  .inline {
    display: flex;
    gap: 6px;
  }
  .inline input {
    flex: 1;
  }
  .blocked-tag {
    font-size: 10px;
    color: var(--danger);
    text-transform: none;
    letter-spacing: 0;
  }
  .timeline {
    list-style: none;
    margin: 0 0 8px;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .event {
    font-size: 11px;
    padding: 1px 8px;
  }
  .event .when {
    margin-left: 6px;
    opacity: 0.7;
  }
  article {
    padding: 6px 8px;
    margin-bottom: 2px;
    background: var(--row);
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  article p {
    margin: 2px 0 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .comment-head {
    display: flex;
    gap: 8px;
    align-items: baseline;
    font-size: 11px;
  }
  .comment-head .link {
    margin-left: auto;
    font-size: 11px;
  }
  form {
    display: grid;
    gap: 6px;
    justify-items: end;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
  }
  footer .muted {
    flex: 1;
  }
</style>
