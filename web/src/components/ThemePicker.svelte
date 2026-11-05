<!--
  ThemePicker: the display choices, shared by the signed-out header popover
  (components/ThemeMenu.svelte), the "Theme and colours" dialog of the
  account menu (ThemeDialog.svelte) and the profile page. It names the
  current theme, then three sections and a reset:
  - Interface: one radio group (System, the four built-in themes by group,
    the six presets, Custom), each row with a small swatch painted in that
    theme; Custom opens CustomThemeBuilder under its row;
  - Editor colours: one radio group of the five schemes, each with a snippet
    of code in that scheme's colours;
  - Verdict colours: a checkbox for the colour-blind safe set;
  - Reset to default.
  A choice applies at once; when someone is signed in it is also saved to
  their account, so it follows them to another machine. Each change is
  announced politely through the app's announcer (lib/announce.svelte.ts).
  Takes no props.
-->
<script lang="ts">
  import { polite } from '../lib/announce.svelte';
  import { errorText, sendJSON } from '../lib/api';
  import { EDITOR_SCHEMES, SAMPLE, editorLabel } from '../lib/editor-colours';
  import { choosePreferences, preferences } from '../lib/preferences.svelte';
  import { session } from '../lib/session.svelte';
  import { CUSTOM_ID, DEFAULT_PREFERENCES, GROUPS, choiceLabel, derive, themesIn, toAccount, type Preferences } from '../lib/theme';
  import type { ThemeInput } from '../lib/theme-engine';
  import { CUSTOM_START, PRESETS } from '../lib/theme-presets';
  import { showToast } from '../lib/toast.svelte';
  import CustomThemeBuilder from './CustomThemeBuilder.svelte';

  const current = $derived(preferences.current);
  // Each picker has its own radio names, so two on one page never interfere.
  const uid = $props.id();
  const themeGroup = `theme-${uid}`;
  const editorGroup = `editor-${uid}`;

  const customValue = $derived(current.custom ?? CUSTOM_START);

  const styleOf = (vars: Record<string, string>) => Object.entries(vars).map(([k, v]) => `${k}: ${v}`).join('; ');

  async function save(next: Preferences) {
    if (session.status !== 'signed-in') return;
    try {
      await sendJSON('PUT', '/api/me/preferences', toAccount(next));
    } catch (err) {
      showToast(`Changed on this device only. It was not saved to your account: ${errorText(err)} Pick it again to retry.`, 'error');
    }
  }

  async function change(next: Preferences, message: string) {
    choosePreferences(next);
    polite(message);
    await save(next);
  }

  function pickTheme(theme: string) {
    change({ ...current, theme, custom: theme === CUSTOM_ID ? customValue : current.custom }, `Theme set to ${choiceLabel(theme)}`);
  }

  function pickEditor(editor: string) {
    change({ ...current, editor }, `Editor colours set to ${editorLabel(editor)}`);
  }

  // A colour well reports every step of a drag (commit false: apply only) and
  // the end of it (commit true: apply and save once).
  function editCustom(custom: ThemeInput, commit: boolean) {
    const next = { ...current, theme: CUSTOM_ID, custom };
    choosePreferences(next);
    if (commit) {
      polite('Custom theme updated');
      void save(next);
    }
  }

  function toggleVerdicts(event: Event) {
    const on = (event.currentTarget as HTMLInputElement).checked;
    change(
      { ...current, verdicts: on ? 'signal' : 'standard' },
      on ? 'Colour-blind safe verdict colours on' : 'Standard verdict colours on',
    );
  }

  function reset() {
    change({ ...DEFAULT_PREFERENCES }, 'Theme and colours reset to the defaults');
  }
</script>

<p class="current">Current theme: {choiceLabel(current.theme)}</p>

