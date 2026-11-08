<!--
  PromptEditor: the owner's editor for the rules text of one AI prompt (the
  wizard's, or the prompt for another AI) in the Dashboard's AI prompts tab.
  Shows where the active text comes from, the fixed part nobody can edit
  and the built-in default (both read-only), the editable rules, one diff of
  the edit against the active text, and "Save rules". "Preview the full
  prompt" is a dry run: the server returns the exact prompt it would send
  and saves nothing. The history lists every version with who saved it and
  when, with "Restore this version" (a new version with the old text) and
  "Reset to default" (a new version that means the default; an older edit
  never comes back). Warns when the built-in default changed since the
  active version was saved. A save from a stale page is refused with a
  sentence; a Reload button keeps the draft and shows it against the newer
  text. Props: `data` (one kind from GET /api/admin/ai/prompts), `onchange`
  (gets the kind the server sent back) and `onreload` (reloads the tab).
-->
<script lang="ts">
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { lineDiff } from '../../lib/diff';
  import { formatDateTime } from '../../lib/format';
  import { DEFAULT_CHANGED_WARNING, STALE_CODE, sourceLine, type PromptKindData, type PromptPreview, type PromptVersion } from '../../lib/prompts';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    data: PromptKindData;
    onchange: (kind: PromptKindData) => void;
    onreload: () => Promise<void>;
  }

  let { data, onchange, onreload }: Props = $props();

  let draft = $state('');
  let seen = $state(-1);
  let busy = $state(false);
  let error = $state('');
  let stale = $state(false);
  let previews = $state<PromptPreview[]>([]);
  let asking = $state<'reset' | PromptVersion | null>(null);

  const id = $derived(`prompt-${data.kind}`);
  const baseId = $derived(data.history[0]?.id ?? 0);
  const diff = $derived(draft === data.active.text ? [] : lineDiff(data.active.text, draft));
  const bytes = $derived(new TextEncoder().encode(draft).length);
  const path = $derived(`/api/admin/ai/prompts/${data.kind}`);

  // A new version from the server resets the draft; a reload after a stale
  // save keeps what the owner typed so it can be compared with the newer text.
  $effect(() => {
    if (baseId !== seen) {
      const first = seen === -1;
      seen = baseId;
      if (first || !stale) draft = data.active.text;
    }
  });

  async function send<T>(run: () => Promise<T>): Promise<T | undefined> {
    busy = true;
    error = '';
    stale = false;
    try {
      return await run();
    } catch (err) {
      stale = err instanceof ApiError && err.code === STALE_CODE;
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    const r = await send(() => sendJSON<{ kind: PromptKindData }>('PUT', path, { body: draft, base_id: baseId }));
    if (r) {
      previews = [];
      onchange(r.kind);
      draft = r.kind.active.text;
      showToast('Saved the rules');
    }
  }

  async function preview() {
    const r = await send(() => sendJSON<{ previews: PromptPreview[] }>('PUT', path, { body: draft, base_id: baseId, dry_run: true }));
    if (r) previews = r.previews;
  }

  async function confirm() {
    const what = asking;
    if (what === null) return;
    const r = await send(() =>
      what === 'reset'
        ? sendJSON<{ kind: PromptKindData }>('POST', `${path}/reset`, { base_id: baseId })
        : sendJSON<{ kind: PromptKindData }>('PUT', path, { restore_of: what.id, base_id: baseId }),
    );
    if (r) {
      asking = null;
      previews = [];
      onchange(r.kind);
      draft = r.kind.active.text;
      showToast(what === 'reset' ? 'Reset the rules to default' : 'Restored the version');
    }
  }

  async function reload() {
    await onreload();
    error = '';
    stale = false;
  }
</script>

<section class="section editor" aria-labelledby="{id}-heading">
  <h2 id="{id}-heading">{data.label}</h2>
  <p class="intro muted">{sourceLine(data.active)}</p>
  {#if data.default_changed}<p class="banner banner-info" role="status">{DEFAULT_CHANGED_WARNING} Compare it with the built-in default below.</p>{/if}

  <details>
    <summary>Fixed part, cannot be edited</summary>
    <textarea class="input textarea-code ro" readonly rows="8" aria-label="Fixed part of the {data.label}" value={data.fixed}></textarea>
  </details>

  <form novalidate onsubmit={save}>
    <div class="field">
      <label for="{id}-text">Rules text</label>
      <textarea
        id="{id}-text"
        class="input textarea-code"
        rows="14"
        spellcheck="false"
        bind:value={draft}
        aria-describedby="{id}-count{error ? ` ${id}-error` : ''}"
        aria-invalid={error && !stale ? 'true' : undefined}
      ></textarea>
      <p id="{id}-count" class="field-help">{bytes.toLocaleString('en-US')} of {data.limit.toLocaleString('en-US')} bytes</p>
      {#if error}<p id="{id}-error" class="field-error" role="alert">{error}</p>{/if}
    </div>

    <div class="diff-area">
      <h3>Changes against the active text</h3>
      {#if draft === data.active.text}
        <p class="muted">No changes.</p>
      {:else if diff === null}
        <p class="muted">The two texts are too long to compare here.</p>
      {:else}
        <p class="muted">Lines marked - are only in the active text, + only in your edit.</p>
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div class="compare" role="region" aria-label="Your edit compared with the active text" tabindex="0">
          <pre translate="no"><code>{#each diff as l, i (i)}<span class="dl" class:del={l.kind === '-'} class:ins={l.kind === '+'}>{l.kind} {l.text}</span>{/each}</code></pre>
        </div>
      {/if}
    </div>

    <div class="actions">
      <button type="submit" class="btn btn-secondary" disabled={busy || draft === data.active.text || draft.trim() === ''}>Save rules</button>
      <button type="button" class="btn btn-secondary" disabled={busy || draft.trim() === ''} onclick={preview}>Preview the full prompt</button>
      <button type="button" class="btn btn-secondary" disabled={busy || data.active.source !== 'database'} onclick={() => (asking = 'reset')}>Reset to default</button>
      {#if stale}<button type="button" class="btn btn-secondary" onclick={reload}>Reload</button>{/if}
    </div>
  </form>

  {#each previews as p (p.label)}
    <div class="preview">
      <h3>{p.label}, as it would be sent</h3>
      <textarea class="input textarea-code ro" readonly rows="14" aria-label="{p.label}, as it would be sent" value={p.text}></textarea>
    </div>
  {/each}

  <details>
    <summary>Built-in default</summary>
    <textarea class="input textarea-code ro" readonly rows="10" aria-label="Built-in default of the {data.label}" value={data.default}></textarea>
  </details>

  <h3>History</h3>
  {#if data.history.length === 0}
    <p class="muted">No version saved yet. The active text is the built-in or file text.</p>
  {:else}
    <ul class="history">
      {#each data.history as v, i (v.id)}
        <li>
          <span>
            Version {v.id}{v.is_reset ? ', reset to default' : ''}{i === 0 ? ', active' : ''}, by {v.by || 'an erased account'}, {formatDateTime(v.at)}.
            {#if v.default_changed}<span class="muted">The default changed since.</span>{/if}
          </span>
          {#if !v.is_reset && i !== 0}
            <button type="button" class="btn btn-link" disabled={busy} onclick={() => (asking = v)}>Restore this version</button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<ConfirmDialog
  open={asking !== null}
  title={asking === 'reset' ? 'Reset the rules to default?' : 'Restore this version?'}
  body={asking === 'reset'
    ? 'The rules go back to the default. Older versions stay in the history, and none of them comes back by itself.'
    : 'The rules become a copy of this version. The current text stays in the history.'}
  confirmLabel={asking === 'reset' ? 'Reset to default' : 'Restore this version'}
  {busy}
  {error}
  onconfirm={confirm}
  oncancel={() => (asking = null)}
/>

<style>
  .editor,
  form {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
    /* A track of minmax(0, 1fr), so a long line in the compare box scrolls inside it. */
    grid-template-columns: minmax(0, 1fr);
  }

  .field,
  .diff-area,
  .preview,
  details,
  form {
    width: 100%;
  }

  textarea {
    width: 100%;
  }

  .ro {
    background: var(--surface-2);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .history {
    display: grid;
    gap: var(--space-2);
    list-style: none;
    padding: 0;
  }

  .history li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-3);
  }

  .compare {
    max-height: 50vh;
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
  }

  .compare pre {
    width: max-content;
    min-width: 100%;
    padding-block: var(--space-2);
    font-size: var(--fs-0);
    line-height: var(--lh-body);
  }

  .dl {
    display: block;
    padding-inline: var(--space-2);
    white-space: pre;
  }

  .dl.del {
    background: var(--bad-tint);
  }

  .dl.ins {
    background: var(--ok-tint);
  }
</style>
