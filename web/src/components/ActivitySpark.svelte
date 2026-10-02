<script lang="ts">
  // A project's story changes and comments per day, oldest first, as a row
  // of small bars: enough to see whether it is picking up or winding down.
  import { formatDay } from '../lib/format'

  let { days }: { days: number[] } = $props()

  const SLOT = 4
  const BAR = 3
  const H = 20
  // A project with a single change a day should not look as busy as one with
  // twenty, so the scale never zooms in further than this.
  const MIN_PEAK = 5

  let total = $derived(days.reduce((a, b) => a + b, 0))
  let peak = $derived(Math.max(MIN_PEAK, ...days))
  const changes = (n: number) => `${n} ${n === 1 ? 'change' : 'changes'}`
  const dayOf = (i: number) => formatDay(new Date(Date.now() - (days.length - 1 - i) * 86_400_000))
</script>

<svg
  class="spark"
  viewBox={`0 0 ${days.length * SLOT - (SLOT - BAR)} ${H}`}
  width={days.length * SLOT - (SLOT - BAR)}
  height={H}
  role="img"
  aria-label={`${changes(total)} in the last ${days.length} days`}
>
  {#each days as n, i (i)}
    {@const h = n === 0 ? 1 : Math.max(2, Math.round((n / peak) * H))}
    <rect class:none={n === 0} x={i * SLOT} y={H - h} width={BAR} height={h} rx={n === 0 ? 0 : 1} />
    <!-- The whole column is the hover target; a 3px bar is too small to hit. -->
    <rect class="hit" x={i * SLOT - (SLOT - BAR) / 2} y="0" width={SLOT} height={H}>
      <title>{dayOf(i)}: {changes(n)}</title>
    </rect>
  {/each}
</svg>

<style>
  .spark {
    display: block;
    flex: none;
  }
  rect {
    fill: var(--accent);
  }
  rect.none {
    fill: var(--border);
  }
  rect.hit {
    fill: transparent;
  }
</style>
