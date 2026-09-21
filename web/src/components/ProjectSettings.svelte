<script lang="ts">
  import { api } from '../lib/api'
  import { WEEKDAYS } from '../lib/format'
  import type { Project } from '../lib/types'

  interface Props {
    project: Project
    onsaved: (project: Project) => void
    onclose: () => void
  }
  let { project, onsaved, onclose }: Props = $props()

  // svelte-ignore state_referenced_locally
  let form = $state({
    name: project.name,
    description: project.description,
    iteration_length_days: project.iteration_length_days,
    iteration_start_weekday: project.iteration_start_weekday,
    velocity_window: project.velocity_window,
  })
  let error = $state('')

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    error = ''
    try {
      onsaved(await api.updateProject(project.id, form))
    } catch (err) {
      error = (err as Error).message
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <form class="modal" onsubmit={submit} aria-label="Project settings">
    <h2>Project settings</h2>
    <label class="field"><span>Name</span><input bind:value={form.name} required maxlength="100" /></label>
    <label class="field"><span>Description</span><textarea bind:value={form.description} rows="2"></textarea></label>
    <div class="grid">
      <label class="field">
        <span>Iteration length</span>
        <select bind:value={form.iteration_length_days}>
          {#each [7, 14, 21, 28] as days (days)}
            <option value={days}>{days / 7} week{days > 7 ? 's' : ''}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>Iterations start on</span>
        <select bind:value={form.iteration_start_weekday}>
          {#each WEEKDAYS as name, i (i)}<option value={i}>{name}</option>{/each}
        </select>
      </label>
      <label class="field">
        <span>Velocity window</span>
        <select bind:value={form.velocity_window}>
          {#each [1, 2, 3, 4, 5, 6, 8, 12] as n (n)}<option value={n}>last {n} iteration{n > 1 ? 's' : ''}</option>{/each}
        </select>
      </label>
    </div>
    <p class="muted">Changing the iteration schedule renumbers past iterations; accepted stories keep their dates.</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row-actions">
      <button type="button" onclick={onclose}>Cancel</button>
      <button class="primary">Save</button>
    </div>
  </form>
</div>

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  p {
    margin: 0;
    font-size: 12px;
  }
</style>
