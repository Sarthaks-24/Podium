<!--
  EditorSettings: the "Editor settings" button in the workspace toolbar and
  the small panel it opens (audit 2026-10-20 S2), holding the editor's font
  size and "Tab moves focus", which used to take a row of their own above
  and below the editor. A disclosure, not a menu: the panel holds form
  controls. Escape closes it and returns focus to the button, as do a click
  outside and tabbing out (the same behaviour as AccountMenu). Props:
  `fontSize`, `sizes` (the choices, in px), `tabMovesFocus`, `onfont` and
  `ontab` (the change events of the select and the checkbox).
-->
<script lang="ts">
  import Icon from './Icon.svelte';

  interface Props {
    fontSize: number;
    sizes: readonly number[];
    tabMovesFocus: boolean;
    onfont: (event: Event) => void;
    ontab: (event: Event) => void;
  }

  let { fontSize, sizes, tabMovesFocus, onfont, ontab }: Props = $props();

  let open = $state(false);
  let root: HTMLDivElement | undefined = $state();
  let button: HTMLButtonElement | undefined = $state();

  function onWindowKeydown(event: KeyboardEvent) {
    if (!open || event.key !== 'Escape' || event.defaultPrevented) return;
    if (document.querySelector('dialog[open]')) return;
    event.preventDefault();
    open = false;
    button?.focus();
  }

  function onWindowPointerdown(event: PointerEvent) {
    if (open && root && !root.contains(event.target as Node)) open = false;
  }

  function onFocusout(event: FocusEvent) {
    const next = event.relatedTarget as Node | null;
    if (open && next && root && !root.contains(next)) open = false;
  }
</script>

<svelte:window onkeydown={onWindowKeydown} onpointerdown={onWindowPointerdown} />

<div class="settings" bind:this={root} onfocusout={onFocusout}>
  <button
    type="button"
    class="btn btn-quiet btn-compact"
    aria-expanded={open}
    aria-controls="ws-editor-settings"
    bind:this={button}
    onclick={() => (open = !open)}
  >
    Editor settings<Icon name="chevron-down" />
  </button>
  <div id="ws-editor-settings" class="panel" hidden={!open}>
    <div class="field">
      <label for="ws-font">Font size</label>
      <select id="ws-font" class="select" value={fontSize} onchange={onfont}>
        {#each sizes as size (size)}<option value={size}>{size} px</option>{/each}
      </select>
    </div>
    <label class="check">
      <input type="checkbox" checked={tabMovesFocus} onchange={ontab} />
      Tab moves focus
    </label>
    <p class="field-help">With it on, Tab leaves the editor instead of indenting. Esc then Tab always leaves it.</p>
  </div>
</div>

<style>
  .settings {
    position: relative;
  }

  .panel {
    position: absolute;
    inset-inline-start: 0;
    top: calc(100% + var(--space-1));
    z-index: var(--z-bar);
    display: grid;
    gap: var(--space-3);
    width: min(17rem, calc(100vw - 2 * var(--gutter)));
    padding: var(--space-3);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
  }

  .panel[hidden] {
    display: none;
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-size: var(--fs-0);
    cursor: pointer;
  }

  .check input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
  }
</style>
