<script lang="ts">
  import { onMount } from 'svelte'
  import { api, ApiError } from '../lib/api'
  import {
    acceptedThisIteration,
    applyMove,
    bulkMoveRequest,
    canDrop,
    moveRequest,
    orderedSelection,
    projectBacklog,
    sectionStories,
    totalPoints,
    type BacklogRow,
  } from '../lib/board'
  import type { DropEvent } from '../lib/dragdrop'
  import { blockingSet, matches as matchesFilter, parseQuery } from '../lib/filter'
  import { connectLive, type LiveStatus } from '../lib/live'
  import { formatRange } from '../lib/format'
  import type { DropSection, Epic, Iteration, Member, NewStory, Project, SavedFilter, Story, StoryPatch, StoryState, User, Velocity } from '../lib/types'
  import AccountDialog from './AccountDialog.svelte'
  import DonePanel from './DonePanel.svelte'
  import EpicsPanel from './EpicsPanel.svelte'
  import FilterBar from './FilterBar.svelte'
  import TrashPanel from './TrashPanel.svelte'
  import UsersDialog from './UsersDialog.svelte'
  import Panel from './Panel.svelte'
  import ProjectSettings from './ProjectSettings.svelte'
  import QuickCreate from './QuickCreate.svelte'
  import StoryDrawer from './StoryDrawer.svelte'
  import StoryRow from './StoryRow.svelte'

  interface Props {
    slug: string
    user: User
    onlogout: () => void
    onunauthorized: () => void
    onuserchanged: (user: User) => void
  }
  let { slug, user, onlogout, onunauthorized, onuserchanged }: Props = $props()

  const SECTIONS: DropSection[] = ['icebox', 'backlog', 'current']
  // Live events do the real work; this poll is only a safety net.
  const REFRESH_MS = 5 * 60_000

  let project = $state<Project | null>(null)
  let stories = $state<Story[]>([])
  let doneStories = $state<Story[]>([])
  let userList = $state<User[]>([])
  let velocity = $state<Velocity | null>(null)
  let iterations = $state<Iteration[]>([])

  let selectedId = $state<number | null>(null)
  let openId = $state<number | null>(null)
  let creating = $state<DropSection | null>(null)
  let showSettings = $state(false)
  let showDone = $state(false)
  let showTrash = $state(false)
  let showEpics = $state(false)
  let epics = $state<Epic[]>([])
  let activeEpic = $state<string | null>(null)
  let savedFilters = $state<SavedFilter[]>([])
  let members = $state<Member[]>([])
  // Multi-selection for bulk moves (x / shift-click).
  let checked = $state(new Set<number>())
  // Phone layout: one panel at a time, chosen with the tab strip.
  let mobile = $state(false)
  type MobileTab = DropSection | 'done' | 'trash' | 'epics'
  let mobileTab = $state<MobileTab>('current')
  let trashStories = $state<Story[]>([])
  let showAccount = $state(false)
  let showUsers = $state(false)
  let menuOpen = $state(false)
  // Project switcher: the project name in the top bar opens a list of all
  // projects. Loaded lazily the first time it opens.
  let switcherOpen = $state(false)
  let allProjects = $state<Project[] | null>(null)
  // Stories changed by others in the last few seconds get a brief highlight.
  let recentIds = $state(new Set<number>())
  let showHelp = $state(false)
  let query = $state('')
  let busyIds = $state(new Set<number>())
  let dragging = $state(false)
  let liveStatus = $state<LiveStatus>('connecting')
  // A live update arrived while the user was mid-drag or a request was in flight.
  let refreshPending = $state(false)
  let toast = $state('')
  // Undo offer after a delete.
  let undo = $state<{ id: number; title: string } | null>(null)
  let undoTimer: ReturnType<typeof setTimeout> | undefined
  let loadError = $state('')
  let searchInput = $state<HTMLInputElement>()

  let users = $derived(new Map(userList.map((u) => [u.id, u])))
  let currentIteration = $derived(iterations.find((it) => it.current) ?? null)
  let readOnly = $derived(project?.can_write === false)
  let epicNames = $derived(new Set(epics.map((e) => e.name)))
  // The active filter: typed query plus the selected epic, evaluated locally.
  let terms = $derived(parseQuery(query + (activeEpic ? ` label:"${activeEpic}"` : '')))
  let filterCtx = $derived({ me: user, users, blocking: blockingSet(stories) })
  let filtering = $derived(terms.length > 0)
  let visible = $derived((s: Story) => !filtering || matchesFilter(s, terms, filterCtx))
  let lists = $derived({
    icebox: sectionStories(stories, 'icebox').filter(visible),
    backlog: sectionStories(stories, 'backlog').filter(visible),
    current: sectionStories(stories, 'current').filter(visible),
  })
  let accepted = $derived(acceptedThisIteration(stories).filter(visible))
  let openStory = $derived(
    openId === null
      ? null
      : (stories.find((s) => s.id === openId) ??
        doneStories.find((s) => s.id === openId) ??
        trashStories.find((s) => s.id === openId) ??
        null),
  )

  const storyRows = (list: Story[]): BacklogRow[] => list.map((story) => ({ kind: 'story', key: `story-${story.id}`, story }))
  let rows = $derived({
    icebox: storyRows(lists.icebox),
    // Projection markers only make sense for the complete, unfiltered backlog.
    backlog:
      !filtering && project && velocity && currentIteration
        ? projectBacklog(lists.backlog, velocity.velocity, currentIteration, project.iteration_length_days)
        : storyRows(lists.backlog),
    current: storyRows(lists.current),
  })

  let currentTotal = $derived(totalPoints(lists.current) + totalPoints(accepted))
  let blockedInCurrent = $derived(lists.current.filter((s) => s.blocked).length)
  let summaries = $derived({
    icebox: `${lists.icebox.length} stories`,
    backlog: `${totalPoints(lists.backlog)} pts · ${lists.backlog.length} stories`,
    current:
      (currentIteration ? `#${currentIteration.number} · ${formatRange(currentIteration.start_at, currentIteration.end_at)}` : '') +
      (blockedInCurrent ? ` · ⛔ ${blockedInCurrent} blocked` : ''),
  })

  function fail(err: unknown) {
    if (err instanceof ApiError && err.status === 401) return onunauthorized()
    toast = (err as Error).message
    setTimeout(() => (toast = ''), 5000)
  }

  async function loadStories() {
    if (!project) return
    stories = await api.stories(project.id)
    if (showDone) doneStories = await api.stories(project.id, { done: true })
    if (showTrash) trashStories = await api.stories(project.id, { deleted: true })
  }

  async function loadMeta() {
    if (!project) return
    ;[velocity, iterations, epics] = await Promise.all([api.velocity(project.id), api.iterations(project.id), api.epics(project.id)])
  }
  async function loadEpics() {
    if (project) epics = await api.epics(project.id).catch((err) => (fail(err), epics))
  }

  async function refresh() {
    try {
      await Promise.all([loadStories(), loadMeta()])
    } catch (err) {
      fail(err)
    }
  }

  // Refresh now, or as soon as the board is idle: replacing the list under a
  // drag or before an optimistic move has been confirmed would fight the user.
  function refreshWhenIdle(changed: number[] = []) {
    if (changed.length > 0) flash(changed)
    if (dragging || busyIds.size > 0) {
      refreshPending = true
      return
    }
    refreshPending = false
    refresh()
  }

  function flash(ids: number[]) {
    recentIds = new Set([...recentIds, ...ids])
    setTimeout(() => {
      const next = new Set(recentIds)
      for (const id of ids) next.delete(id)
      recentIds = next
    }, 2500)
  }
  $effect(() => {
    if (refreshPending && !dragging && busyIds.size === 0) refreshWhenIdle()
  })

  onMount(() => {
    let live: ReturnType<typeof connectLive> | undefined
    const mq = window.matchMedia('(max-width: 900px)')
    const onMq = () => (mobile = mq.matches)
    onMq()
    mq.addEventListener('change', onMq)
    ;(async () => {
      try {
        project = await api.project(slug)
        document.title = `${project.name} · Trackstar`
        userList = await api.users()
        await refresh()
        ;[savedFilters, members] = await Promise.all([api.filters(project.id), api.members(project.id)])
        live = connectLive({ projectId: project.id, onChange: refreshWhenIdle, onStatus: (st) => (liveStatus = st) })
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) onunauthorized()
        else loadError = (err as Error).message
      }
    })()

    // Refresh when the tab regains focus (its stream may have been throttled), plus a slow poll.
    const tick = () => {
      if (document.visibilityState === 'visible') refreshWhenIdle()
    }
    const timer = setInterval(tick, REFRESH_MS)
    document.addEventListener('visibilitychange', tick)
    return () => {
      live?.close()
      mq.removeEventListener('change', onMq)
      clearInterval(timer)
      document.removeEventListener('visibilitychange', tick)
      document.title = 'Trackstar'
    }
  })

  function replaceStory(next: Story) {
    stories = stories.some((s) => s.id === next.id) ? stories.map((s) => (s.id === next.id ? next : s)) : [...stories, next]
    doneStories = doneStories.map((s) => (s.id === next.id ? next : s))
    trashStories = trashStories.map((s) => (s.id === next.id ? next : s))
  }

  async function withBusy<T>(id: number, work: () => Promise<T>): Promise<T | undefined> {
    busyIds = new Set(busyIds).add(id)
    try {
      return await work()
    } catch (err) {
      fail(err)
      await loadStories().catch(() => {})
      return undefined
    } finally {
      const next = new Set(busyIds)
      next.delete(id)
      busyIds = next
    }
  }

  async function patchStory(id: number, patch: StoryPatch): Promise<boolean> {
    const updated = await withBusy(id, () => api.updateStory(id, patch))
    if (!updated) return false
    replaceStory(updated)
    if (patch.state === 'accepted' || patch.estimate !== undefined) loadMeta().catch(fail)
    return true
  }

  async function onDrop(event: DropEvent) {
    if (checked.has(event.storyId) && checked.size > 1) {
      const ids = orderedSelection(stories, checked)
      return bulkMove(ids, bulkMoveRequest(lists[event.to].map((s) => s.id), ids, event.to, event.index))
    }
    const request = moveRequest(
      lists[event.to].map((s) => s.id),
      event.storyId,
      event.to,
      event.index,
    )
    stories = applyMove(stories, event.storyId, event.to, event.index) // optimistic
    selectedId = event.storyId
    const result = await withBusy(event.storyId, () => api.moveStory(event.storyId, request))
    if (!result) return
    replaceStory(result.story)
    if (result.renormalized) await loadStories().catch(fail)
  }

  // Moves several stories in one request; the list is reloaded because many
  // rows (and possibly the rebalanced section) changed.
  async function bulkMove(ids: number[], request: ReturnType<typeof bulkMoveRequest>) {
    if (ids.length === 0) return
    if (ids.some((id) => !mayDrop(id, request.section))) {
      toast = `Some selected stories cannot move to ${request.section}`
      setTimeout(() => (toast = ''), 4000)
      return
    }
    busyIds = new Set([...busyIds, ...ids])
    try {
      await api.moveStories(ids, request)
      await loadStories()
      checked = new Set()
    } catch (err) {
      fail(err)
      await loadStories().catch(() => {})
    } finally {
      const next = new Set(busyIds)
      for (const id of ids) next.delete(id)
      busyIds = next
    }
  }

  function toggleChecked(id: number) {
    const next = new Set(checked)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    checked = next
    selectedId = id
  }

  async function createStory(input: NewStory, keepOpen: boolean) {
    if (!project) return
    const created = await api.createStory(project.id, input)
    replaceStory(created)
    selectedId = created.id
    if (!keepOpen) creating = null
  }

  async function deleteStory(id: number) {
    const trashed = await withBusy(id, () => api.deleteStory(id))
    if (!trashed) return
    stories = stories.filter((s) => s.id !== id)
    doneStories = doneStories.filter((s) => s.id !== id)
    trashStories = [trashed, ...trashStories]
    openId = null
    if (selectedId === id) selectedId = null
    undo = { id, title: trashed.title }
    clearTimeout(undoTimer)
    undoTimer = setTimeout(() => (undo = null), 8000)
    if (trashed.state === 'accepted') loadMeta().catch(fail)
  }

  async function restoreStory(id: number) {
    if (undo?.id === id) undo = null
    const restored = await withBusy(id, () => api.restoreStory(id))
    if (!restored) return
    trashStories = trashStories.filter((s) => s.id !== id)
    replaceStory(restored)
    selectedId = id
    if (restored.state === 'accepted') loadMeta().catch(fail)
  }

  function commentCountChanged(id: number, delta: number) {
    const s = stories.find((x) => x.id === id) ?? doneStories.find((x) => x.id === id)
    if (s) replaceStory({ ...s, comment_count: Math.max(0, s.comment_count + delta) })
  }

  async function toggleEpics() {
    showEpics = !showEpics
    if (showEpics) await loadEpics()
  }

  async function toggleDone() {
    showDone = !showDone
    if (showDone && project) doneStories = await api.stories(project.id, { done: true }).catch((err) => (fail(err), []))
  }

  async function toggleTrash() {
    showTrash = !showTrash
    if (showTrash && project) trashStories = await api.stories(project.id, { deleted: true }).catch((err) => (fail(err), []))
  }

  async function selectTab(tab: MobileTab) {
    mobileTab = tab
    if (!project) return
    if (tab === 'done') doneStories = await api.stories(project.id, { done: true }).catch((err) => (fail(err), doneStories))
    if (tab === 'trash') trashStories = await api.stories(project.id, { deleted: true }).catch((err) => (fail(err), trashStories))
    if (tab === 'epics') await loadEpics()
  }
  // Which panels the data loaders keep fresh: on a phone, the visible tab.
  $effect(() => {
    if (mobile) {
      showDone = mobileTab === 'done'
      showTrash = mobileTab === 'trash'
    }
  })

  // "Move to" from the drawer: append to a section (the phone has no cross-panel drag).
  function moveToSection(id: number, section: DropSection) {
    const target = lists[section].map((s) => s.id).filter((x) => x !== id)
    bulkMove([id], { section, prev_id: target.at(-1) ?? null, next_id: null })
  }

  async function reloadUsers() {
    userList = await api.users().catch((err) => (fail(err), userList))
  }

  // --- keyboard ---------------------------------------------------------------

  function columns(): number[][] {
    return [lists.icebox, lists.backlog, [...accepted, ...lists.current]].map((list) => list.map((s) => s.id))
  }

  function moveSelection(dy: number, dx: number) {
    const cols = columns()
    let col = cols.findIndex((ids) => selectedId !== null && ids.includes(selectedId))
    if (col === -1) {
      const first = cols.findIndex((ids) => ids.length > 0)
      if (first !== -1) selectedId = cols[first][0]
      return
    }
    let row = cols[col].indexOf(selectedId!)
    if (dx !== 0) {
      for (let next = col + dx; next >= 0 && next < cols.length; next += dx) {
        if (cols[next].length > 0) {
          col = next
          row = Math.min(row, cols[col].length - 1)
          break
        }
      }
    }
    row = Math.max(0, Math.min(cols[col].length - 1, row + dy))
    selectedId = cols[col][row]
  }

  $effect(() => {
    if (selectedId !== null)
      document.querySelector(`[data-story-id="${selectedId}"]`)?.scrollIntoView({ block: 'nearest' })
  })

  async function toggleSwitcher() {
    switcherOpen = !switcherOpen
    if (switcherOpen && allProjects === null) {
      try {
        allProjects = await api.projects()
      } catch (err) {
        switcherOpen = false
        fail(err)
      }
    }
  }

  function switchTo(p: Project) {
    switcherOpen = false
    if (p.slug !== slug) location.hash = `#/p/${p.slug}`
  }

  function closeTopmost(): boolean {
    if (creating) creating = null
    else if (menuOpen) menuOpen = false
    else if (switcherOpen) switcherOpen = false
    else if (showAccount) showAccount = false
    else if (showUsers) showUsers = false
    else if (showSettings) showSettings = false
    else if (showHelp) showHelp = false
    else if (openId !== null) openId = null
    else if (checked.size > 0) checked = new Set()
    else if (query || activeEpic) {
      query = ''
      activeEpic = null
    } else if (selectedId !== null) selectedId = null
    else return false
    return true
  }

  function onKeydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement
    const typing = target.matches('input, textarea, select') || target.isContentEditable
    if (event.key === 'Escape') {
      if (typing) target.blur()
      if (closeTopmost()) event.preventDefault()
      return
    }
    if (typing || event.ctrlKey || event.metaKey || event.altKey || creating || showSettings || showAccount || showUsers) return

    const selected = stories.find((s) => s.id === selectedId)
    // Shift+I / B / C: move the selection (or the focused story) to the end of a section.
    if (event.shiftKey && !readOnly && (event.key === 'I' || event.key === 'B' || event.key === 'C')) {
      const section = ({ I: 'icebox', B: 'backlog', C: 'current' } as const)[event.key]
      const ids = checked.size > 0 ? orderedSelection(stories, checked) : selectedId !== null ? [selectedId] : []
      const target = lists[section].map((s) => s.id).filter((id) => !ids.includes(id))
      bulkMove(ids, { section, prev_id: target.at(-1) ?? null, next_id: null })
      event.preventDefault()
      return
    }
    switch (event.key) {
      case 'c':
        if (readOnly) return
        creating = creatingSection()
        break
      case 'x':
        if (selectedId !== null && !readOnly) toggleChecked(selectedId)
        break
      case 'e':
        toggleEpics()
        break
      case 'j':
      case 'ArrowDown':
        moveSelection(1, 0)
        break
      case 'k':
      case 'ArrowUp':
        moveSelection(-1, 0)
        break
      case 'h':
      case 'ArrowLeft':
        moveSelection(0, -1)
        break
      case 'l':
      case 'ArrowRight':
        moveSelection(0, 1)
        break
      case 'Enter':
        if (selectedId === null) return
        openId = selectedId
        break
      case '/':
        searchInput?.focus()
        break
      case '?':
        showHelp = !showHelp
        break
      default:
        return
    }
    event.preventDefault()
  }

  const openStoryRow = (story: Story) => {
    selectedId = story.id
    openId = story.id
  }
  const creatingSection = (): DropSection => {
    if (mobile && (mobileTab === 'icebox' || mobileTab === 'backlog' || mobileTab === 'current')) return mobileTab
    const selected = stories.find((s) => s.id === selectedId)
    return selected && selected.section !== 'done' ? selected.section : 'icebox'
  }
  const act = (story: Story, state: StoryState) => {
    if (readOnly) return
    patchStory(story.id, { state })
  }
  const estimate = (story: Story, points: number) => patchStory(story.id, { estimate: points })
  const mayDrop = (id: number, target: DropSection) => {
    if (readOnly) return false
    const story = stories.find((s) => s.id === id)
    return story !== undefined && canDrop(story, target)
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if loadError}
  <p class="load-error error">{loadError} — <a href="#/">back to projects</a></p>
{:else if project}
  <div class="board">
    <header class="topbar" class:mobile>
      <a href="#/" class="home" title="All projects">Trackstar</a>
      <div class="switcher">
        <button
          class="project-name"
          class:open={switcherOpen}
          onclick={toggleSwitcher}
          aria-haspopup="listbox"
          aria-expanded={switcherOpen}
          title="Switch project">
          <strong>{project.name}</strong><span class="caret">▾</span>
        </button>
        {#if switcherOpen}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <div class="menu-backdrop" onclick={() => (switcherOpen = false)}></div>
          <div class="switcher-items" role="listbox" aria-label="Projects">
            {#if allProjects === null}
              <span class="muted">Loading…</span>
            {:else}
              {#each allProjects as p (p.id)}
                <button role="option" aria-selected={p.id === project.id} class:current={p.id === project.id} onclick={() => switchTo(p)}>
                  {p.name}
                </button>
              {/each}
              <div class="sep"></div>
              <a href="#/" onclick={() => (switcherOpen = false)}>All projects…</a>
            {/if}
          </div>
        {/if}
      </div>
      {#if velocity}
        <span class="velocity" title={velocity.estimated
          ? 'No completed iteration yet — using the default velocity'
          : `Average of the last ${velocity.iterations.length} completed iteration(s): ${velocity.iterations.map((i) => i.points).join(', ')}`}>
          Velocity <b>{velocity.velocity}</b>{velocity.estimated ? '*' : ''}
        </span>
      {/if}
      {#if !mobile}
        <FilterBar
          projectId={project.id}
          {query}
          saved={savedFilters}
          onquery={(q) => (query = q)}
          onsavedchanged={(f) => (savedFilters = f)}
          oninput={(el) => (searchInput = el)}
        />
      {/if}
      {#if activeEpic}
        <button class="chip" onclick={() => (activeEpic = null)} title="Clear epic filter">epic: {activeEpic} ✕</button>
      {/if}
      {#if checked.size > 0}
        <span class="chip selection" title="Shift+I / B / C moves the selection; Esc clears">{checked.size} selected</span>
      {/if}
      {#if readOnly}<span class="chip">read-only</span>{/if}
      <span class="spacer"></span>
      <span
        class="live {liveStatus}"
        title={liveStatus === 'live'
          ? 'Live: changes by others appear as they happen'
          : liveStatus === 'offline'
            ? 'Connection lost — reconnecting'
            : 'Connecting…'}>●</span
      >
      {#if !mobile}
        <button class:on={showEpics} onclick={toggleEpics} title="Epics (e)">Epics</button>
        <button class:on={showDone} onclick={toggleDone}>Done</button>
        <button class:on={showTrash} onclick={toggleTrash} title="Deleted stories">Trash</button>
        {#if !readOnly}<button onclick={() => (creating = creatingSection())} title="New story (c)">+ Story</button>{/if}
        <button onclick={() => (showSettings = true)}>Settings</button>
        <button onclick={() => (showHelp = !showHelp)} title="Keyboard shortcuts (?)">?</button>
      {/if}
      <div class="menu">
        <button class:on={menuOpen} onclick={() => (menuOpen = !menuOpen)} aria-haspopup="menu" aria-expanded={menuOpen} aria-label="Menu">
          {mobile ? '☰' : `${user.display_name} ▾`}
        </button>
        {#if menuOpen}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <div class="menu-backdrop" onclick={() => (menuOpen = false)}></div>
          <div class="menu-items" role="menu">
            {#if mobile}
              <button role="menuitem" onclick={() => ((showSettings = true), (menuOpen = false))}>Project settings…</button>
              <div class="sep"></div>
            {/if}
            <button role="menuitem" onclick={() => ((showAccount = true), (menuOpen = false))}>Account…</button>
            {#if user.is_admin}
              <button role="menuitem" onclick={() => ((showUsers = true), (menuOpen = false))}>Users…</button>
            {/if}
            <button role="menuitem" onclick={onlogout}>Sign out</button>
          </div>
        {/if}
      </div>
    </header>

    {#if mobile}
      <div class="filter-row">
        <FilterBar
          projectId={project.id}
          {query}
          saved={savedFilters}
          onquery={(q) => (query = q)}
          onsavedchanged={(f) => (savedFilters = f)}
          oninput={(el) => (searchInput = el)}
        />
      </div>
      <nav class="tabs" aria-label="Panels">
        {#each [['icebox', 'Icebox', lists.icebox.length], ['backlog', 'Backlog', lists.backlog.length], ['current', 'Current', lists.current.length + accepted.length], ['epics', 'Epics', epics.length], ['done', 'Done', null], ['trash', 'Trash', null]] as const as [tab, label, count] (tab)}
          <button class:on={mobileTab === tab} onclick={() => selectTab(tab)} role="tab" aria-selected={mobileTab === tab}>
            {label}{#if count !== null && count > 0}<span class="count">{count}</span>{/if}
          </button>
        {/each}
      </nav>
    {/if}

    <main
      class="panels"
      class:mobile
      style:grid-template-columns={mobile
        ? 'minmax(0, 1fr)'
        : `${showEpics ? 'minmax(0, 0.7fr) ' : ''}repeat(${3 + Number(showDone) + Number(showTrash)}, minmax(0, 1fr))`}
    >
      {#if mobile ? mobileTab === 'epics' : showEpics}
        <EpicsPanel projectId={project.id} {epics} active={activeEpic} canWrite={!readOnly} onchanged={loadEpics} onselect={(n) => (activeEpic = n)} />
      {/if}
      {#if mobile ? mobileTab === 'done' : showDone}
        <DonePanel {iterations} stories={doneStories} {users} {selectedId} velocity={velocity?.velocity ?? null} onopen={openStoryRow} />
      {/if}
      {#if mobile ? mobileTab === 'trash' : showTrash}
        <TrashPanel stories={trashStories} onrestore={(s) => restoreStory(s.id)} onopen={openStoryRow} />
      {/if}
      {#each SECTIONS.filter((s) => !mobile || s === mobileTab) as section (section)}
        <Panel
          {section}
          rows={rows[section]}
          {users}
          {selectedId}
          {busyIds}
          {recentIds}
          checkedIds={checked}
          {epicNames}
          {readOnly}
          summary={summaries[section]}
          dragDisabled={filtering || readOnly}
          {dragging}
          canDrop={mayDrop}
          ondrop={onDrop}
          ondragstate={(d) => (dragging = d)}
          onadd={(s) => (creating = s)}
          onopen={openStoryRow}
          onaction={act}
          onestimate={estimate}
          ontoggle={(s) => toggleChecked(s.id)}
          top={section === 'current' ? currentTop : undefined}
        />
      {/each}
    </main>
  </div>

  {#if mobile && !readOnly && !openStory && !creating}
    <button class="fab" onclick={() => (creating = creatingSection())} aria-label="New story">+</button>
  {/if}

  {#snippet currentTop()}
    {#if currentTotal > 0}
      <div class="progress" title="Accepted points in this iteration">
        <div class="bar"><div style:width={`${(totalPoints(accepted) / currentTotal) * 100}%`}></div></div>
        <span>{totalPoints(accepted)} / {currentTotal} pts accepted</span>
      </div>
    {/if}
    {#each accepted as story (story.id)}
      <StoryRow {story} {users} {epicNames} {readOnly} selected={story.id === selectedId} onopen={openStoryRow} onaction={act} onestimate={estimate} />
    {/each}
  {/snippet}

  {#if openStory}
    <StoryDrawer
      story={openStory}
      {users}
      me={user}
      {stories}
      {readOnly}
      onmove={moveToSection}
      onpatch={patchStory}
      ondelete={deleteStory}
      onrestore={restoreStory}
      oncommented={commentCountChanged}
      onclose={() => (openId = null)}
    />
  {/if}
  {#if creating}
    <QuickCreate section={creating} oncreate={createStory} onclose={() => (creating = null)} />
  {/if}
  {#if showSettings}
    <ProjectSettings
      {project}
      users={userList}
      {members}
      onmembers={(m) => (members = m)}
      onclose={() => (showSettings = false)}
      onsaved={(p) => {
        project = p
        showSettings = false
        refresh()
      }}
    />
  {/if}
  {#if showAccount}
    <AccountDialog {user} onsaved={(u) => (onuserchanged(u), reloadUsers())} onclose={() => (showAccount = false)} />
  {/if}
  {#if showUsers}
    <UsersDialog me={user} users={userList} onchanged={(list) => (userList = list)} onclose={() => (showUsers = false)} />
  {/if}
  {#if showHelp}
    <div class="help" role="note">
      <kbd>c</kbd> new story · <kbd>j</kbd>/<kbd>k</kbd> move · <kbd>h</kbd>/<kbd>l</kbd> switch panel · <kbd>Enter</kbd> open ·
      <kbd>x</kbd> / <kbd>Shift</kbd>-click select · <kbd>Shift</kbd>+<kbd>I</kbd>/<kbd>B</kbd>/<kbd>C</kbd> move selection to Icebox/Backlog/Current ·
      <kbd>e</kbd> epics · <kbd>/</kbd> filter · <kbd>Esc</kbd> close
    </div>
  {/if}
  {#if toast}<div class="toast" role="alert">{toast}</div>{/if}
  {#if undo}
    <div class="undo" role="status">
      Deleted “{undo.title}”. <button class="link" onclick={() => restoreStory(undo!.id)}>Undo</button>
    </div>
  {/if}
{:else}
  <p class="load-error muted">Loading…</p>
{/if}

<style>
  .board {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .topbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px;
    background: var(--panel-head);
    color: var(--panel-head-text);
  }
  .topbar button {
    background: transparent;
    color: inherit;
    border-color: rgba(255, 255, 255, 0.3);
    padding: 2px 9px;
  }
  .topbar button.on {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-text);
  }
  .home {
    color: inherit;
    opacity: 0.7;
    text-decoration: none;
  }
  .live {
    font-size: 10px;
    opacity: 0.85;
  }
  .live.live {
    color: var(--accept);
  }
  .live.offline {
    color: var(--danger);
  }
  .live.connecting {
    color: var(--muted);
  }
  .velocity {
    padding: 1px 8px;
    border-radius: 10px;
    background: rgba(255, 255, 255, 0.14);
    font-size: 12px;
    white-space: nowrap;
  }
  .chip {
    padding: 1px 8px;
    border-radius: 10px;
    background: rgba(255, 255, 255, 0.14);
    font-size: 12px;
    white-space: nowrap;
    border: 0;
    color: inherit;
  }
  .chip.selection {
    background: var(--accent);
    color: var(--accent-text);
  }
  .spacer {
    flex: 1;
  }
  .me {
    opacity: 0.75;
    white-space: nowrap;
  }
  .panels {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
    padding: 8px;
  }
  .panels.mobile {
    padding: 6px;
  }
  .topbar.mobile {
    padding: 6px 10px;
    gap: 6px;
  }
  .topbar.mobile .switcher {
    flex: 1;
    min-width: 0;
  }
  .topbar.mobile .project-name {
    max-width: 100%;
  }
  .topbar.mobile .velocity {
    display: none;
  }
  .topbar.mobile .spacer {
    flex: 0;
  }
  .topbar.mobile .menu > button {
    font-size: 16px;
    padding: 0 10px;
    min-height: 32px;
  }
  .filter-row {
    padding: 6px 8px 0;
    background: var(--panel-head);
  }
  .filter-row :global(.filter) {
    width: 100%;
  }
  .filter-row :global(.search) {
    flex: 1;
    width: auto;
    min-height: 34px;
  }
  .filter-row :global(.filter > button) {
    color: var(--panel-head-text);
    min-height: 34px;
  }
  .tabs {
    display: flex;
    background: var(--panel-head);
    padding: 6px 8px 0;
    gap: 2px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs button {
    flex: 1;
    min-height: 34px;
    border: 0;
    border-radius: 6px 6px 0 0;
    background: rgba(255, 255, 255, 0.08);
    color: var(--panel-head-text);
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
    padding: 4px 8px;
  }
  .tabs button.on {
    background: var(--bg);
    color: var(--text);
  }
  .tabs .count {
    margin-left: 4px;
    font-weight: 400;
    opacity: 0.7;
  }
  .fab {
    position: fixed;
    right: 18px;
    bottom: calc(18px + env(safe-area-inset-bottom, 0px));
    width: 52px;
    height: 52px;
    border-radius: 50%;
    border: 0;
    background: var(--accent);
    color: var(--accent-text);
    font-size: 28px;
    line-height: 1;
    box-shadow: var(--shadow);
    z-index: 15;
  }
  .menu-items .sep {
    border-top: 1px solid var(--border);
    margin: 3px 0;
  }
  .progress {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    font-size: 11px;
    color: var(--muted);
    border-bottom: 1px solid var(--border);
  }
  .bar {
    flex: 1;
    height: 5px;
    border-radius: 3px;
    background: var(--border);
    overflow: hidden;
  }
  .bar div {
    height: 100%;
    background: var(--accept);
  }
  .help,
  .toast {
    position: fixed;
    left: 50%;
    transform: translateX(-50%);
    bottom: 14px;
    padding: 7px 14px;
    border-radius: 6px;
    background: var(--panel-head);
    color: var(--panel-head-text);
    box-shadow: var(--shadow);
    z-index: 40;
  }
  .toast {
    background: var(--danger);
    color: #fff;
    bottom: 52px;
  }
  .undo {
    position: fixed;
    left: 50%;
    transform: translateX(-50%);
    bottom: 14px;
    padding: 7px 14px;
    border-radius: 6px;
    background: var(--panel-head);
    color: var(--panel-head-text);
    box-shadow: var(--shadow);
    z-index: 40;
  }
  .undo .link {
    color: var(--accent);
    font-weight: 600;
    margin-left: 6px;
  }
  .menu {
    position: relative;
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
  .switcher {
    position: relative;
    min-width: 0;
  }
  .topbar button.project-name {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    max-width: 320px;
    padding: 2px 6px;
    margin-left: -6px;
    border-color: transparent;
    font-size: inherit;
  }
  .topbar button.project-name:hover,
  .topbar button.project-name.open {
    border-color: rgba(255, 255, 255, 0.3);
  }
  .project-name strong {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .project-name .caret {
    opacity: 0.6;
    font-size: 11px;
  }
  .switcher-items {
    position: absolute;
    left: 0;
    top: calc(100% + 4px);
    z-index: 26;
    display: grid;
    min-width: 200px;
    max-width: min(360px, 90vw);
    max-height: 60vh;
    overflow-y: auto;
    background: var(--panel);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: var(--shadow);
    padding: 4px;
  }
  .switcher-items button,
  .switcher-items a {
    text-align: left;
    border: 0;
    border-radius: 4px;
    color: var(--text);
    padding: 6px 10px;
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .switcher-items button:hover,
  .switcher-items a:hover {
    background: var(--row-hover);
  }
  .switcher-items button.current {
    font-weight: 600;
    background: var(--row);
  }
  .switcher-items .sep {
    border-top: 1px solid var(--border);
    margin: 3px 0;
  }
  .switcher-items .muted {
    padding: 6px 10px;
  }
  .load-error {
    padding: 40px;
    text-align: center;
  }
</style>
