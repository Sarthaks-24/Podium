<!--
  SubmissionList: the person's own submissions to this problem, newest
  first. Each row reads as when, language, outcome and score, and opens in
  place to show the full verdict panel for that submission. Props:
  `submissions` (newest first) and `onjump` (passed to each panel for
  compile error jumps). A row whose result the server hides only says
  "Submitted" and does not open. An open row has "Code" (the submitted
  code, fetched on first use; the server sends it to its author only);
  in the workspace also "Compare with mine" (`mine`, the editor's code
  now) and, where the editor takes typing, "Use this code" (`onuse`, which
  confirms first).
-->
<script lang="ts">
  import Badge from './Badge.svelte';
  import CodeView from './CodeView.svelte';
  import MonoDigits from './MonoDigits.svelte';
  import { getJSON } from '../lib/api';
  import { lineDiff } from '../lib/diff';
  import { scoreParts } from '../lib/format';
  import { formatWhen, headline, languageLabel, type Submission } from '../lib/workspace';
  import VerdictPanel from './VerdictPanel.svelte';

  interface Props {
    submissions: Submission[];
    onjump: ((line: number, col: number | null) => void) | null;
    mine?: () => string;
    onuse?: (s: Submission, source: string) => void;
  }

  let { submissions, onjump, mine, onuse }: Props = $props();

  let open = $state<Record<number, boolean>>({});
  let view = $state<Record<number, 'code' | 'compare' | undefined>>({});
  // undefined: not loaded yet; null: could not load, or not sent.
  let sources = $state<Record<number, string | null | undefined>>({});

  async function load(id: number): Promise<string | null> {
    if (typeof sources[id] === 'string') return sources[id];
    sources[id] = undefined;
    try {
      sources[id] = (await getJSON<{ source?: string }>(`/api/submissions/${id}`)).source ?? null;
    } catch {
      sources[id] = null;
    }
    return sources[id];
  }

  function toggle(id: number, next: 'code' | 'compare') {
    view[id] = view[id] === next ? undefined : next;
    if (view[id]) void load(id);
  }

  async function use(s: Submission) {
    const source = await load(s.id);
    if (source !== null) onuse?.(s, source);
  }
</script>

{#if submissions.length === 0}
  <p class="muted empty">No submissions yet. Submit when your code passes the examples.</p>
{:else}
  <ul class="list">
    {#each submissions as s (s.id)}
      {@const h = headline(s)}
      <li class="item">
        {#if s.hidden}
          <div class="row">
            <span class="when"><MonoDigits text={formatWhen(s.created_at)} /></span>
            <span class="lang">{languageLabel(s.language)}</span>
            <span class="outcome"><Badge text="Submitted" /></span>
            <span class="score"></span>
          </div>
        {:else}
          <button
            type="button"
            class="row"
            aria-expanded={open[s.id] === true}
            aria-controls="submission-{s.id}"
            onclick={() => (open[s.id] = !open[s.id])}
          >
            <span class="when"><MonoDigits text={formatWhen(s.created_at)} /></span>
            <span class="lang">{languageLabel(s.language)}</span>
            <span class="outcome"><Badge tone={h.tone} text={h.text} /></span>
            <span class="score">{#if s.state === 'done'}<MonoDigits parts={scoreParts(s.score, s.max_score)} />{/if}</span>
          </button>
          <div id="submission-{s.id}" class="body" hidden={open[s.id] !== true}>
            {#if open[s.id]}
              <VerdictPanel submission={s} {onjump} idPrefix="submission-{s.id}" />
              <div class="code-actions">
                <button type="button" class="btn btn-secondary btn-compact" aria-pressed={view[s.id] === 'code'} onclick={() => toggle(s.id, 'code')}>Code</button>
                {#if mine}
                  <button type="button" class="btn btn-secondary btn-compact" aria-pressed={view[s.id] === 'compare'} onclick={() => toggle(s.id, 'compare')}>Compare with mine</button>
                {/if}
                {#if onuse}
                  <button type="button" class="btn btn-secondary btn-compact" onclick={() => void use(s)}>Use this code</button>
                {/if}
              </div>
              {#if view[s.id]}
                {@const source = sources[s.id]}
                {#if source === undefined}
                  <p class="muted note" role="status">Loading the code</p>
                {:else if source === null}
                  <p class="field-error note">The code could not load. Choose the button again to retry.</p>
                {:else if view[s.id] === 'code'}
                  <CodeView {source} spans={[]} label="Code submitted {formatWhen(s.created_at)}" />
                {:else if mine}
                  {@const diff = lineDiff(source, mine())}
                  {#if diff === null}
                    <p class="muted note">These two are too long to compare here.</p>
                  {:else if diff.every((l) => l.kind === ' ')}
                    <p class="muted note">Same as your code now.</p>
                  {:else}
                    <p class="muted note">Lines marked - are only in this submission, + only in your code now.</p>
                    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                    <div class="compare" role="region" aria-label="This submission compared with your code" tabindex="0">
                      <pre translate="no"><code>{#each diff as l, i (i)}<span class="dl" class:del={l.kind === '-'} class:ins={l.kind === '+'}>{l.kind} {l.text}</span>{/each}</code></pre>
                    </div>
                  {/if}
                {/if}
              {/if}
            {/if}
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .empty {
    font-size: var(--fs-0);
  }

  .list {
    container-type: inline-size;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .item {
    border-bottom: var(--rule) solid var(--line);
  }

  .row {
    display: grid;
    grid-template-columns: auto auto minmax(0, 1fr) auto;
    align-items: baseline;
    gap: var(--space-3);
    width: 100%;
    min-height: var(--control-h);
    padding: var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: var(--ink);
    font-size: var(--fs-0);
    text-align: left;
  }

  button.row:hover {
    background: var(--surface-2);
  }

  .when,
  .lang,
  .score {
    color: var(--ink-muted);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .outcome {
    min-width: 0;
  }

  /* A long verdict ("Time limit exceeded on test 9") wraps inside its badge. */
  .outcome :global(.badge) {
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .body {
    padding-bottom: var(--space-3);
  }

  .body[hidden] {
    display: none;
  }

  .code-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-block: var(--space-3) var(--space-2);
  }

  .note {
    margin-bottom: var(--space-2);
    font-size: var(--fs-0);
  }

  .compare {
    max-height: 70vh;
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
    tab-size: 4;
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

  @container (max-width: 22rem) {
    .row {
      grid-template-columns: auto minmax(0, 1fr) auto;
    }

    .lang {
      display: none;
    }
  }
</style>
