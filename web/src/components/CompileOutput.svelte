<!--
  CompileOutput: compiler output shown verbatim in mono, with every source
  location it names (lib/compileErrors.ts: "main.cpp:12:5",
  "Main.java:12", 'line 12', "main.js:12") turned into a button that moves
  the editor's cursor there. The text is never rendered as HTML. Props:
  `output` (the compiler's text), `language` (which compiler's shapes to
  look for) and `onjump` (called with the line and column, or null when
  there is no editor to jump to).
-->
<script lang="ts">
  import { segments } from '../lib/compileErrors';

  interface Props {
    output: string;
    language: string;
    onjump: ((line: number, col: number | null) => void) | null;
  }

  let { output, language, onjump }: Props = $props();

  const parts = $derived(segments(output, language));
</script>

<pre class="output">{#each parts as part, i (i)}{#if typeof part === 'string'}{part}{:else if onjump}<button
        type="button"
        class="jump"
        aria-label="Go to line {part.line}{part.col !== null ? `, column ${part.col}` : ''} in the editor"
        onclick={() => onjump?.(part.line, part.col)}>{part.text}</button
      >{:else}{part.text}{/if}{/each}</pre>

<style>
  .output {
    max-height: calc(16 * var(--lh-body) * var(--fs-0));
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
    color: var(--ink);
    font-size: var(--fs-0);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .jump {
    padding: 0;
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: var(--act);
    font: inherit;
    text-decoration: underline;
    text-underline-offset: 0.15em;
  }

  .jump:hover {
    text-decoration-thickness: 2px;
  }
</style>
