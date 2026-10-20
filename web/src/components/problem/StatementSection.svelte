<!--
  StatementSection: the first section of the problem editor (v2 P3 F5.1;
  "Statement" from F5.3 A3): title, slug, the Markdown statement that
  grows with its text, limits, tags and difficulty, beside a live preview
  from 1024 px; under 1024 px the preview follows the form, under 768 px
  the statement has Write and Preview tabs instead, and from 1280 px the
  editor shows the preview in its context column (`showPreview` false).
  The checker is in JudgingSection. The preview draws the statement and
  its samples with the students' own component (A5). Props: `draft`
  (bindable), `fields` (the field errors by key), `showPreview` and
  `tests` (for the samples).
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import StatementPreview from '../StatementPreview.svelte';
  import { media } from '../../lib/breakpoints';
  import { describedBy } from '../../lib/problemFields';
  import { DIFFICULTIES, previewSamples, type Draft, type Problem } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
    /** False when the editor shows the preview in its context column instead (from 1280 px). */
    showPreview?: boolean;
    /** The stored tests, for the samples the preview shows as students see them (A5). */
    tests?: Problem['tests'];
  }

  let { draft = $bindable(), fields, showPreview = true, tests = [] }: Props = $props();

  const preview = $derived(previewSamples(tests, draft.samples));

  let phone = $state(!media('tablet').matches);
  let statementTab = $state<'write' | 'preview'>('write');

  onMount(() => {
    const query = media('tablet');
    const sync = () => {
      phone = !query.matches;
      if (!phone) statementTab = 'write';
    };
    sync();
    query.addEventListener('change', sync);
    return () => query.removeEventListener('change', sync);
  });

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, help = ''): string | undefined {
    return describedBy(fields, key, help);
  }

  // Statement tabs under 768 px follow the ARIA tabs pattern.
  function onStatementTabKey(event: KeyboardEvent) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    statementTab = statementTab === 'write' ? 'preview' : 'write';
    tick().then(() => document.getElementById(`statement-tab-${statementTab}`)?.focus());
  }

  /** Grows a textarea with its content, between its rows and 40 lines. */
  function autosize(node: HTMLTextAreaElement, value: string) {
    let last = value;
    const fit = () => {
      node.style.height = 'auto';
      const line = parseFloat(getComputedStyle(node).lineHeight) || 20;
      node.style.height = `${Math.min(node.scrollHeight + 2, line * 40)}px`;
    };
    requestAnimationFrame(fit);
    return {
      update(next: string) {
        if (next === last) return;
        last = next;
        fit();
      },
    };
  }
</script>

