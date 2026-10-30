<!--
  StatementPreview: a problem statement rendered the way students will see
  it, for the problem editor. On mount it loads lib/markdown.ts with a
  dynamic import, so marked, DOMPurify and KaTeX are only fetched by
  screens that show statements, then re-renders shortly after each change
  to the Markdown. The rendered HTML is sanitised before it is inserted and
  maths is drawn by KaTeX afterwards. Props: `title` (shown above the
  statement), `markdown` (the statement source), `timeLimitMs` and
  `memoryMb` (the limits line; either may be null while being edited), and
  `headingBase` (the level a "#" heading becomes, default 4, to sit under
  the editor's own headings), and `samples`: when given, the statement and
  its samples are drawn by the students' own Statement component, so the
  preview matches what students see; `samplesCut` says the
  sample text is only the start of each file.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import Statement from './Statement.svelte';
  import type { Sample } from '../lib/workspace';

  interface Props {
    title: string;
    markdown: string;
    timeLimitMs: number | null;
    memoryMb: number | null;
    headingBase?: number;
    samples?: Sample[];
    samplesCut?: boolean;
  }

  let { title, markdown, timeLimitMs, memoryMb, headingBase = 4, samples, samplesCut = false }: Props = $props();

  type Renderer = typeof import('../lib/markdown');

  let renderer = $state<Renderer | null>(null);
  let failed = $state(false);
  let target: HTMLDivElement | undefined = $state();

  const empty = $derived(markdown.trim() === '');

  const limits = $derived.by(() => {
    const parts: string[] = [];
    if (timeLimitMs !== null && Number.isFinite(timeLimitMs)) parts.push(`Time limit ${timeLimitMs} ms`);
    if (memoryMb !== null && Number.isFinite(memoryMb)) parts.push(`memory limit ${memoryMb} MB`);
    const text = parts.join(', ');
    return text.charAt(0).toUpperCase() + text.slice(1);
  });

  onMount(() => {
    let alive = true;
    import('../lib/markdown')
      .then((mod) => {
        if (alive) renderer = mod;
      })
      .catch(() => {
        if (alive) failed = true;
      });
    return () => {
      alive = false;
    };
  });

  // The first render is immediate; later ones wait for a pause in typing.
  let rendered = false;
  $effect(() => {
    const source = markdown;
    const base = headingBase;
    const mod = renderer;
    const el = target;
    if (!mod || !el) return;
    const run = () => {
      void mod.renderStatement(el, source, { headingBase: base });
      rendered = true;
    };
    if (!rendered) {
      run();
      return;
    }
    const timer = setTimeout(run, 150);
    return () => clearTimeout(timer);
  });
</script>

<div class="preview">
  <p class="preview-title">{title.trim() !== '' ? title : 'Untitled problem'}</p>
  {#if limits !== ''}<p class="limits muted">{limits}</p>{/if}

  {#if failed}
    <p class="muted note">The preview could not load. Reload the page to try again.</p>
  {:else if renderer === null}
    <p class="muted note" role="status">Loading preview</p>
  {/if}
  {#if empty && renderer !== null}
    <p class="muted note">Write the statement to see it here.</p>
  {/if}
  {#if samples}
    <div class="as-students" hidden={empty}>
      <Statement {markdown} {samples} {headingBase} />
      {#if samplesCut}<p class="muted note">Long samples show only their first 200 bytes here; students see them whole.</p>{/if}
    </div>
  {:else}
    <div class="statement" bind:this={target} hidden={empty || renderer === null}></div>
  {/if}
</div>

<style>
  .preview {
    min-width: 0;
  }

  .preview-title {
    font-size: var(--fs-3);
    font-weight: 600;
    line-height: var(--lh-tight);
    overflow-wrap: anywhere;
  }

  .limits {
    margin-top: var(--space-1);
    font-size: var(--fs-0);
  }

  .note {
    margin-top: var(--space-4);
  }

  .statement {
    max-width: var(--measure-prose);
    margin-top: var(--space-4);
    overflow-wrap: break-word;
  }

  .statement[hidden],
  .as-students[hidden] {
    display: none;
  }

  .as-students {
    margin-top: var(--space-4);
  }

  .statement :global(:is(p, ul, ol, pre, blockquote, table, .math-block, hr)) {
    margin-top: var(--space-3);
  }

  .statement :global(:is(h3, h4, h5, h6)) {
    margin-top: var(--space-5);
    font-size: var(--fs-2);
  }

  .statement :global(:is(h5, h6)) {
    font-size: var(--fs-1);
  }

  .statement :global(:is(ul, ol)) {
    padding-left: var(--space-5);
  }

  .statement :global(li + li) {
    margin-top: var(--space-1);
  }

  .statement :global(code) {
    font-size: var(--mono-optical);
  }

  .statement :global(:not(pre) > code) {
    padding-inline: var(--space-1);
    border-radius: var(--radius);
    background: var(--surface-2);
  }

  .statement :global(pre) {
    padding: var(--space-3);
    overflow-x: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
  }

  .statement :global(blockquote) {
    padding-left: var(--space-3);
    border-left: var(--space-1) solid var(--line-strong);
    color: var(--ink-muted);
  }

  .statement :global(table) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
    border-collapse: collapse;
  }

  .statement :global(:is(th, td)) {
    padding: var(--space-1) var(--space-2);
    border: var(--rule) solid var(--line);
    text-align: left;
  }

  .statement :global(th) {
    background: var(--surface-2);
  }

  .statement :global(hr) {
    border: 0;
    border-top: var(--rule) solid var(--line);
  }

  /* Figures are usually drawn black on transparent, so they sit on a fixed
     light backing in every theme. */
  .statement :global(img) {
    max-width: 100%;
    height: auto;
    padding: var(--space-2);
    background: var(--figure-backing);
  }

  .statement :global(.math-block),
  .statement :global(.katex-display) {
    max-width: 100%;
    overflow-x: auto;
    overflow-y: hidden;
  }
</style>
