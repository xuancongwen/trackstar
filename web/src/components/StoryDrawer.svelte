<script lang="ts">
  import { api } from '../lib/api'
  import { ESTIMATES, nextActions } from '../lib/board'
  import { formatDayTime } from '../lib/format'
  import type { Comment, Story, StoryPatch, StoryType, User } from '../lib/types'

  interface Props {
    story: Story
    users: Map<number, User>
    me: User
    onpatch: (id: number, patch: StoryPatch) => Promise<boolean>
    ondelete: (id: number) => Promise<void>
    oncommented: (id: number, delta: number) => void
    onclose: () => void
  }
  let { story, users, me, onpatch, ondelete, oncommented, onclose }: Props = $props()

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
  let commentBody = $state('')
  let confirmDelete = $state(false)
  let error = $state('')

  $effect(() => {
    const id = story.id
    comments = []
    confirmDelete = false
    api
      .story(id)
      .then((d) => {
        if (d.id === story.id) comments = d.comments
      })
      .catch((err) => (error = err.message))
  })

  let locked = $derived(story.state === 'accepted')
  let userList = $derived([...users.values()])

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
    if (!commentBody.trim()) return
    try {
      comments = [...comments, await api.addComment(story.id, commentBody)]
      commentBody = ''
      oncommented(story.id, 1)
    } catch (err) {
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
    <span class="muted">#{story.id} · {story.state}</span>
    <span class="spacer"></span>
    {#each nextActions(story) as action (action.state)}
      <button class="primary" onclick={() => onpatch(story.id, { state: action.state })}>{action.label}</button>
    {/each}
    {#if story.state === 'started'}
      <button onclick={() => onpatch(story.id, { state: 'unstarted' })} title="Back to unstarted">Unstart</button>
    {/if}
    <button onclick={onclose} title="Close (Esc)" aria-label="Close">✕</button>
  </header>

  <div class="content">
    <input
      class="title"
      aria-label="Title"
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
          onchange={(e) => onpatch(story.id, { requester_id: Number(e.currentTarget.value) })}
        >
          {#each userList as u (u.id)}<option value={u.id}>{u.display_name}</option>{/each}
        </select>
      </label>
    </div>

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
      <textarea bind:value={description} onblur={saveDescription} rows="7" placeholder="Add a description…"></textarea>
    </label>

    <label class="field">
      <span>Labels</span>
      <input
        bind:value={labels}
        onblur={saveLabels}
        onkeydown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
        placeholder="comma, separated"
      />
    </label>

    <section>
      <h3>Activity</h3>
      {#each comments as comment (comment.id)}
        <article>
          <div class="comment-head">
            <strong>{users.get(comment.user_id)?.display_name ?? 'Unknown'}</strong>
            <span class="muted">{formatDayTime(comment.created_at)}</span>
            {#if comment.user_id === me.id}
              <button class="link" onclick={() => removeComment(comment)}>delete</button>
            {/if}
          </div>
          <p>{comment.body}</p>
        </article>
      {/each}
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
    </section>

    {#if error}<p class="error" role="alert">{error}</p>{/if}

    <footer>
      <span class="muted">
        Requested {formatDayTime(story.created_at)}
        {#if story.accepted_at}· accepted {formatDayTime(story.accepted_at)}{/if}
      </span>
      {#if confirmDelete}
        <button class="danger" onclick={() => ondelete(story.id)}>Really delete</button>
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
  article {
    padding: 6px 8px;
    margin-bottom: 6px;
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
