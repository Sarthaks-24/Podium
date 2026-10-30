<!--
  Toolbar: the code column's one row: the language, Reset, Settings
  (components/EditorSettings), then Run and, from 1024 px, Submit at the
  end. A narrow code column (the `code` container under 36rem) drops the
  icons and narrows the select so the row holds. When Run and Submit are
  off, the reason follows on its own line (#ws-locked). Props:
  `languages`, `language` (the one shown in the select), `onlanguage`,
  `onreset`, `settings` and `onsettings` (EditorSettings' values and
  change), `locked` (the reason, or ""), `runBusy`, `onrun` and `submit`
  (a snippet, from 1024 px).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { EditorSettingsValue } from '../../lib/draft.svelte';
  import { FONT_SIZES } from '../../lib/drafts';
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
  }

  let { languages, language, onlanguage, onreset, settings, onsettings, locked, runBusy, onrun, submit }: Props = $props();
</script>

<div class="toolbar-wrap">
  <div class="toolbar">
    <label class="visually-hidden" for="ws-language">Language</label>
    <select id="ws-language" class="select lang" value={language} onchange={(e) => onlanguage(e.currentTarget.value as Language)}>
      {#each languages as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
    </select>
    <button type="button" class="btn btn-quiet btn-compact" onclick={onreset}>Reset</button>
    <EditorSettings {...settings} sizes={FONT_SIZES} onchange={onsettings} />
    <div class="run-actions">
      <button
        type="button"
        class="btn btn-secondary btn-compact"
        disabled={locked !== ''}
        aria-disabled={runBusy ? 'true' : undefined}
        aria-describedby={locked ? 'ws-locked' : undefined}
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
