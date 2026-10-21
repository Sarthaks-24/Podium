<!--
  MyCase: one question from the organisers about the student's own work,
  at /integrity/cases/{case} (v2 P4 F9.2, contract C9; the notices link
  here). The student view of a case; organisers use IntegrityCase.

  From GET /api/integrity/cases (the case is found in the student's own
  list). When the case is shown the page sends POST
  /api/integrity/cases/{case}/opened once: the first open after each
  share starts the student's 7 days to reply (without it the 21-day cap
  applies). First, where it stands: a badge, the sentence, "Reply by 26
  Oct, 18:00" and "Nothing affects your marks until a person decides."
  while it is open; once decided, the decision in words ("Zero for
  problem B in Week 6 contest") with the reason written for the student,
  then the appeal: its state and the ruling's reason, or "Appeal this
  decision" (AppealForm) while the 14 days run. Beside that from 1280 px,
  under it on smaller screens: what the organiser asks about (the
  evidence the server words for the student, with their own program and
  the passages or lines in question highlighted in CodeView), and the
  explanation box (PUT .../explanation), which reads only once decided.
  Never another student, a signal's strength or a reviewer's note.
  Props: `id` (the case).
-->
<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import CodeView from '../components/CodeView.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import AppealForm from '../components/standing/AppealForm.svelte';
  import { onMount } from 'svelte';
  import { ApiError, getJSON, sendJSON, errorText, PAGE_ERROR_MESSAGE } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDayTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { problemName, studentStateText } from '../lib/review';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { loadQuestions } from '../lib/shell.svelte';
  import { appealSentence, appealState, caseBadge, evidenceSpans, outcomeWords, type StudentAppeal, type StudentCaseListV14, type StudentCaseV14 } from '../lib/standing';

  let { id }: { id: string } = $props();

  /** The longest explanation the server accepts. */
  const MAX_EXPLANATION = 8000;

  let loadState = $state<'loading' | 'ready' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let list = $state.raw<StudentCaseListV14 | null>(null);
  let c = $state.raw<StudentCaseV14 | null>(null);
  let draft = $state('');
  let saving = $state(false);
  let error = $state('');
  let saved = $state('');
  let opened = false;
  let box: HTMLTextAreaElement | undefined = $state();

  const contestTitle = $derived(c ? (list?.contests?.[String(c.contest_id)]?.title ?? '') : '');
  const badge = $derived(c ? caseBadge(c) : null);
  const decided = $derived(c ? c.decided || c.state === 'cleared' || c.state === 'actioned' : false);

  function signedOut(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const d = await getJSON<StudentCaseListV14>('/api/integrity/cases');
      const found = (d.cases ?? []).find((x) => String(x.id) === id) ?? null;
      const restore = keepHeadingFocus();
      list = d;
      c = found;
      draft = found?.explanation ?? '';
      loadState = found ? 'ready' : 'missing';
      if (found) {
        const t = d.contests?.[String(found.contest_id)]?.title;
        setTitle(routeTitle('my-case'), ...(t ? [t] : []));
        markOpened(found.id);
      }
      void restore();
    } catch (e) {
      if (signedOut(e)) return;
      loadError = errorText(e);
      loadState = 'error';
    }
  }

  /** The student saw the case: starts their 7 days on the first open after a share. */
  function markOpened(caseId: number) {
    if (opened) return;
    opened = true;
    void sendJSON<void>('POST', `/api/integrity/cases/${caseId}/opened`).catch(() => undefined);
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!c) return;
    const text = draft.trim();
    error = '';
    saved = '';
    if (text === '') {
      error = 'Write your explanation first.';
      box?.focus();
      return;
    }
    if ([...text].length > MAX_EXPLANATION) {
      error = `Keep the explanation to ${MAX_EXPLANATION} characters.`;
      box?.focus();
      return;
    }
    saving = true;
    try {
      await sendJSON<void>('PUT', `/api/integrity/cases/${c.id}/explanation`, { explanation: text });
      c = { ...c, explanation: text };
      saved = 'Sent to the organisers. You can change it until a decision is made.';
      void loadQuestions();
    } catch (e) {
      if (signedOut(e)) return;
      error = e instanceof ApiError ? (e.fields.explanation ?? e.message) : PAGE_ERROR_MESSAGE;
    } finally {
      saving = false;
    }
  }

  function onAppeal(appeal: StudentAppeal) {
    if (c) c = { ...c, appeal, can_appeal: false };
  }

  onMount(() => {
    setTitle(routeTitle('my-case'));
    void load();
  });
</script>

<PageHeader
  title={contestTitle ? `Question about ${contestTitle}` : 'Question about your submission'}
  back={{ href: '/integrity', label: 'Questions and standing' }}
  state={null}
/>

