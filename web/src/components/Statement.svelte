<!--
  Statement: a problem statement as students read it in the workspace,
  followed by the samples. On mount it loads lib/markdown.ts with a dynamic
  import (marked, DOMPurify and KaTeX stay out of the main bundle), renders
  the sanitised HTML and draws the maths. Each sample shows its input and
  output in mono with a Copy button for each; the button says "Copied" for
  a moment and a polite status line announces it. Props: `markdown` (the
  statement source), `samples`, and `headingBase` (the level a "#" heading
  in the statement becomes, default 2, under the page's h1).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { Sample } from '../lib/workspace';
  import Icon from './Icon.svelte';

  interface Props {
    markdown: string;
    samples: Sample[];
    headingBase?: number;
  }

  let { markdown, samples, headingBase = 2 }: Props = $props();

  type Renderer = typeof import('../lib/markdown');

  let renderer = $state<Renderer | null>(null);
  let failed = $state(false);
  let target: HTMLDivElement | undefined = $state();
  let copied = $state('');
  let status = $state('');
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

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
      clearTimeout(copyTimer);
    };
  });

  $effect(() => {
    if (renderer && target) void renderer.renderStatement(target, markdown, { headingBase });
  });

  async function copy(text: string, key: string, what: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = key;
      status = `Copied ${what}`;
    } catch {
      status = 'Copying is blocked in this browser. Select the text and copy it instead.';
    }
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = ''), 2000);
  }
</script>

<div class="statement-wrap">
  {#if failed}
    <p class="muted">The statement could not load. Reload the page to try again.</p>
  {:else if renderer === null}
    <p class="muted" role="status">Loading the statement</p>
  {/if}
  <div class="statement" bind:this={target} hidden={renderer === null}></div>

  {#if samples.length > 0}
    <div class="samples">
      {#each samples as s, i (s.idx)}
        <section class="sample" aria-labelledby="sample-{s.idx}">
          <h2 id="sample-{s.idx}" class="sample-title">Sample {i + 1}</h2>
          <div class="io">
            {#each [{ kind: 'input', label: 'Input', text: s.input }, { kind: 'output', label: 'Output', text: s.output }] as part (part.kind)}
              {@const key = `${s.idx}-${part.kind}`}
              <div class="io-part">
                <div class="io-head">
                  <span class="io-label">{part.label}</span>
                  <button
                    type="button"
                    class="btn btn-quiet btn-compact copy"
                    aria-label="Copy sample {i + 1} {part.kind}"
                    onclick={() => copy(part.text, key, `sample ${i + 1} ${part.kind}`)}
                  >
                    <Icon name={copied === key ? 'check' : 'copy'} />
                    {copied === key ? 'Copied' : 'Copy'}
                  </button>
                </div>
                <pre>{part.text}</pre>
              </div>
            {/each}
          </div>
          {#if s.truncated}<p class="hint">This sample is long; only the first 4 KB is shown.</p>{/if}
        </section>
      {/each}
    </div>
  {/if}
  <p class="visually-hidden" role="status">{status}</p>
</div>

<style>
  .statement-wrap {
    min-width: 0;
    container: statement / inline-size;
  }

  .statement {
    max-width: var(--measure-prose);
    overflow-wrap: break-word;
  }

  .statement[hidden] {
    display: none;
  }

  .statement :global(> :first-child) {
    margin-top: 0;
  }

  .statement :global(:is(p, ul, ol, pre, blockquote, table, .math-block, hr)) {
    margin-top: var(--space-3);
  }

  .statement :global(:is(h2, h3, h4, h5, h6)) {
    margin-top: var(--space-5);
    font-size: var(--fs-2);
  }

  .statement :global(:is(h4, h5, h6)) {
    font-size: var(--fs-1);
  }

  .statement :global(:is(ul, ol)) {
    padding-left: var(--space-5);
  }

  .statement :global(li + li) {
    margin-top: var(--space-1);
  }

  .statement :global(code) {
    font-size: 0.9em;
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

  .samples {
    display: grid;
    gap: var(--space-5);
    max-width: var(--measure-prose);
    margin-top: var(--space-6);
  }

  .sample-title {
    font-size: var(--fs-2);
  }

  .io {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }

  .io-part {
    min-width: 0;
  }

  .io-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .io-label {
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .copy {
    color: var(--ink-muted);
  }

  .copy:hover {
    color: var(--ink);
  }

  .io pre {
    max-height: calc(12 * var(--lh-body) * var(--fs-0));
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface);
    font-size: var(--fs-0);
  }

  .hint {
    margin-top: var(--space-1);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  @container statement (min-width: 34rem) {
    .io {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
</style>
