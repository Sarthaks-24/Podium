<!--
  ThemeMenu: the header's theme control. A button labelled with the current
  theme ("Theme: Nordic frost") opens a panel holding one radio group for
  the theme (System, then the Light, Dark and Accessible groups from
  lib/theme.ts, each option with a small swatch painted in that theme) and
  a checkbox for colour-blind safe verdict colours. A choice applies at
  once; when someone is signed in it is also saved to their account, so it
  follows them to another machine (decision D42). Escape or a click outside
  closes the panel, as does tabbing out of it; Escape returns focus to the
  button. A visually
  hidden status line announces each change. Takes no props.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { ApiError, sendJSON } from './api';
  import { choosePreferences, preferences } from './preferences.svelte';
  import { session } from './session.svelte';
  import { GROUPS, choiceLabel, themesIn, toAccount, type Preferences } from './theme';
  import { showToast } from './toast.svelte';

  let open = $state(false);
  let announcement = $state('');
  let button: HTMLButtonElement | undefined = $state();
  let root: HTMLDivElement | undefined = $state();
  let panel: HTMLDivElement | undefined = $state();

  const current = $derived(preferences.current);
  const panelId = 'theme-panel';

  async function change(next: Preferences, message: string) {
    choosePreferences(next);
    announcement = message;
    if (session.status !== 'signed-in') return;
    try {
      await sendJSON('PUT', '/api/me/preferences', toAccount(next));
    } catch (err) {
      const reason = err instanceof ApiError ? err.message : String(err);
      showToast(`Changed on this device only. Saving it to your account failed: ${reason}`, 'error');
    }
  }

  function pickTheme(theme: string) {
    change({ ...current, theme }, `Theme set to ${choiceLabel(theme)}`);
  }

  function toggleVerdicts(event: Event) {
    const on = (event.currentTarget as HTMLInputElement).checked;
    change(
      { ...current, verdicts: on ? 'signal' : 'standard' },
      on ? 'Colour-blind safe verdict colours on' : 'Standard verdict colours on',
    );
  }

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
    if (open && event.key === 'Escape') {
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
  <button
    type="button"
    class="trigger"
    aria-expanded={open}
    aria-controls={panelId}
    bind:this={button}
    onclick={toggle}
  >
    Theme: {choiceLabel(current.theme)}
  </button>

  <div id={panelId} class="panel" hidden={!open} bind:this={panel}>
    <fieldset class="group">
      <legend class="visually-hidden">Theme</legend>
      <label class="option">
        <input
          type="radio"
          name="theme"
          value="system"
          checked={current.theme === 'system'}
          onchange={() => pickTheme('system')}
        />
        <span class="swatch swatch-pair" aria-hidden="true">
          <span class="chips" data-theme="light"><span class="chip paper"></span><span class="chip act"></span></span>
          <span class="chips" data-theme="dark"><span class="chip paper"></span><span class="chip act"></span></span>
        </span>
        <span class="name">System <span class="hint">Light or dark, as your device is set</span></span>
      </label>

      {#each GROUPS as group (group)}
        <fieldset class="group">
          <legend class="group-name">{group}</legend>
          {#each themesIn(group) as theme (theme.id)}
            <label class="option">
              <input
                type="radio"
                name="theme"
                value={theme.id}
                checked={current.theme === theme.id}
                onchange={() => pickTheme(theme.id)}
              />
              <span class="swatch chips" data-theme={theme.id} aria-hidden="true">
                <span class="chip paper"></span><span class="chip ink"></span><span class="chip act"></span>
              </span>
              <span class="name">{theme.label}</span>
            </label>
          {/each}
        </fieldset>
      {/each}
    </fieldset>

    <label class="option verdicts">
      <input type="checkbox" checked={current.verdicts === 'signal'} onchange={toggleVerdicts} />
      <span class="name">
        Colour-blind safe verdict colours
        <span class="sample" aria-hidden="true">
          <span class="ok">Accepted</span>
          <span class="part">Partial</span>
          <span class="bad">Wrong</span>
        </span>
      </span>
    </label>
  </div>
</div>
<span class="visually-hidden" role="status">{announcement}</span>

<style>
  .theme-menu {
    position: relative;
  }

  .trigger {
    min-height: var(--control-h);
    padding-inline: var(--space-3);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--ink);
    font-size: var(--fs-0);
    font-weight: 500;
    white-space: nowrap;
    transition: border-color var(--dur-fast) var(--ease);
  }

  .trigger:hover {
    border-color: var(--ink-muted);
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

  .group {
    margin: 0;
    padding: 0;
    border: 0;
  }

  .group-name {
    padding: 0;
    margin: var(--space-3) var(--space-2) var(--space-1);
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .option {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-h);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius);
    color: var(--ink);
    cursor: pointer;
  }

  .option:hover {
    background: var(--surface-2);
  }

  .option:has(input:focus-visible) {
    box-shadow: var(--focus);
  }

  .option input {
    flex: none;
    width: 1rem;
    height: 1rem;
    margin: 0;
    accent-color: var(--act);
  }

  /* The ring is drawn on the whole row instead. */
  .option input:focus-visible {
    outline: none;
    box-shadow: none;
  }

  .name {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .hint {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .swatch {
    flex: none;
  }

  .chips {
    display: inline-flex;
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .swatch-pair {
    display: inline-flex;
    gap: var(--space-1);
  }

  .chip {
    display: block;
    width: var(--space-3);
    height: var(--space-5);
  }

  .chip.paper {
    background: var(--paper);
  }

  .chip.ink {
    background: var(--ink);
  }

  .chip.act {
    background: var(--act);
  }

  .verdicts {
    align-items: flex-start;
    margin-top: var(--space-2);
    padding-top: var(--space-3);
    border-top: var(--rule) solid var(--line);
    border-radius: 0;
  }

  .verdicts input {
    margin-top: var(--space-1);
  }

  .sample {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .sample .ok {
    color: var(--ok);
  }

  .sample .part {
    color: var(--part);
  }

  .sample .bad {
    color: var(--bad);
  }
</style>
