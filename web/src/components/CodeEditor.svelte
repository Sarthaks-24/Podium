<!--
  CodeEditor: the workspace's code editor in the dark well. It loads
  lib/editor.ts with a dynamic import on mount, so only screens that edit
  code fetch CodeMirror, and shows a short loading line until then.
  Changing the language, label or Tab behaviour reconfigures the running
  editor; the font size is at least 16 px on touch screens so iOS never
  zooms in. A visually hidden line, referenced by aria-describedby, says
  how to leave the editor and which keys run and submit. Under 1024 px it
  is read only, and a contest may block paste. Callers reach jumpTo(),
  setDoc() and focus() through bind:this.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { EditorHandle, EditorLanguage, EditorMark } from '../lib/editor';

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const mod = isMac ? 'Cmd' : 'Ctrl';
  const hintId = `editor-hint-${Math.random().toString(36).slice(2, 9)}`;

  interface Props {
    initial: string;
    language: EditorLanguage;
    label: string;
    id?: string;
    keysHint?: string;
    fontSize: number;
    tabMovesFocus: boolean;
    tabSize: 2 | 4;
    wrap: boolean;
    readOnly?: boolean;
    blockPaste?: boolean;
    onpasteblocked?: () => void;
    onchange: (source: string) => void;
    ontyped?: (chars: number) => void;
    onpaste?: (chars: number) => void;
    onblur?: () => void;
    onsubmit?: () => void;
    onrun?: () => void;
    oncursor?: (line: number, col: number) => void;
  }

  let {
    initial,
    language,
    label,
    id,
    keysHint,
    fontSize,
    tabMovesFocus,
    tabSize,
    wrap,
    readOnly = false,
    blockPaste = false,
    onpasteblocked,
    onchange,
    ontyped,
    onpaste,
    onblur,
    onsubmit,
    onrun,
    oncursor,
  }: Props = $props();

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
          id,
          describedBy: hintId,
          tabMovesFocus,
          tabSize,
          wrap,
          readOnly,
          blockPaste,
          onPasteBlocked: () => onpasteblocked?.(),
          onChange: (s) => onchange(s),
          onTyped: (n) => ontyped?.(n),
          onPaste: (n) => onpaste?.(n),
          onBlur: () => onblur?.(),
          onSubmit: () => onsubmit?.(),
          onRun: () => onrun?.(),
          onCursor: (line, col) => oncursor?.(line, col),
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

  $effect(() => {
    handle?.setTabSize(tabSize);
  });

  $effect(() => {
    handle?.setWrap(wrap);
  });

  $effect(() => {
    handle?.setReadOnly(readOnly);
  });

  $effect(() => {
    handle?.setBlockPaste(blockPaste);
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

  /** Marks compiler messages in the code; an empty list clears them. */
  export function setMarks(marks: readonly EditorMark[]): void {
    handle?.setMarks(marks);
  }

  /** Focuses the editor. */
  export function focus(): void {
    handle?.focus();
  }
</script>

<div class="well" style:--editor-font-size="{fontSize}px">
  <div class="host" bind:this={host}></div>
  <p class="visually-hidden" id={hintId}>
    {tabMovesFocus ? 'Tab leaves the editor.' : 'Tab indents. Press Escape, then Tab, to leave the editor.'}
    {keysHint ?? `${mod}+Enter submits and ${mod}+' runs. Press ? outside the editor for every shortcut.`}
  </p>
  {#if failed}
    <p class="note">The editor could not load. Reload the page to try again; your code is kept.</p>
  {:else if handle === null}
    <p class="note" role="status">Loading the editor</p>
  {/if}
</div>

<style>
  .well {
    /* lib/editor.ts reads the size from here; touch screens never go under 16 px. */
    --editor-font-size-used: var(--editor-font-size);
    position: relative;
    display: grid;
    min-height: 0;
    height: 100%;
    border: var(--rule) solid var(--well-edge);
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
    outline: calc(2 * var(--rule)) solid var(--code-focus);
    outline-offset: calc(-2 * var(--rule));
  }

  @media (pointer: coarse) {
    .well {
      --editor-font-size-used: max(var(--fs-1), var(--editor-font-size));
    }
  }

  .note {
    position: absolute;
    inset: var(--space-3) var(--space-4) auto;
    color: var(--well-ink);
    font-size: var(--fs-0);
  }
</style>
