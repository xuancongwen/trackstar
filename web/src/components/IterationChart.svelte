<script lang="ts">
  // Accepted points per iteration with the velocity average, as inline SVG.
  import type { Iteration } from '../lib/types'

  interface Props {
    iterations: Iteration[]
    velocity: number | null
    /** How many of the most recent iterations to draw. */
    count?: number
  }
  let { iterations, velocity, count = 12 }: Props = $props()

  const W = 320
  const H = 90
  const PAD = { top: 10, right: 8, bottom: 18, left: 26 }

  let shown = $derived(iterations.slice(-count))
  let maxPoints = $derived(Math.max(1, velocity ?? 0, ...shown.map((it) => it.points)))
  let innerW = $derived(W - PAD.left - PAD.right)
  let innerH = $derived(H - PAD.top - PAD.bottom)
  let slot = $derived(shown.length ? innerW / shown.length : innerW)
  let y = $derived((points: number) => PAD.top + innerH - (points / maxPoints) * innerH)
</script>

{#if shown.length > 0}
  <svg viewBox={`0 0 ${W} ${H}`} class="chart" role="img" aria-label="Accepted points per iteration">
    <line x1={PAD.left} y1={PAD.top + innerH} x2={W - PAD.right} y2={PAD.top + innerH} class="axis" />
    <text x={PAD.left - 4} y={PAD.top + 4} class="tick">{maxPoints}</text>
    <text x={PAD.left - 4} y={PAD.top + innerH} class="tick">0</text>
    {#each shown as it, i (it.number)}
      {@const barW = Math.max(3, slot * 0.6)}
      {@const x = PAD.left + i * slot + (slot - barW) / 2}
      <rect {x} y={y(it.points)} width={barW} height={PAD.top + innerH - y(it.points)} class:current={it.current} class="bar">
        <title>Iteration {it.number}: {it.points} pts, {it.accepted_stories} stories{it.current ? ' (in progress)' : ''}</title>
      </rect>
      {#if shown.length <= 16 || i % 2 === 0}
        <text x={x + barW / 2} y={H - 5} class="label">{it.number}</text>
      {/if}
    {/each}
    {#if velocity !== null && velocity > 0}
      <line x1={PAD.left} y1={y(velocity)} x2={W - PAD.right} y2={y(velocity)} class="velocity">
        <title>Velocity {velocity}</title>
      </line>
      <text x={W - PAD.right} y={y(velocity) - 3} class="velocity-label">v {velocity}</text>
    {/if}
  </svg>
{/if}

<style>
  .chart {
    display: block;
    width: 100%;
    height: auto;
    padding: 4px 6px 0;
    box-sizing: border-box;
  }
  .axis {
    stroke: var(--border);
    stroke-width: 1;
  }
  .bar {
    fill: var(--accept);
  }
  .bar.current {
    fill: var(--accent);
    opacity: 0.6;
  }
  .velocity {
    stroke: var(--feature);
    stroke-width: 1;
    stroke-dasharray: 3 3;
  }
  .velocity-label,
  .tick,
  .label {
    font-size: 9px;
    fill: var(--muted);
  }
  .tick {
    text-anchor: end;
  }
  .label {
    text-anchor: middle;
  }
  .velocity-label {
    text-anchor: end;
    fill: var(--feature);
  }
</style>
