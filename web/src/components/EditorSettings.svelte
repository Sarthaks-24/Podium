<!--
  EditorSettings: the "Settings" button in the workspace toolbar and the
  small panel it opens (audit 2026-10-20 S2, cache/v3/design/editor.md
  section 1): font size, tab size (2 or 4), word wrap and "Tab moves
  focus". A disclosure, not a menu: the panel holds form controls. Escape
  closes it and returns focus to the button, as do a click outside and
  tabbing out (the same behaviour as AccountMenu). Reset is not here; it
  sits beside the language in the toolbar. Props: `fontSize`, `sizes` (the
  choices, in px), `tabSize`, `wrap`, `tabMovesFocus`, and `onchange`,
  called with the changed setting.
-->
<script lang="ts">
  import Icon from './Icon.svelte';
  import { dismissable } from '../lib/dismissable';

  interface Settings {
    fontSize: number;
    tabSize: 2 | 4;
    wrap: boolean;
    tabMovesFocus: boolean;
  }

  interface Props extends Settings {
    sizes: readonly number[];
    onchange: (next: Partial<Settings>) => void;
  }

  let { fontSize, sizes, tabSize, wrap, tabMovesFocus, onchange }: Props = $props();

  let open = $state(false);
  let button: HTMLButtonElement | undefined = $state();
</script>

<div class="settings" use:dismissable={{ open, onclose: () => (open = false), trigger: button }}>
  <button
    type="button"
    class="btn btn-quiet btn-compact"
    aria-expanded={open}
    aria-controls="ws-editor-settings"
    bind:this={button}
    onclick={() => (open = !open)}
  >
    Settings<Icon name="chevron-down" />
  </button>
  <div id="ws-editor-settings" class="panel" hidden={!open}>
    <div class="field">
      <label for="ws-font">Font size</label>
      <select id="ws-font" class="select" value={fontSize} onchange={(e) => onchange({ fontSize: Number(e.currentTarget.value) })}>
        {#each sizes as size (size)}<option value={size}>{size} px</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="ws-tab-size">Tab size</label>
      <select id="ws-tab-size" class="select" value={tabSize} onchange={(e) => onchange({ tabSize: e.currentTarget.value === '2' ? 2 : 4 })}>
        <option value={2}>2 spaces</option>
        <option value={4}>4 spaces</option>
      </select>
    </div>
    <label class="check">
      <input type="checkbox" checked={wrap} onchange={(e) => onchange({ wrap: e.currentTarget.checked })} />
      Word wrap
    </label>
    <label class="check">
      <input type="checkbox" checked={tabMovesFocus} onchange={(e) => onchange({ tabMovesFocus: e.currentTarget.checked })} />
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
