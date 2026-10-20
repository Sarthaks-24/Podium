<!--
  MyIntegrity: "Questions and standing" at /integrity, the student's side
  of integrity review (integrity-plan.md sections 5 and 7; v2 P4 F9.2,
  contracts C9 and C12). Reached from "Questions" in the header while a
  case exists, "Your standing" in the account menu, the line on Your
  contests and the notices. An old notice address /integrity#case-12 goes
  straight to that case's page.

  Two parts, in this order:
  - "Your standing" (GET /api/me/standing): the band's name as a badge
    ("Good standing", "Outcomes on record", "Repeated outcomes"), one
    sentence on what it means and how it changes, then the dated lines
    (outcomes and organiser adjustments), newest first, each with the
    reason written for the student, a link to its case, its appeal's
    state, and "Appeal this decision" while the 14 days run. Never a
    number: the page has no field that could hold one.
  - "Questions about your submissions" (GET /api/integrity/cases): one
    row per case, newest first: the contest, where it stands as a badge
    and a sentence, the reply deadline while it is open, and "Open the
    question" to the case page (MyCase), where the student reads the
    evidence, explains and appeals.
  Either part failing to load shows its own error and "Try again"; the
  other still shows. Takes no props.
-->
<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import AppealForm from '../components/standing/AppealForm.svelte';
  import { onMount } from 'svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDayTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { casesStanding, studentLead } from '../lib/review';
  import { navigate } from '../lib/router.svelte';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import {
    appealSentence,
    appealState,
    BAND_HELP,
    bandName,
    bandSentence,
    bandTone,
    caseBadge,
    caseFromHash,
    caseSentence,
    type Standing,
    type StandingLine,
    type StudentAppeal,
    type StudentCaseListV14,
  } from '../lib/standing';

  let standingState = $state<'loading' | 'ready' | 'error'>('loading');
  let standingError = $state('');
  let standing = $state.raw<Standing | null>(null);

  let casesState = $state<'loading' | 'ready' | 'error'>('loading');
  let casesError = $state('');
  let data = $state.raw<StudentCaseListV14 | null>(null);

  function signedOut(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  function message(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  async function loadStanding() {
    standingState = 'loading';
    try {
      const s = await getJSON<Standing>('/api/me/standing');
      const restore = keepHeadingFocus();
      standing = { ...s, lines: s.lines ?? [] };
      standingState = 'ready';
      void restore();
    } catch (e) {
      if (signedOut(e)) return;
      standingError = message(e);
      standingState = 'error';
    }
  }

  async function loadCases() {
    casesState = 'loading';
    try {
      const d = await getJSON<StudentCaseListV14>('/api/integrity/cases');
      const restore = keepHeadingFocus();
      data = { ...d, cases: d.cases ?? [] };
      casesState = 'ready';
      void restore();
    } catch (e) {
      if (signedOut(e)) return;
      casesError = message(e);
      casesState = 'error';
    }
  }

  function contestTitle(contestId: number): string {
    return data?.contests?.[String(contestId)]?.title ?? '';
  }

  function onAppeal(line: StandingLine, appeal: StudentAppeal) {
    if (!standing) return;
    standing = { ...standing, lines: standing.lines.map((l) => (l.id === line.id ? { ...l, appeal, can_appeal: false } : l)) };
  }

  onMount(() => {
    const fromHash = caseFromHash(window.location.hash);
    if (fromHash !== null) {
      navigate(`/integrity/cases/${fromHash}`, { replace: true });
      return;
    }
    setTitle(routeTitle('my-integrity'));
    void loadStanding();
    void loadCases();
  });
</script>

<PageHeader title="Questions and standing" back={{ href: '/', label: 'Your contests' }} />

<section class="part" aria-labelledby="standing-title">
  <h2 id="standing-title">Your standing</h2>
  {#if standingState === 'loading'}
    <PageState kind="loading" message="Loading your standing" />
  {:else if standingState === 'error'}
    <PageState kind="error" message={standingError} onretry={loadStanding} />
  {:else if standing}
    <div class="panel band-panel">
      <p class="band"><Badge tone={bandTone(standing.band)} text={bandName(standing)} /></p>
      <p>{bandSentence(standing.band, standing.lines.length)}</p>
      <p class="muted small">{BAND_HELP}</p>
    </div>

    {#if standing.lines.length > 0}
      <ul class="lines">
        {#each standing.lines as line (line.id)}
          <li class="line">
            <p class="line-head">
              <span class="line-text">{line.text}</span>
              <span class="muted small"><MonoDigits text={formatDayTime(line.at)} /></span>
            </p>
            {#if line.reason}<p class="prose">{line.reason}</p>{/if}
            {#if line.case_id !== null}<p><a href="/integrity/cases/{line.case_id}">See the case</a></p>{/if}
            {#if line.appeal}
              {@const st = appealState(line.appeal)}
              <p class="appeal-line"><Badge tone={st.tone} text={st.word} /> <span class="muted">{appealSentence(line.appeal)}</span></p>
            {:else if line.can_appeal}
              <AppealForm
                target={line.case_id !== null ? { case_id: line.case_id } : { adjustment_id: line.adjustment_id ?? 0 }}
                appealBy={line.appeal_by}
                id="appeal-{line.id}"
                onsent={(a) => onAppeal(line, a)}
              />
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</section>

<section class="part" aria-labelledby="questions-title">
  <h2 id="questions-title">Questions about your submissions</h2>
  {#if casesState === 'loading'}
    <PageState kind="loading" message="Loading your questions" />
  {:else if casesState === 'error'}
    <PageState kind="error" message={casesError} onretry={loadCases} />
  {:else if !data || data.cases.length === 0}
    <p class="muted">No questions. If an organiser asks you about a submission, it appears here and in your notices.</p>
  {:else}
    <p class="lead">{studentLead(casesStanding(data.cases))}</p>
    <ul class="cases panel">
      {#each data.cases as c (c.id)}
        {@const badge = caseBadge(c)}
        {@const open = !(c.decided || c.state === 'cleared' || c.state === 'actioned')}
        <li class="case" id="case-{c.id}">
          <div class="case-head">
            <h3 id="case-title-{c.id}">{contestTitle(c.contest_id) || c.title}</h3>
            <Badge tone={badge.tone} text={badge.word} />
          </div>
          <p class="muted small">Shared <MonoDigits text={formatDayTime(c.shared_at)} /></p>
          <p>{caseSentence(c)}</p>
          {#if open && c.respond_by}<p class="due">Reply by <MonoDigits text={formatDayTime(c.respond_by)} />.</p>{/if}
          <div class="action-row">
            <a class="btn {open ? 'btn-primary' : 'btn-secondary'} btn-compact" href="/integrity/cases/{c.id}" aria-describedby="case-title-{c.id}">Open the question</a>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .part {
    display: grid;
    gap: var(--space-4);
    max-width: var(--measure-form);
    margin-top: var(--space-6);
  }

  .part + .part {
    margin-top: var(--section-gap);
  }

  .band-panel {
    gap: var(--space-2);
  }

  .band {
    font-size: var(--fs-2);
  }

  .small {
    font-size: var(--fs-0);
  }

  .lines,
  .cases {
    display: grid;
    list-style: none;
  }

  .lines {
    padding: 0;
  }

  .line {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .line-head,
  .case-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-1) var(--space-3);
  }

  .line-text {
    font-weight: 600;
  }

  .prose {
    max-width: 60ch;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .appeal-line {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2);
  }

  .lead {
    max-width: 44rem;
  }

  .cases {
    gap: 0;
    padding-block: 0;
  }

  .case {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .case:last-child {
    border-bottom: 0;
  }

  .case h3 {
    min-width: 0;
    font-size: var(--fs-sub);
    overflow-wrap: anywhere;
  }

  .due {
    font-weight: 600;
  }
</style>
