<!--
  ImportConflictDialog: asks what to do with a pack whose slug is already a
  problem's. Three choices, one action button whose label follows the
  choice: import as a new problem under another slug (prefilled with a free
  one), archive the existing problem and import under its slug (listing the
  contests that pin it and have not ended), or replace the existing problem
  (the pack becomes a new version of it; unavailable, with the reason, while
  a running contest uses it). The caller sends the import and shows what
  went wrong in `error`. Props: `open`, `fileName`, `existing` (the problem
  holding the slug), `suggestedSlug`, `busy`, `error`, `onchoose` (called
  with the choice and the slug) and `oncancel`.
-->
<script lang="ts">
  import Dialog from './Dialog.svelte';
  import { archiveWarnings, IMPORT_CHOICE_LABEL, replaceBlocked, type ImportChoice, type ImportExisting } from '../lib/problems';

  interface Props {
    open: boolean;
    fileName: string;
    existing: ImportExisting;
    suggestedSlug: string;
    busy: boolean;
    error: string;
    onchoose: (choice: ImportChoice, slug: string) => void;
    oncancel: () => void;
  }

  let { open, fileName, existing, suggestedSlug, busy, error, onchoose, oncancel }: Props = $props();

  let choice = $state<ImportChoice>('rename');
  let slug = $derived(suggestedSlug);
  let slugError = $state('');
  let slugInput: HTMLInputElement | undefined = $state();

  // Each opening starts on the default choice.
  $effect(() => {
    if (!open) return;
    choice = 'rename';
    slugError = '';
  });

  const blocked = $derived(replaceBlocked(existing));
  // A replace refused as live disables its radio; move off it.
  $effect(() => {
    if (blocked && choice === 'replace') choice = 'rename';
  });
  const warnings = $derived(archiveWarnings(existing.title, [...existing.live_contests, ...existing.open_contests], existing.hidden_live_contests + existing.hidden_open_contests));

  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (choice === 'rename' && slug.trim() === '') {
      slugError = 'Enter a slug.';
      slugInput?.focus();
      return;
    }
    slugError = '';
    onchoose(choice, slug.trim());
  }
</script>

<Dialog {open} onclose={oncancel} {busy} labelledby="import-conflict-title">
  <h2 id="import-conflict-title" tabindex="-1">A problem with the slug {existing.slug} already exists</h2>
  <p class="lead muted">{fileName} uses the slug of {existing.title}. Choose what to do with this pack.</p>
  <form class="form" novalidate onsubmit={submit}>
    <fieldset>
      <legend class="visually-hidden">What to do with the pack</legend>
      <div class="option">
        <label class="choice">
          <input type="radio" name="import-choice" value="rename" bind:group={choice} />
          <span>
            <span class="label">Import as a new problem</span>
            <span class="muted small help">Keeps {existing.title} as it is and adds this pack under another slug.</span>
          </span>
        </label>
        {#if choice === 'rename'}
          <div class="field slug">
            <label for="import-slug">New slug</label>
            <input
              id="import-slug"
              bind:this={slugInput}
              class="input mono"
              type="text"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              bind:value={slug}
              aria-invalid={slugError ? 'true' : undefined}
              aria-describedby={slugError ? 'import-slug-error' : undefined}
            />
            {#if slugError}<p id="import-slug-error" class="field-error">{slugError}</p>{/if}
          </div>
        {/if}
      </div>
      <div class="option">
        <label class="choice">
          <input type="radio" name="import-choice" value="archive_existing" bind:group={choice} />
          <span>
            <span class="label">Archive the existing problem and import this one</span>
            <span class="muted small help">Hides {existing.title} from the list and gives this pack the slug {existing.slug}. You can restore it later.</span>
          </span>
        </label>
        {#if choice === 'archive_existing' && warnings.length > 0}
          <ul class="warnings small">
            {#each warnings as line (line)}<li>{line}</li>{/each}
          </ul>
        {/if}
      </div>
      <div class="option">
        <label class="choice">
          <input type="radio" name="import-choice" value="replace" bind:group={choice} disabled={blocked !== ''} aria-describedby={blocked ? 'import-replace-why' : undefined} />
          <span>
            <span class="label">Replace the existing problem</span>
            <span class="muted small help">Saves this pack as a new version of {existing.title}. Contests keep the version they use until you update them.</span>
          </span>
        </label>
        {#if blocked}<p id="import-replace-why" class="small why">{blocked}</p>{/if}
      </div>
    </fieldset>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>{IMPORT_CHOICE_LABEL[choice]}</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={oncancel}>Cancel</button>
    </div>
  </form>
</Dialog>

<style>
  .lead {
    margin-top: var(--space-2);
  }

  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-4);
  }

  fieldset {
    border: 0;
    padding: 0;
    margin: 0;
    display: grid;
    gap: var(--space-3);
  }

  .choice {
    display: flex;
    align-items: start;
    gap: var(--space-2);
    min-height: var(--control-h);
  }

  .choice input {
    margin-top: var(--space-1);
  }

  .label {
    display: block;
    font-weight: 500;
  }

  .help {
    display: block;
  }

  .slug,
  .warnings,
  .why {
    margin-left: var(--space-5);
    margin-top: var(--space-2);
  }

  .warnings {
    padding-left: var(--space-4);
    color: var(--ink-muted);
  }

  .why {
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
