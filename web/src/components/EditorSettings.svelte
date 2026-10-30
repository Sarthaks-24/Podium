<!--
  EditorSettings: the "Settings" button in the workspace toolbar and the
  small panel it opens: font size, tab size (2 or 4), word wrap and "Tab moves
  focus". A disclosure, not a menu: the panel holds form controls. Escape
  closes it and returns focus to the button, as do a click outside and
  tabbing out (the same behaviour as AccountMenu). Reset is not here; it
  sits beside the language in the toolbar. The fields are EditorSettingsFields,
  shared with the profile page. Props: `fontSize`, `sizes` (the
  choices, in px), `tabSize`, `wrap`, `tabMovesFocus`, and `onchange`,
  called with the changed setting.
-->
<script lang="ts">
  import Icon from './Icon.svelte';
  import EditorSettingsFields from './EditorSettingsFields.svelte';
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
    <EditorSettingsFields {fontSize} {sizes} {tabSize} {wrap} {tabMovesFocus} idPrefix="ws" {onchange} />
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
</style>
