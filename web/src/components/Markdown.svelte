<!--
  Markdown: organiser-written Markdown (contest rules, the honour code,
  announcements, the editorial) rendered the way statements are: loaded
  with a dynamic import of lib/markdown.ts, sanitised with DOMPurify, maths
  drawn with KaTeX. Until the renderer loads (it is tried a second time
  if the first load fails), the source shows as plain text, so nothing is
  ever missing. Props: `source`, `headingBase` (the
  level a "#" heading becomes, default 3) and `compact` (tighter spacing,
  for banners).
-->
<script lang="ts">
  import { onMount } from 'svelte';

  interface Props {
    source: string;
    headingBase?: number;
    compact?: boolean;
  }

  let { source, headingBase = 3, compact = false }: Props = $props();

  type Renderer = typeof import('../lib/markdown');

  let renderer = $state<Renderer | null>(null);
  let target: HTMLDivElement | undefined = $state();

  onMount(() => {
    let alive = true;
    let retry: ReturnType<typeof setTimeout> | undefined;
    // One retry after a failed load (a dropped connection); the plain text stays readable meanwhile.
    const load = (attempt: number) =>
      import('../lib/markdown')
        .then((mod) => {
          if (alive) renderer = mod;
        })
        .catch(() => {
          if (alive && attempt === 0) retry = setTimeout(() => void load(1), 1500);
        });
    void load(0);
    return () => {
      alive = false;
      clearTimeout(retry);
    };
  });

  $effect(() => {
    if (renderer && target) renderer.renderStatement(target, source, { headingBase });
  });
</script>

{#if renderer === null}
  <p class="plain" class:compact>{source}</p>
{/if}
<div class="md" class:compact bind:this={target} hidden={renderer === null}></div>

<style>
  .plain {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .md {
    max-width: var(--measure-prose);
    overflow-wrap: break-word;
  }

  .md[hidden] {
    display: none;
  }

  .md :global(> :first-child) {
    margin-top: 0;
  }

  .md :global(:is(p, ul, ol, pre, blockquote, table, .math-block)) {
    margin-top: var(--space-3);
  }

  .md.compact :global(:is(p, ul, ol, pre, blockquote, table, .math-block)) {
    margin-top: var(--space-1);
  }

  .md :global(:is(h3, h4, h5, h6)) {
    margin-top: var(--space-4);
    font-size: var(--fs-1);
  }

  .md :global(:is(ul, ol)) {
    padding-left: var(--space-5);
  }

  .md :global(li + li) {
    margin-top: var(--space-1);
  }

  .md :global(code) {
    font-size: 0.92em;
  }

  .md :global(pre) {
    padding: var(--space-3);
    overflow-x: auto;
    background: var(--surface-2);
  }
</style>
