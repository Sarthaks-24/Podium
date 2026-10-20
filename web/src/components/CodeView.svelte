<!--
  CodeView: a program shown read only in the dark editor well, with line
  numbers and the passages it shares with another program tinted. Spans
  are byte offsets into the UTF-8 source (lib/review.ts converts them).
  Each shared passage is also marked for screen readers with visually
  hidden "Shared passage starts" and "Shared passage ends" words, and each
  line holding one carries an edge in the gutter, so the match never
  depends on colour alone. The well scrolls sideways on its own and takes
  focus, so long lines are reachable from the keyboard. Student code is
  shown as text, never as HTML. Props: `source`, `spans`, `label` (the
  region's accessible name, for example "Asha Rao's program").
-->
<script lang="ts">
  import { codeLines, type Span } from '../lib/review';

  interface Props {
    source: string;
    spans: Span[];
    label: string;
  }

  let { source, spans, label }: Props = $props();

  const lines = $derived(codeLines(source, spans));
  const passages = $derived(lines.reduce((n, l) => n + l.segments.filter((s) => s.mark && s.opens).length, 0));
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="well" role="region" aria-label="{label}, {lines.length} {lines.length === 1 ? 'line' : 'lines'}, {passages} shared {passages === 1 ? 'passage' : 'passages'}" tabindex="0">
  <pre class="code"><code>{#each lines as line (line.number)}<span class="line" class:hit={line.matched}><span class="ln" aria-hidden="true">{line.number}</span><span class="src">{#each line.segments as seg, i (i)}{#if seg.mark}{#if seg.opens}<span class="visually-hidden">Shared passage starts. </span>{/if}<mark>{seg.text}</mark>{#if seg.closes}<span class="visually-hidden"> Shared passage ends.</span>{/if}{:else}{seg.text}{/if}{/each}</span></span>{/each}</code></pre>
</div>

<style>
  .well {
    position: relative;
    max-width: 100%;
    max-height: 70vh;
    overflow: auto;
    border: var(--rule) solid var(--well-edge);
    background: var(--well);
    color: var(--well-ink);
  }

  .code {
    display: block;
    width: max-content;
    min-width: 100%;
    padding-block: var(--space-2);
    font-size: var(--fs-0);
    line-height: var(--lh-body);
    tab-size: 4;
  }

  .line {
    display: grid;
    grid-template-columns: 4.5ch auto;
    border-left: calc(3 * var(--rule)) solid transparent;
  }

  .line.hit {
    border-left-color: var(--code-match-edge);
  }

  .ln {
    padding-right: var(--space-3);
    color: var(--code-gutter);
    text-align: right;
    user-select: none;
  }

  .src {
    padding-right: var(--space-4);
    white-space: pre;
  }

  mark {
    background: var(--code-match);
    color: var(--well-ink);
  }
</style>
