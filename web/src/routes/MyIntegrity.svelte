<!--
  MyIntegrity: the student's side of integrity cases at /integrity
  (integrity-plan.md sections 5 and 7). Lists the cases an organiser shared
  with the signed-in student (GET /api/integrity/cases), newest first: the
  contest, where the case stands in words, and the evidence about the
  student's own work only, as the server words it, with their own program
  and the passages in question highlighted (CodeView). Each case has an
  explanation box saved with PUT /api/integrity/cases/{case}/explanation;
  it can be changed until a decision is made, then it reads only. Students
  never see signals, strengths or other students. The page is reached from
  "Questions" in the header and the line on Your contests, keeps a "Your
  contests" back link, and words its lead by where the cases stand, also
  once all are decided (S7, audit 01-12 and 01-13). Takes no props.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import CodeView from '../components/CodeView.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { keepHeadingFocus } from '../lib/focus';
  import { casesStanding, problemName, studentLead, studentStateText, type StudentCase, type StudentCaseList } from '../lib/review';
  import { session } from '../lib/session.svelte';

  /** The longest explanation the server accepts. */
  const MAX_EXPLANATION = 8000;

  let loadState = $state<'loading' | 'ready' | 'error'>('loading');
  let loadError = $state('');
  let data = $state.raw<StudentCaseList | null>(null);
  let drafts = $state<Record<number, string>>({});
  let saving = $state<Record<number, boolean>>({});
  let errors = $state<Record<number, string>>({});
  let saved = $state<Record<number, string>>({});

  async function load() {
    loadState = 'loading';
    try {
      const d = await getJSON<StudentCaseList>('/api/integrity/cases');
      const restore = keepHeadingFocus();
      data = { ...d, cases: d.cases ?? [] };
      drafts = Object.fromEntries(data.cases.map((c) => [c.id, c.explanation ?? '']));
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      loadState = 'error';
    }
  }

  function contestTitle(c: StudentCase): string {
    return data?.contests?.[String(c.contest_id)]?.title ?? '';
  }

  async function save(c: StudentCase) {
    const text = (drafts[c.id] ?? '').trim();
    errors = { ...errors, [c.id]: '' };
    saved = { ...saved, [c.id]: '' };
    if (text === '') {
      errors = { ...errors, [c.id]: 'Write your explanation first.' };
      return;
    }
    if ([...text].length > MAX_EXPLANATION) {
      errors = { ...errors, [c.id]: `Keep the explanation to ${MAX_EXPLANATION.toLocaleString('en-GB')} characters.` };
      return;
    }
    saving = { ...saving, [c.id]: true };
    try {
      await sendJSON<void>('PUT', `/api/integrity/cases/${c.id}/explanation`, { explanation: text });
      if (data) data = { ...data, cases: data.cases.map((x) => (x.id === c.id ? { ...x, explanation: text } : x)) };
      saved = { ...saved, [c.id]: 'Saved. You can change it until a decision is made.' };
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      const message = e instanceof ApiError ? (e.fields.explanation ?? e.message) : 'Something went wrong. Try again in a moment.';
      errors = { ...errors, [c.id]: message };
    } finally {
      saving = { ...saving, [c.id]: false };
    }
  }

  onMount(() => {
    setTitle(routeTitle('my-integrity'));
    void load();
  });
</script>

<PageHeader title="Questions about your submissions" back={{ href: '/', label: 'Your contests' }} />

{#if loadState === 'loading'}
  <PageState kind="loading" message="Loading your questions" />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={load} />
{:else if !data || data.cases.length === 0}
  <PageState kind="empty" message="Nothing to explain. If an organiser asks you about a submission, it appears here." />
{:else}
  <p class="lead">{studentLead(casesStanding(data.cases))}</p>
  {#each data.cases as c (c.id)}
    <section class="case" aria-labelledby="case-{c.id}">
      <h2 id="case-{c.id}">{c.title}</h2>
      <p class="muted small">
        {#if contestTitle(c)}{contestTitle(c)}, shared{:else}Shared{/if} <span class="mono">{formatDateTime(c.shared_at)}</span>
      </p>
      <p class="where-now">{studentStateText(c)}</p>

      <h3 class="sub">What the organiser asks about</h3>
      {#if c.evidence.length === 0}
        <p class="muted">The organiser did not attach anything to this question.</p>
      {:else}
        <ul class="evidence">
          {#each c.evidence as ev, i (i)}
            <li>
              <h4>{problemName(data.problems, ev.problem_id)}</h4>
              <p>{ev.text}</p>
              {#if ev.source}
                <p class="legend muted"><span class="swatch" aria-hidden="true"></span>Highlighted passages are the parts in question.</p>
                <CodeView source={ev.source} spans={ev.spans ?? []} label="Your program for {problemName(data.problems, ev.problem_id)}" />
              {/if}
            </li>
          {/each}
        </ul>
      {/if}

      <h3 class="sub">Your explanation</h3>
      {#if c.decided}
        {#if c.explanation.trim() !== ''}
          <p class="prose">{c.explanation}</p>
        {:else}
          <p class="muted">You did not write an explanation.</p>
        {/if}
        <p class="muted small">An organiser has decided, so you can no longer change your explanation.</p>
      {:else}
        <form
          class="explain"
          novalidate
          onsubmit={(e) => {
            e.preventDefault();
            void save(c);
          }}
        >
          <div class="field">
            <label for="explain-{c.id}">Explain how you wrote this, in your own words</label>
            <textarea
              id="explain-{c.id}"
              class="input"
              rows="6"
              maxlength={MAX_EXPLANATION}
              bind:value={drafts[c.id]}
              aria-invalid={errors[c.id] ? 'true' : undefined}
              aria-describedby="explain-{c.id}-help{errors[c.id] ? ` explain-${c.id}-error` : ''}"
            ></textarea>
            <p id="explain-{c.id}-help" class="field-help">
              For example: where the idea came from, what you looked up, and whether you worked with anyone. You can change it until a decision is made.
            </p>
            {#if errors[c.id]}<p id="explain-{c.id}-error" class="field-error" role="alert">{errors[c.id]}</p>{/if}
          </div>
          <div class="actions">
            <button type="submit" class="btn btn-primary" disabled={saving[c.id]}>Save explanation</button>
            <p class="muted small" role="status">{saved[c.id] ?? ''}</p>
          </div>
        </form>
      {/if}
    </section>
  {/each}
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: 44rem;
  }

  .small {
    font-size: var(--fs-0);
  }

  .case {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .case h2 {
    font-size: var(--fs-3);
    overflow-wrap: anywhere;
  }

  .where-now {
    font-weight: 500;
  }

  .sub {
    margin-top: var(--space-3);
    font-size: var(--fs-2);
  }

  .evidence {
    display: grid;
    gap: var(--space-5);
    padding: 0;
    list-style: none;
  }

  .evidence li {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .evidence h4 {
    font-size: var(--fs-1);
  }

  .legend {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-0);
  }

  .swatch {
    flex: none;
    width: var(--space-5);
    height: var(--space-4);
    border-left: calc(3 * var(--rule)) solid var(--code-match-edge);
    background: var(--code-match);
  }

  .prose {
    max-width: 60ch;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .explain {
    display: grid;
    gap: var(--space-3);
    max-width: 48rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
  }
</style>