<fieldset class="section">
  <legend class="section-name">Interface</legend>
  <label class="option">
    <input type="radio" name={themeGroup} value="system" checked={current.theme === 'system'} onchange={() => pickTheme('system')} />
    <span class="swatch swatch-pair" aria-hidden="true">
      <span class="chips" data-theme="light"><span class="chip paper"></span><span class="chip act"></span></span>
      <span class="chips" data-theme="dark"><span class="chip paper"></span><span class="chip act"></span></span>
    </span>
    <span class="name">System <span class="hint">Light or dark, as your device is set</span></span>
  </label>

  {#each GROUPS as group (group)}
    <div class="group" role="group" aria-labelledby="{themeGroup}-{group}">
      <p class="group-name" id="{themeGroup}-{group}">{group}</p>
      {#each themesIn(group) as theme (theme.id)}
        <label class="option">
          <input type="radio" name={themeGroup} value={theme.id} checked={current.theme === theme.id} onchange={() => pickTheme(theme.id)} />
          <span class="swatch chips" data-theme={theme.id} aria-hidden="true">
            <span class="chip paper"></span><span class="chip ink"></span><span class="chip act"></span>
          </span>
          <span class="name">{theme.label}</span>
        </label>
      {/each}
    </div>
  {/each}

  <div class="group" role="group" aria-labelledby="{themeGroup}-presets">
    <p class="group-name" id="{themeGroup}-presets">Presets</p>
    {#each PRESETS as preset (preset.id)}
      {@const d = derive(preset.input)}
      <label class="option">
        <input type="radio" name={themeGroup} value={preset.id} checked={current.theme === preset.id} onchange={() => pickTheme(preset.id)} />
        <span class="swatch chips" style={styleOf(d.vars)} aria-hidden="true">
          <span class="chip paper"></span><span class="chip ink"></span><span class="chip act"></span>
        </span>
        <span class="name">{preset.label} <span class="hint">{d.family === 'light' ? 'Light' : 'Dark'}</span></span>
      </label>
    {/each}
  </div>

  <div class="group" role="group" aria-labelledby="{themeGroup}-custom">
    <p class="group-name" id="{themeGroup}-custom">Your own</p>
    <label class="option">
      <input type="radio" name={themeGroup} value={CUSTOM_ID} checked={current.theme === CUSTOM_ID} onchange={() => pickTheme(CUSTOM_ID)} />
      <span class="swatch chips" style={styleOf(derive(customValue).vars)} aria-hidden="true">
        <span class="chip paper"></span><span class="chip ink"></span><span class="chip act"></span>
      </span>
      <span class="name">Custom <span class="hint">Choose a background and an accent</span></span>
    </label>
    {#if current.theme === CUSTOM_ID}
      <CustomThemeBuilder value={customValue} onchange={editCustom} />
    {/if}
  </div>
</fieldset>

<fieldset class="section">
  <legend class="section-name">Editor colours</legend>
  {#each EDITOR_SCHEMES as scheme (scheme.id)}
    <label class="option editor">
      <input type="radio" name={editorGroup} value={scheme.id} checked={current.editor === scheme.id} onchange={() => pickEditor(scheme.id)} />
      <span class="name">
        {scheme.label} <span class="hint">{scheme.hint}</span>
        <span class="snippet" data-editor-sample={scheme.id} aria-hidden="true">
          {#each SAMPLE as line, i (i)}
            <span class="line">{#each line as [kind, text], j (j)}<span class={kind ? `code-${kind}` : undefined}>{text}</span>{/each}</span>
          {/each}
        </span>
      </span>
    </label>
  {/each}
</fieldset>

<fieldset class="section">
  <legend class="section-name">Verdict colours</legend>
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
</fieldset>

<div class="reset">
  <button type="button" class="btn btn-quiet btn-compact" onclick={reset}>Reset to default</button>
</div>

<style>
  .current {
    margin: var(--space-1) var(--space-2) var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .section {
    min-width: 0; /* a fieldset defaults to min-content; the code sample would push it past 320 px */
    margin: 0;
    padding: 0;
    border: 0;
  }

  .section + .section,
  .reset {
    margin-top: var(--space-2);
    padding-top: var(--space-2);
    border-top: var(--rule) solid var(--line);
  }

  .section-name {
    padding: 0;
    margin: var(--space-1) var(--space-2) var(--space-1);
    color: var(--ink);
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .group {
    margin: 0;
    padding: 0;
    border: 0;
  }

  .group-name {
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

  .editor {
    align-items: flex-start;
  }

  .editor input {
    margin-top: var(--space-1);
  }

  /* Painted in the scheme's own colours: tokens.css sets them on [data-editor-sample]. */
  .snippet {
    display: block;
    margin-top: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: var(--rule) solid var(--line-strong);
    background: var(--well);
    color: var(--well-ink);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    line-height: 1.4;
    white-space: pre;
    overflow-x: auto;
  }

  .line {
    display: block;
  }

  .code-keyword {
    color: var(--code-keyword);
  }

  .code-string {
    color: var(--code-string);
  }

  .code-number {
    color: var(--code-number);
  }

  .code-comment {
    color: var(--code-comment);
    font-style: italic;
  }

  .code-type {
    color: var(--code-type);
  }

  .code-function {
    color: var(--code-function);
  }

  .verdicts {
    align-items: flex-start;
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

  .reset {
    padding-inline: var(--space-2);
  }
</style>
