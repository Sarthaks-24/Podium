<!--
  StatementSection: the problem editor's Statement tab: title, the
  statement, limits, tags and difficulty, beside a live preview from
  1024 px. The statement is written in the workspace's own code editor
  (CodeMirror with the same keys and theme) as plain text, because no
  Markdown mode is installed; Tab leaves it, since it is one field of a
  form. Under 768 px the statement has Write and Preview tabs instead of
  the preview beside it. The preview draws the statement and its examples
  with the students' own component. There is no slug field: the server
  makes it from the title when the problem is created. Props: `draft`
  (bindable), `fields` (the field errors by key) and `tests` (for the
  examples).
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import CodeEditor from '../CodeEditor.svelte';
  import StatementPreview from '../StatementPreview.svelte';
  import { media } from '../../lib/breakpoints';
  import { DEFAULT_FONT_SIZE } from '../../lib/drafts';
  import { describedBy } from '../../lib/problemFields';
  import { DIFFICULTIES, previewSamples, type Draft, type Problem } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
    /** The stored tests, for the examples the preview shows as students see them. */
    tests?: Problem['tests'];
  }

  let { draft = $bindable(), fields, tests = [] }: Props = $props();

  const preview = $derived(previewSamples(tests, draft.samples));

  let phone = $state(!media('tablet').matches);
  let statementTab = $state<'write' | 'preview'>('write');
  let editor: CodeEditor | undefined = $state();
  /** The text the editor last reported, so a new working copy (a reload) is pushed into it. */
  let shown = draft.statement_md;

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

  $effect(() => {
    const text = draft.statement_md;
    if (text === shown) return;
    shown = text;
    editor?.setDoc(text);
  });

  function onStatement(text: string) {
    shown = text;
    draft.statement_md = text;
  }

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
</script>

<div class="top" class:with-preview={!phone}>
  <section class="details" aria-labelledby="details-heading">
    <h2 id="details-heading" class="visually-hidden" tabindex="-1">Statement and limits</h2>

    <div class="field">
      <label for="title">Title</label>
      <input
        id="title"
        aria-required="true"
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
      <div class="statement-head">
        <span class="label" id="statement-label">Statement</span>
        {#if phone}
          <div class="mini-tabs" role="tablist" aria-label="Statement">
            {#each [{ id: 'write', label: 'Write' }, { id: 'preview', label: 'Preview' }] as t (t.id)}
              <button
                type="button"
                role="tab"
                id="statement-tab-{t.id}"
                class="mini-tab"
                aria-selected={statementTab === t.id}
                aria-controls={t.id === 'write' || statementTab === 'preview' ? `statement-panel-${t.id}` : undefined}
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
        <div class="statement-editor" class:invalid={!!err('statement_md')}>
          <CodeEditor
            bind:this={editor}
            id="statement_md"
            initial={draft.statement_md}
            language="text"
            label="Statement"
            keysHint="Tab leaves the statement."
            fontSize={DEFAULT_FONT_SIZE}
            tabMovesFocus={true}
            tabSize={2}
            wrap={true}
            onchange={onStatement}
          />
        </div>
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
          aria-required="true"
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
          aria-required="true"
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

    <div class="field">
      <label for="tags">Tags (optional)</label>
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
  </section>

  {#if !phone}
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

  .label {
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

  /* A fixed height: the editor scrolls inside, so the tab stays short. */
  .statement-editor {
    height: 26rem;
    min-height: 0;
  }

  .statement-editor.invalid {
    outline: calc(2 * var(--rule)) solid var(--bad);
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

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
  }

  .row > .field {
    flex: 1 1 8rem;
    min-width: 0;
  }

  .preview {
    min-width: 0;
  }

  .preview h2 {
    font-size: var(--fs-sub);
  }

  .preview-body {
    margin-top: var(--space-3);
    padding: var(--space-4) var(--space-5);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .inline-preview {
    margin-top: 0;
    padding: var(--space-3) var(--space-4);
  }

  @media (width < 768px) {
    .statement-editor {
      height: 20rem;
    }
  }

  @media (min-width: 1024px) {
    .top.with-preview {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }

    .preview {
      position: sticky;
      top: calc(var(--sticky-bar-h, var(--space-7)) + var(--space-4));
      align-self: start;
      max-height: calc(100dvh - var(--sticky-bar-h, var(--space-7)) - 2 * var(--space-4));
      overflow-y: auto;
      overscroll-behavior: contain;
    }
  }
</style>
