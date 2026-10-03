<script lang="ts">
  import { storyHref, storyRefs } from '../lib/links'
  import type { Story } from '../lib/types'

  interface Props {
    text: string
    /** Known stories, to show a referenced story's title on hover. */
    stories?: Story[]
  }
  let { text, stories = [] }: Props = $props()

  let parts = $derived(storyRefs(text))
  const titleOf = (id: number) => stories.find((s) => s.id === id)?.title
</script>

{#each parts as part, i (i)}{#if part.storyId}<a class="story-ref" href={storyHref(part.storyId)} title={titleOf(part.storyId)}>{part.text}</a>{:else}{part.text}{/if}{/each}
