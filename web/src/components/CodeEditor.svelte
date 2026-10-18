<!--
  CodeEditor: the workspace's code editor in the dark well (design.md 5.3).
  On mount it loads lib/editor.ts with a dynamic import, so CodeMirror is
  fetched only by screens that edit code, and shows a short loading line
  until then. Changing `language`, `label` or `tabMovesFocus` reconfigures
  the running editor; `fontSize` sets --editor-font-size on the well.
  Callers reach jumpTo(), setDoc() and focus() through bind:this.
  Props: `initial` (the code to start with), `language`, `label` (the
  editor's accessible name, naming the problem and language), `fontSize`
  (px), `tabMovesFocus`, and the callbacks `onchange`, `ontyped`, `onpaste`,
  `onblur`, `onsubmit` and `onrun`.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { EditorHandle } from '../lib/editor';
  import type { Language } from '../lib/workspace';

  interface Props {
    initial: string;
    language: Language;
    label: string;
    fontSize: number;
    tabMovesFocus: boolean;
    onchange: (source: string) => void;
    ontyped: (chars: number) => void;
    onpaste: (chars: number) => void;
    onblur: () => void;
    onsubmit: () => void;
    onrun: () => void;
  }

  let { initial, language, label, fontSize, tabMovesFocus, onchange, ontyped, onpaste, onblur, onsubmit, onrun }: Props =
    $props();

  let host: HTMLDivElement | undefined = $state();
  let handle = $state<EditorHandle | null>(null);
  let failed = $state(false);
  // Calls that arrive before the editor has loaded.
  let pendingDoc: string | null = null;
  let pendingJump: { line: number; col: number | null } | null = null;

  onMount(() => {
    let alive = true;
    let created: EditorHandle | null = null;
    import('../lib/editor')
      .then((mod) => {
        if (!alive || !host) return;
        created = mod.createEditor({
          parent: host,
          doc: pendingDoc ?? initial,
          language,
          label,
          tabMovesFocus,
          onChange: (s) => onchange(s),
          onTyped: (n) => ontyped(n),
          onPaste: (n) => onpaste(n),
          onBlur: () => onblur(),
          onSubmit: () => onsubmit(),
          onRun: () => onrun(),
        });
        handle = created;
        if (pendingJump) created.jumpTo(pendingJump.line, pendingJump.col);
        pendingDoc = null;
        pendingJump = null;
      })
      .catch(() => {
        if (alive) failed = true;
      });
    return () => {
      alive = false;
      created?.destroy();
    };
  });

  $effect(() => {
    handle?.setLanguage(language, label);
  });

  $effect(() => {
    handle?.setTabMovesFocus(tabMovesFocus);
  });

  /** Moves the cursor to a line and column and focuses the editor. */
  export function jumpTo(line: number, col: number | null): void {
    if (handle) handle.jumpTo(line, col);
    else pendingJump = { line, col };
  }

  /** Replaces the code without counting it as typing. */
  export function setDoc(doc: string): void {
    if (handle) handle.setDoc(doc);
    else pendingDoc = doc;
  }

  /** Focuses the editor. */
  export function focus(): void {
    handle?.focus();
  }
</script>

<div class="well" style:--editor-font-size="{fontSize}px">
  <div class="host" bind:this={host}></div>
  {#if failed}
    <p class="note">The editor could not load. Reload the page to try again; your code is kept.</p>
  {:else if handle === null}
    <p class="note" role="status">Loading the editor</p>
  {/if}
</div>

<style>
  .well {
    position: relative;
    display: grid;
    min-height: 0;
    height: 100%;
    background: var(--well);
    color: var(--well-ink);
  }

  .host {
    min-height: 0;
    min-width: 0;
    height: 100%;
    overflow: hidden;
  }

  /* The ring shows when the editor's text area has keyboard focus. */
  .host:focus-within {
    box-shadow: inset 0 0 0 calc(2 * var(--rule)) var(--act);
  }

  .note {
    position: absolute;
    inset: var(--space-3) var(--space-4) auto;
    color: var(--well-ink);
    font-size: var(--fs-0);
  }
</style>
