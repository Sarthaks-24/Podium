<!--
  ThemeMenu: the header's theme control for signed-out visitors. Signed in,
  the same choices live in the account menu's "Theme and colours" dialog
  (components/AccountMenu.svelte). A quiet button, the word Theme with a
  sun or moon icon for the family showing, opens a panel holding
  components/ThemePicker. Escape or a click outside closes the panel, as
  does tabbing out of it; Escape returns focus to the button. Takes no
  props.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Icon from '../components/Icon.svelte';
  import ThemePicker from '../components/ThemePicker.svelte';
  import { preferences } from './preferences.svelte';
  import { THEMES } from './theme';

  let open = $state(false);
  let button: HTMLButtonElement | undefined = $state();
  let root: HTMLDivElement | undefined = $state();
  let panel: HTMLDivElement | undefined = $state();

  const current = $derived(preferences.current);
  let systemDark = $state(false);
  const dark = $derived(
    current.theme === 'system' ? systemDark : THEMES.find((t) => t.id === current.theme)?.family === 'dark',
  );

  onMount(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)');
    const sync = () => (systemDark = query.matches);
    sync();
    query.addEventListener('change', sync);
    return () => query.removeEventListener('change', sync);
  });
  const panelId = 'theme-panel';

  // The panel hangs from the button's right edge; where the button sits
  // near the left of a narrow screen, shift the panel so it stays inside
  // the page gutters.
  function place() {
    if (!open || !panel) return;
    panel.style.setProperty('--shift', '0px');
    const gutter = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--gutter')) || 0;
    const rect = panel.getBoundingClientRect();
    const viewport = document.documentElement.clientWidth;
    let shift = 0;
    if (rect.left < gutter) shift = gutter - rect.left;
    else if (rect.right > viewport - gutter) shift = viewport - gutter - rect.right;
    panel.style.setProperty('--shift', `${shift}px`);
  }

  async function toggle() {
    open = !open;
    await tick();
    place();
  }

  function onWindowKeydown(event: KeyboardEvent) {
    if (open && event.key === 'Escape' && !event.defaultPrevented) {
      event.preventDefault();
      open = false;
      button?.focus();
    }
  }

  function onWindowPointerdown(event: PointerEvent) {
    if (open && root && !root.contains(event.target as Node)) open = false;
  }

  // Tabbing past the last option leaves the menu, so the panel closes
  // rather than staying open over the page.
  function onFocusout(event: FocusEvent) {
    const next = event.relatedTarget as Node | null;
    if (open && next && root && !root.contains(next)) open = false;
  }
</script>

<svelte:window onkeydown={onWindowKeydown} onpointerdown={onWindowPointerdown} onresize={place} />

<div class="theme-menu" bind:this={root} onfocusout={onFocusout}>
  <button type="button" class="trigger" aria-expanded={open} aria-controls={panelId} bind:this={button} onclick={toggle}>
    <Icon name={dark ? 'moon' : 'sun'} />
    Theme
  </button>

  <div id={panelId} class="panel" hidden={!open} bind:this={panel}>
    <ThemePicker />
  </div>
</div>

<style>
  .theme-menu {
    position: relative;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    padding-inline: var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 500;
    white-space: nowrap;
    transition: color var(--dur-fast) var(--ease), background-color var(--dur-fast) var(--ease);
  }

  .trigger:hover,
  .trigger[aria-expanded='true'] {
    background: var(--surface-2);
    color: var(--ink);
  }

  .panel {
    position: absolute;
    inset-inline-end: 0;
    top: calc(100% + var(--space-2));
    z-index: 20;
    width: min(20rem, calc(100vw - 2 * var(--gutter)));
    max-height: min(75vh, 40rem);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: var(--space-2);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
    translate: var(--shift, 0) 0;
  }

  .panel[hidden] {
    display: none;
  }
</style>
