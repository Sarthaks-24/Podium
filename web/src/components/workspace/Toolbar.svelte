<!--
  Toolbar: the code column's one row: the language, Reset, Settings
  (components/EditorSettings), Full screen (v4 E8: the whole page, Exit full
  screen while it is; the title names F11, which browsers own), then Run and, from 1024 px, Submit at the
  end. A narrow code column (the `code` container under 36rem) drops the
  icons and narrows the select so the row holds. When Run and Submit are
  off, the reason follows on its own line (#ws-locked). Props:
  `languages`, `language` (the one shown in the select), `onlanguage`,
  `onreset`, `settings` and `onsettings` (EditorSettings' values and
  change), `locked` (the reason, or ""), `runBusy`, `onrun` and `submit`
  (a snippet, from 1024 px), and `fullscreen` (false hides Full screen: an
  exam that requires it makes its own request on Start, and leaving it there
  is recorded).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { EditorSettingsValue } from '../../lib/draft.svelte';
  import { FONT_SIZES } from '../../lib/drafts';
  import { shortcutKeys } from '../../lib/shortcuts';
  import type { Language } from '../../lib/workspace';
  import EditorSettings from '../EditorSettings.svelte';
  import Icon from '../Icon.svelte';

  interface Props {
    languages: readonly { id: Language; label: string }[];
    language: Language;
    onlanguage: (next: Language) => void;
    onreset: () => void;
    settings: EditorSettingsValue;
    onsettings: (next: Partial<EditorSettingsValue>) => void;
    locked: string;
    runBusy: boolean;
    onrun: () => void;
    submit?: Snippet;
    fullscreen?: boolean;
  }

  let { languages, language, onlanguage, onreset, settings, onsettings, locked, runBusy, onrun, submit, fullscreen = true }: Props = $props();

  // Run's key, from the shortcut table (v4 U6).
  const runKeys = shortcutKeys('Run on the examples');

  const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled === true;
  let full = $state(typeof document !== 'undefined' && document.fullscreenElement !== null);

  function toggleFullscreen() {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    else void document.documentElement.requestFullscreen().catch(() => undefined);
  }
</script>

<svelte:document onfullscreenchange={() => (full = document.fullscreenElement !== null)} />

<div class="toolbar-wrap">
  <div class="toolbar">
    <label class="visually-hidden" for="ws-language">Language</label>
    <select id="ws-language" class="select lang" value={language} onchange={(e) => onlanguage(e.currentTarget.value as Language)}>
      {#each languages as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
    </select>
    <button type="button" class="btn btn-quiet btn-compact" onclick={onreset}>Reset</button>
    <EditorSettings {...settings} sizes={FONT_SIZES} onchange={onsettings} />
    {#if fullscreen && canFullscreen}
      <button
        type="button"
        class="btn btn-quiet btn-compact fullscreen"
        aria-label={full ? 'Exit full screen' : 'Full screen'}
        title={full ? 'Exit full screen (Esc or F11)' : 'Full screen (F11 works too)'}
        onclick={toggleFullscreen}><Icon name={full ? 'minimize' : 'maximize'} /></button
      >
    {/if}
    <div class="run-actions">
      <button
        type="button"
        class="btn btn-secondary btn-compact"
        disabled={locked !== ''}
        aria-disabled={runBusy ? 'true' : undefined}
        aria-describedby={locked ? 'ws-locked' : undefined}
        title="Run on the examples ({runKeys.text})"
        aria-keyshortcuts={runKeys.aria}
        onclick={onrun}><Icon name="play" />Run</button
      >
      {@render submit?.()}
    </div>
  </div>
  {#if locked}<p class="locked" id="ws-locked">{locked}</p>{/if}
</div>

<style>
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding-bottom: var(--space-2);
  }

  .toolbar .select {
    width: auto;
  }

  .toolbar .lang {
    width: 8rem;
  }

  .run-actions {
    display: flex;
    gap: var(--space-2);
    margin-left: auto;
  }

  .locked {
    padding-bottom: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  /* A narrow code column keeps the toolbar on one row: the icons go from
     Run, Submit and Settings, and the select and buttons narrow. */
  /* A code column too narrow for one more button leaves Full screen to F11. */
  @container code (width < 42rem) {
    .fullscreen {
      display: none;
    }
  }

  @container code (width < 36rem) {
    .run-actions :global(svg),
    .toolbar :global(.settings svg) {
      display: none;
    }

    .run-actions :global(.submit) {
      min-width: 7.5rem;
    }

    .toolbar .lang {
      width: 6.5rem;
    }

    .toolbar :global(.btn) {
      padding-inline: var(--space-2);
    }
  }

  @media (min-width: 1024px) and (max-height: 45rem) {
    .toolbar {
      padding-bottom: var(--space-1);
    }
  }
</style>
