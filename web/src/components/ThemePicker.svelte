<!--
  ThemePicker: the theme and verdict-colour choices, shared by the
  signed-out header popover (components/ThemeMenu.svelte) and the "Theme and
  colours" dialog of the account menu (ThemeDialog.svelte). It names the
  current theme, then one radio group for the theme (System, then the
  Light, Dark and Accessible groups from lib/theme.ts, each option with a
  small swatch painted in that theme) and a checkbox for colour-blind safe
  verdict colours. A choice applies at once; when someone is signed in it
  is also saved to their account, so it follows them to another machine.
  Each change is announced politely through the app's
  announcer (lib/announce.svelte.ts). Takes no props.
-->
<script lang="ts">
  import { polite } from '../lib/announce.svelte';
  import { ApiError, sendJSON } from '../lib/api';
  import { choosePreferences, preferences } from '../lib/preferences.svelte';
  import { session } from '../lib/session.svelte';
  import { GROUPS, choiceLabel, themesIn, toAccount, type Preferences } from '../lib/theme';
  import { showToast } from '../lib/toast.svelte';

  const current = $derived(preferences.current);
  // Each picker is its own radio group, so two on one page never interfere.
  const uid = $props.id();
  const group = `theme-${uid}`;

  async function change(next: Preferences, message: string) {
    choosePreferences(next);
    polite(message);
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
</script>

<p class="current">Current theme: {choiceLabel(current.theme)}</p>
<fieldset class="group">
  <legend class="visually-hidden">Theme</legend>
  <label class="option">
    <input type="radio" name={group} value="system" checked={current.theme === 'system'} onchange={() => pickTheme('system')} />
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
          <input type="radio" name={group} value={theme.id} checked={current.theme === theme.id} onchange={() => pickTheme(theme.id)} />
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

<style>
  .current {
    margin: var(--space-1) var(--space-2) var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
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

  /* The ring is a shadow; the transparent outline shows in forced colours. */
  .option:has(input:focus-visible) {
    outline: calc(2 * var(--rule)) solid transparent;
    outline-offset: calc(2 * var(--rule));
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