<div class="top" class:with-preview={showPreview && !phone}>
  <section class="details" aria-labelledby="details-heading">
    <h2 id="details-heading" tabindex="-1">Statement and limits</h2>

    <div class="field">
      <label for="title">Title</label>
      <input
        id="title"
        class="input"
        type="text"
        autocomplete="off"
        bind:value={draft.title}
        aria-invalid={err('title') ? 'true' : undefined}
        aria-describedby={described('title')}
      />
      {#if err('title')}<p id="title-error" class="field-error">{err('title')}</p>{/if}
    </div>

    <div class="field">
      <label for="slug">Slug</label>
      <input
        id="slug"
        class="input mono"
        type="text"
        autocomplete="off"
        autocapitalize="none"
        spellcheck="false"
        bind:value={draft.slug}
        aria-invalid={err('slug') ? 'true' : undefined}
        aria-describedby={described('slug', 'slug-help')}
      />
      <p id="slug-help" class="field-help">Lower case letters, digits and hyphens. Used in pack file names.</p>
      {#if err('slug')}<p id="slug-error" class="field-error">{err('slug')}</p>{/if}
    </div>

    <div class="field">
      <div class="statement-head">
        <label for="statement_md">Statement</label>
        {#if phone}
          <div class="mini-tabs" role="tablist" aria-label="Statement">
            {#each [{ id: 'write', label: 'Write' }, { id: 'preview', label: 'Preview' }] as t (t.id)}
              <button
                type="button"
                role="tab"
                id="statement-tab-{t.id}"
                class="mini-tab"
                aria-selected={statementTab === t.id}
                aria-controls="statement-panel-{t.id}"
                tabindex={statementTab === t.id ? 0 : -1}
                onclick={() => (statementTab = t.id as 'write' | 'preview')}
                onkeydown={onStatementTabKey}
              >
                {t.label}
              </button>
            {/each}
          </div>
        {/if}
      </div>
      <div
        id="statement-panel-write"
        class="statement-write"
        hidden={phone && statementTab === 'preview'}
        role={phone ? 'tabpanel' : undefined}
        aria-labelledby={phone ? 'statement-tab-write' : undefined}
      >
        <textarea
          id="statement_md"
          class="input textarea-code statement-input"
          rows="12"
          spellcheck="true"
          bind:value={draft.statement_md}
          use:autosize={draft.statement_md}
          aria-invalid={err('statement_md') ? 'true' : undefined}
          aria-describedby={described('statement_md', 'statement-help')}
        ></textarea>
        <p id="statement-help" class="field-help">Markdown. Write maths between dollar signs: $n \le 10^5$, or $$ on its own lines for a block.</p>
        {#if err('statement_md')}<p id="statement_md-error" class="field-error">{err('statement_md')}</p>{/if}
      </div>
      {#if phone && statementTab === 'preview'}
        <div id="statement-panel-preview" class="preview-body inline-preview" role="tabpanel" aria-labelledby="statement-tab-preview">
          <StatementPreview
            title={draft.title}
            markdown={draft.statement_md}
            timeLimitMs={draft.time_limit_ms}
            memoryMb={draft.memory_limit_mb}
            samples={preview.samples}
            samplesCut={preview.cut}
          />
        </div>
      {/if}
    </div>

    <div class="row">
      <div class="field">
        <label for="time_limit_ms">Time limit (ms)</label>
        <input
          id="time_limit_ms"
          class="input mono"
          type="number"
          inputmode="numeric"
          min="100"
          max="10000"
          step="100"
          bind:value={draft.time_limit_ms}
          aria-invalid={err('time_limit_ms') ? 'true' : undefined}
          aria-describedby={described('time_limit_ms')}
        />
        {#if err('time_limit_ms')}<p id="time_limit_ms-error" class="field-error">{err('time_limit_ms')}</p>{/if}
      </div>
      <div class="field">
        <label for="memory_limit_mb">Memory (MB)</label>
        <input
          id="memory_limit_mb"
          class="input mono"
          type="number"
          inputmode="numeric"
          min="16"
          max="1024"
          bind:value={draft.memory_limit_mb}
          aria-invalid={err('memory_limit_mb') ? 'true' : undefined}
          aria-describedby={described('memory_limit_mb')}
        />
        {#if err('memory_limit_mb')}<p id="memory_limit_mb-error" class="field-error">{err('memory_limit_mb')}</p>{/if}
      </div>
    </div>

    <div class="row">
      <div class="field grow">
        <label for="tags">Tags</label>
        <input
          id="tags"
          class="input"
          type="text"
          autocomplete="off"
          bind:value={draft.tags}
          aria-invalid={err('tags') ? 'true' : undefined}
          aria-describedby={described('tags', 'tags-help')}
        />
        <p id="tags-help" class="field-help">Separate tags with commas.</p>
        {#if err('tags')}<p id="tags-error" class="field-error">{err('tags')}</p>{/if}
      </div>
      <div class="field">
        <label for="difficulty">Difficulty</label>
        <select
          id="difficulty"
          class="select"
          bind:value={draft.difficulty}
          aria-invalid={err('difficulty') ? 'true' : undefined}
          aria-describedby={described('difficulty')}
        >
          {#each DIFFICULTIES as d (d.id)}<option value={d.id}>{d.label}</option>{/each}
        </select>
        {#if err('difficulty')}<p id="difficulty-error" class="field-error">{err('difficulty')}</p>{/if}
      </div>
    </div>
  </section>

  {#if showPreview && !phone}
    <section class="preview" aria-labelledby="preview-heading">
      <h2 id="preview-heading">Preview</h2>
      <div class="preview-body">
        <StatementPreview
          title={draft.title}
          markdown={draft.statement_md}
          timeLimitMs={draft.time_limit_ms}
          memoryMb={draft.memory_limit_mb}
          samples={preview.samples}
          samplesCut={preview.cut}
        />
      </div>
    </section>
  {/if}
</div>

<style>
  .top {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
    margin-top: var(--space-5);
  }

  .details {
    display: grid;
    gap: var(--space-4);
    align-content: start;
    min-width: 0;
  }

  .statement-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .statement-head label {
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .statement-write {
    display: grid;
    gap: var(--space-1);
  }

  .statement-write[hidden] {
    display: none;
  }

  .mini-tabs {
    display: flex;
    gap: var(--space-1);
  }

  .mini-tab {
    min-height: var(--control-h);
    padding-inline: var(--space-3);
    border: var(--rule) solid transparent;
    border-radius: var(--radius);
    background: none;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .mini-tab[aria-selected='true'] {
    border-color: var(--line-strong);
    background: var(--surface);
    color: var(--ink);
  }

  /* Markdown is prose, so it wraps; source code does not. */
  .statement-input {
    min-height: calc(12 * var(--lh-body) * var(--fs-0));
    resize: vertical;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
  }

  .row > .field {
    flex: 1 1 10rem;
    min-width: 0;
  }

  .row > .grow {
    flex-grow: 3;
  }

  .preview {
    min-width: 0;
  }

  .preview-body {
    margin-top: var(--space-4);
    padding: var(--space-4) var(--space-5);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .inline-preview {
    margin-top: 0;
    padding: var(--space-3) var(--space-4);
  }

  @media (min-width: 1024px) {
    .top.with-preview {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }

    .preview {
      position: sticky;
      top: calc(var(--space-7) + var(--space-4));
      align-self: start;
      max-height: calc(100vh - var(--space-7) - 2 * var(--space-4));
      overflow-y: auto;
    }
  }
</style>