{#if loadState === 'loading'}
  <PageState kind="loading" message="Loading the question" />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={load} />
{:else if loadState === 'missing' || !c}
  <PageState kind="missing" message="There is no question with that number for you." parent={{ href: '/integrity', label: 'Questions and standing' }} />
{:else}
  <div class="page-split case-page">
    <aside class="page-split-aside" aria-label="Where this stands">
      <section class="panel" aria-labelledby="status-title">
        <div class="panel-head">
          <h2 id="status-title" class="panel-title">Where it stands</h2>
          {#if badge}<Badge tone={badge.tone} text={badge.word} />{/if}
        </div>
        <p>{c.outcome ? 'Decided. The decision and the reason for it are below.' : studentStateText(c)}</p>
        {#if !decided}
          {#if c.respond_by}<p class="due">Reply by <MonoDigits text={formatDayTime(c.respond_by)} />.</p>{/if}
          <p class="muted">Nothing affects your marks until a person decides.</p>
        {/if}
      </section>

      {#if c.outcome}
        <section class="panel" aria-labelledby="decision-title">
          <h2 id="decision-title" class="panel-title">Decision</h2>
          <p class="outcome">{outcomeWords(c.outcome)}{contestTitle ? ` in ${contestTitle}` : ''}.</p>
          <p class="muted small">Decided <MonoDigits text={formatDayTime(c.outcome.decided_at)} /></p>
          {#if c.outcome.reason}
            <h3 class="sub">The organisers' reason</h3>
            <p class="prose">{c.outcome.reason}</p>
          {/if}

          {#if c.appeal}
            {@const st = appealState(c.appeal)}
            <div class="appeal-state">
              <h3 class="sub">Your appeal</h3>
              <p><Badge tone={st.tone} text={st.word} /></p>
              <p>{appealSentence(c.appeal)}</p>
              {#if c.appeal.ruling?.reason}<p class="prose">{c.appeal.ruling.reason}</p>{/if}
            </div>
          {:else if c.can_appeal}
            <h3 class="sub">Appeal</h3>
            <p class="muted">If you think the decision is wrong, you can ask for it to be looked at again.</p>
            <AppealForm target={{ case_id: c.id }} appealBy={c.outcome.appeal_by} id="appeal-{c.id}" onsent={onAppeal} />
          {:else if c.outcome.appeal_by !== null}
            <p class="muted small">The 14 days to appeal ended on <MonoDigits text={formatDayTime(c.outcome.appeal_by)} />.</p>
          {/if}
        </section>
      {/if}
    </aside>

    <div class="page-split-main">
      <section class="block" aria-labelledby="asks-title">
        <h2 id="asks-title">What the organiser asks about</h2>
        {#if c.evidence.length === 0}
          <p class="muted">The organiser did not attach anything to this question.</p>
        {:else}
          <ul class="evidence">
            {#each c.evidence as ev, i (i)}
              {@const spans = evidenceSpans(ev)}
              <li>
                <h3 class="sub">{problemName(list?.problems, ev.problem_id)}</h3>
                <p>{ev.text}</p>
                {#if ev.source}
                  {#if spans.length > 0}<p class="legend muted"><span class="swatch" aria-hidden="true"></span>Highlighted lines are the parts in question.</p>{/if}
                  <CodeView source={ev.source} {spans} label="Your program for {problemName(list?.problems, ev.problem_id)}" />
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="block" aria-labelledby="explain-title">
        <h2 id="explain-title">Your explanation</h2>
        {#if decided}
          {#if c.explanation.trim() !== ''}
            <p class="prose">{c.explanation}</p>
          {:else}
            <p class="muted">You did not write an explanation.</p>
          {/if}
          <p class="muted small">An organiser has decided, so you can no longer change your explanation.</p>
        {:else}
          <form class="explain" novalidate onsubmit={save}>
            <div class="field">
              <label for="explain-{c.id}">Explain how you wrote this, in your own words</label>
              <textarea
                id="explain-{c.id}"
                name="explanation"
                autocomplete="off"
                class="input"
                bind:this={box}
                rows="8"
                maxlength={MAX_EXPLANATION}
                bind:value={draft}
                aria-invalid={error ? 'true' : undefined}
                aria-describedby="explain-{c.id}-help{error ? ` explain-${c.id}-error` : ''}"
              ></textarea>
              <p id="explain-{c.id}-help" class="field-help">
                For example: where the idea came from, what you looked up, and whether you worked with anyone. You can change it until a decision is made.
              </p>
              {#if error}<p id="explain-{c.id}-error" class="field-error" role="alert">{error}</p>{/if}
            </div>
            <div class="action-row">
              <button type="submit" class="btn btn-primary" disabled={saving}>Send explanation</button>
              <p class="muted small" role="status">{saved}</p>
            </div>
          </form>
        {/if}
      </section>
    </div>
  </div>
{/if}

<style>
  .case-page {
    margin-top: var(--space-5);
  }

  .block {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .page-split-main {
    gap: var(--section-gap);
  }

  .panel-title {
    font-size: var(--fs-sub);
  }

  .sub {
    margin-top: var(--space-2);
    font-size: var(--fs-1);
  }

  .due,
  .outcome {
    font-weight: 600;
  }

  .prose {
    max-width: 60ch;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .appeal-state {
    display: grid;
    gap: var(--space-2);
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

  .explain {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-form);
  }
</style>
