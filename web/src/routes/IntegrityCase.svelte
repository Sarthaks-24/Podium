<!--
  IntegrityCase: one integrity case at /admin/integrity/cases/{case}
  (integrity-plan.md sections 5 and 7, docs/INTEGRITY.md "Reviewing";
  v1.4.0 outcomes, the two-person rule, appeals and deadlines, v2 P4
  F9.3). From GET /api/admin/integrity/cases/{case}.

  The work column: the students, with whether and when the case was
  shared, when they first opened it, their reply deadline and their
  explanations (every saved version, with its time); the evidence copied
  when the case was opened (signals through SignalTable, whose links open
  the case's own copy; a case opened from a submission shows that
  submission and the notes copied with it); the appeals, each ruled on
  here by a reviewer who took no part (components/students/AppealRuling);
  and the decision log, oldest first, with proposals and confirmed
  outcomes, which nothing can edit.

  Beside it from 1280 px, first on smaller screens, the decision panel:
  the deadlines (reply, ready, decide by, with "Overdue" once passed), the
  policy line when a student has a prior outcome ("Policy: referral
  suggested"), then what can happen next. Every change of state needs a
  note of at least 20 characters for the log. "Share with the students"
  and "Clear the case" use PUT {state, note, seen_explanations}. An
  outcome is never set by one person: "Propose an outcome" (warning, zero
  for one problem, or referred, for some or all of the students, with a
  note for reviewers and a reason the student reads) stays disabled with
  the reason until every student has replied since the latest share or
  run out of days; a pending proposal shows who made it and the
  two-person rule as it applies to the viewer, and only a second reviewer
  who neither opened the case nor proposed the outcome can confirm it
  (the owner alone with a written reason when no other organiser
  exists). Confirming asks first and says what changes. A case closed
  with an outcome is changed only through an appeal; any other decided
  case can be reopened, which needs a new share. If a student changed
  their explanation since the page loaded, the server refuses the
  decision and the page offers a reload that keeps the note. Names and
  problem labels come from the contest's latest check when it loads.
  Props: `id` (the case).
-->
<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onDestroy, onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import IntegrityNote from '../components/IntegrityNote.svelte';
  import SignalTable from '../components/SignalTable.svelte';
  import AppealRuling from '../components/students/AppealRuling.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import type { AdminContest } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    MIN_NOTE,
    MIN_REASON,
    OUTCOMES,
    RESPONSE_DAYS,
    actionState,
    caseStateWord,
    confirmRule,
    dueText,
    joinWords,
    latestExplanationAt,
    noteCounter,
    noteLength,
    outcomePhrase,
    outcomeWord,
    pendingProposal,
    seenExplanations,
    type Case,
    type CaseAppeal,
    type CaseState,
    type CaseUser,
    type Decision,
    type Outcome,
    type ReviewUser,
    type Run,
  } from '../lib/review';
  import { session } from '../lib/session.svelte';
  import { linesText } from '../lib/students';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let kase = $state.raw<Case | null>(null);
  let run = $state.raw<Run | null>(null);
  let contestTitle = $state('');
  let labels = $state<string[]>([]);

  let note = $state('');
  let noteError = $state('');
  let actionError = $state('');
  let busy = $state(false);
  let noteField: HTMLTextAreaElement | undefined = $state();
  let confirm = $state<null | { title: string; body: string; items: string[]; label: string; danger: boolean; run: () => Promise<void> }>(null);
  // A student saved a newer explanation than the one on screen.
  let stale = $state(false);
  // The server's clock, ticking, for the response windows.
  let skew = 0;
  let now = $state(Date.now());
  let timer: ReturnType<typeof setInterval> | undefined;

  // The proposal form.
  let proposing = $state(false);
  let outcome = $state<Outcome | ''>('');
  let problemLabel = $state('');
  let forUsers = $state<number[]>([]);
  let studentReason = $state('');
  let proposalErrors = $state({ outcome: '', problem: '', users: '', reason: '' });
  // Confirming as the only organiser.
  let soleReason = $state('');
  let soleError = $state('');

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const isOwner = $derived(session.user?.role === 'owner');
  const api = $derived(`/api/admin/integrity/cases/${encodeURIComponent(id)}`);
  const back = $derived(kase ? `/admin/contests/${kase.contest_id}/integrity` : '/admin/contests');
  const action = $derived(kase ? actionState(kase, now) : { open: false, opensAt: null });
  const pending = $derived(kase ? pendingProposal(kase) : null);
  const decided = $derived(kase?.state === 'cleared' || kase?.state === 'actioned');
  /** A case closed with a confirmed outcome changes only through an appeal. */
  const lockedByOutcome = $derived(kase?.state === 'actioned' && (kase.decisions ?? []).some((d) => d.state === 'actioned' && !!d.outcome));
  const signalEvidence = $derived((kase?.evidence ?? []).filter((s) => (s.kind as string) !== 'manual'));
  const manualEvidence = $derived((kase?.evidence ?? []).filter((s) => (s.kind as string) === 'manual'));
  const proposeHelp = $derived.by(() => {
    if (!kase || action.open) return '';
    if (kase.state !== 'awaiting_student') return 'An outcome can be proposed once the case is shared with every student, so they can explain first.';
    const when = action.opensAt ? ` on ${formatDateTime(action.opensAt)}` : '';
    return `An outcome can be proposed when every student has explained since the case was shared, or when their ${RESPONSE_DAYS} days to explain end${when}.`;
  });

  function userName(uid: number): string {
    return kase?.users.find((u) => u.user_id === uid)?.name ?? users[String(uid)]?.name ?? `Student ${uid}`;
  }

  function waitingText(u: CaseUser): string {
    if (u.asked_at === null || u.asked_at === undefined || u.respond_by === null || u.respond_by === undefined) return '';
    const at = latestExplanationAt(u);
    if (at !== null && at >= u.asked_at) return '';
    return now >= u.respond_by ? 'Days to explain are over' : `Can explain until ${formatDateTime(u.respond_by)}`;
  }
  // Names for the evidence: the case's own students first, then the check's map.
  const users = $derived.by(() => {
    const out: Record<string, ReviewUser> = { ...(run?.users ?? {}) };
    for (const u of kase?.users ?? []) out[String(u.user_id)] = { name: u.name, email: out[String(u.user_id)]?.email ?? '' };
    return out;
  });

  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  function messageOf(e: unknown): string {
    return errorText(e);
  }

  function setClock(serverNow: number | undefined) {
    if (serverNow) skew = serverNow - Date.now();
    now = Date.now() + skew;
  }

  async function load() {
    loadState = 'loading';
    try {
      const c = await getJSON<Case & { server_now?: number }>(api);
      const restore = keepHeadingFocus();
      setClock(c.server_now);
      kase = c;
      stale = false;
      setTitle(c.title, routeTitle('integrity-case'));
      loadState = 'ready';
      void restore();
      void loadContext(c.contest_id);
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Case not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
    }
  }

  // Names and labels are a convenience; the case reads without them.
  async function loadContext(contestId: number) {
    const contestApi = `/api/admin/contests/${contestId}`;
    const [r, c] = await Promise.allSettled([getJSON<Run>(`${contestApi}/integrity`), getJSON<AdminContest>(contestApi)]);
    if (r.status === 'fulfilled') run = r.value;
    if (c.status === 'fulfilled') {
      contestTitle = c.value.title;
      labels = (c.value.problems ?? []).map((p) => p.label);
    }
    if (labels.length === 0 && run?.problems) labels = Object.values(run.problems).map((p) => p.label).sort();
  }

  function byName(d: { by: number; by_name?: string }): string {
    if (session.user && d.by === session.user.id) return 'you';
    if (d.by_name) return d.by_name;
    const u = run?.users?.[String(d.by)];
    return u?.name || u?.email || 'An organiser';
  }

  function decisionWord(d: Decision): string {
    if (d.state === 'actioned' && d.outcome) {
      const who = d.outcome_users && kase && d.outcome_users.length < kase.users.length ? ` for ${joinWords(d.outcome_users.map(userName))}` : '';
      return `Outcome confirmed: ${outcomeWord(d.outcome, d.outcome_problem_label)}${who}`;
    }
    return caseStateWord(d.state);
  }

  // Reloads the case after a refused decision, keeping the note.
  async function reload() {
    try {
      const c = await getJSON<Case & { server_now?: number }>(api);
      setClock(c.server_now);
      kase = c;
      stale = false;
      actionError = '';
    } catch (e) {
      if (!handleAuth(e)) actionError = messageOf(e);
    }
  }

  async function checkNote(): Promise<boolean> {
    actionError = '';
    noteError = noteLength(note) < MIN_NOTE ? `Write a note of at least ${MIN_NOTE} characters for the log first.` : '';
    if (noteError) {
      await tick();
      noteField?.focus();
      return false;
    }
    return true;
  }

  const DONE: Record<CaseState, string> = {
    open: 'Reopened the case',
    awaiting_student: 'Shared with the students',
    cleared: 'Cleared the case',
    actioned: 'Outcome confirmed',
  };

  async function ask(state: CaseState) {
    if (!(await checkNote())) return;
    if (state === 'awaiting_student') {
      confirm = {
        title: 'Share with the students?',
        body: "Each student sees their own evidence, never another student's name or code, and can write an explanation.",
        items: ['Each student gets a notice. Their 7 days to reply start when they open the case.'],
        label: 'Share with the students',
        danger: false,
        run: () => change(state),
      };
    } else {
      await change(state);
    }
  }

  /** Runs a request that answers with the case, then shows it; refusals land beside the buttons. */
  async function act(send: () => Promise<Case & { server_now?: number }>, done: string, after?: () => void) {
    busy = true;
    actionError = '';
    try {
      const c = await send();
      setClock(c.server_now);
      kase = c;
      note = '';
      noteError = '';
      after?.();
      showToast(done);
      // Close any confirmation first, so the browser's return of focus to the opener comes before ours.
      confirm = null;
      await tick();
      document.getElementById('decide-title')?.focus();
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.fields.note) noteError = e.fields.note;
      else actionError = messageOf(e);
      stale = e instanceof ApiError && e.code === 'explanation_changed';
    } finally {
      busy = false;
      confirm = null;
    }
  }

  function change(state: CaseState): Promise<void> {
    return act(() => sendJSON('PUT', api, { state, note: note.trim(), seen_explanations: kase ? seenExplanations(kase) : {} }), DONE[state]);
  }

  async function openProposal() {
    proposing = true;
    outcome = '';
    problemLabel = '';
    forUsers = (kase?.users ?? []).map((u) => u.user_id);
    studentReason = '';
    proposalErrors = { outcome: '', problem: '', users: '', reason: '' };
    await tick();
    document.getElementById('proposal-title')?.focus();
  }

  async function propose(event: SubmitEvent) {
    event.preventDefault();
    if (!kase) return;
    const reasonLen = noteLength(studentReason);
    proposalErrors = {
      outcome: outcome === '' ? 'Choose an outcome.' : '',
      problem: outcome === 'zero_problem' && problemLabel === '' ? 'Choose the problem that scores zero.' : '',
      users: forUsers.length === 0 ? 'Choose at least one student.' : '',
      reason: reasonLen === 0 ? 'Write the reason the student reads first.' : reasonLen < MIN_REASON ? `Write at least ${MIN_REASON} characters, so the student can understand it.` : '',
    };
    const noteOk = await checkNote();
    if (Object.values(proposalErrors).some((x) => x !== '')) {
      await tick();
      document.querySelector<HTMLElement>('#proposal-form :is([aria-invalid="true"], [data-invalid="true"] input)')?.focus();
      return;
    }
    if (!noteOk) return;
    const k = kase;
    const body: Record<string, unknown> = {
      outcome,
      user_ids: forUsers,
      note: note.trim(),
      reason: studentReason.trim(),
      seen_explanations: seenExplanations(k),
    };
    if (outcome === 'zero_problem') body.problem_label = problemLabel;
    await act(() => sendJSON('POST', `${api}/proposals`, body), 'Proposed the outcome. A second reviewer confirms it.', () => (proposing = false));
  }

  async function askConfirm(sole: boolean) {
    const p = pending;
    if (!p || !kase) return;
    soleError = '';
    if (!(await checkNote())) return;
    if (sole && noteLength(soleReason) < MIN_REASON) {
      soleError = `Write at least ${MIN_REASON} characters on why you confirm alone.`;
      document.getElementById('sole-reason')?.focus();
      return;
    }
    const who = joinWords(p.user_ids.map(userName));
    const items = [`${who} ${p.user_ids.length === 1 ? 'gets' : 'get'} a notice with the reason, and can appeal within 14 days.`];
    if (p.outcome === 'zero_problem') items.push(`Problem ${p.problem_label} scores zero on the board from now on, and the ranks follow.`);
    items.push('The rating goes down, and the case closes. It changes later only through an appeal.');
    if (kase.users.length > p.user_ids.length) items.push('The other students in the case are cleared.');
    confirm = {
      title: `Confirm ${outcomePhrase(p.outcome, p.problem_label)} for ${who}?`,
      body: 'The decision is final unless an appeal is upheld.',
      items,
      label: 'Confirm the outcome',
      danger: true,
      run: () =>
        act(
          () =>
            sendJSON('POST', `${api}/proposals/${p.id}/confirm`, {
              note: note.trim(),
              seen_explanations: kase ? seenExplanations(kase) : {},
              ...(sole ? { sole_organiser_reason: soleReason.trim() } : {}),
            }),
          'Outcome confirmed',
          () => (soleReason = ''),
        ),
    };
  }

  function ruled(a: CaseAppeal) {
    if (!kase) return;
    kase = { ...kase, appeals: (kase.appeals ?? []).map((x) => (x.id === a.id ? a : x)) };
    // An upheld appeal can clear the case and change the board: read it again.
    if (a.state === 'upheld') void reload();
  }

  function appealWhat(a: CaseAppeal): string {
    const d = [...(kase?.decisions ?? [])].reverse().find((x) => x.outcome && (x.outcome_users ?? []).includes(a.user_id));
    return d?.outcome ? outcomePhrase(d.outcome, d.outcome_problem_label) : 'the outcome';
  }

  onMount(() => {
    setTitle(routeTitle('integrity-case'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
    timer = setInterval(() => (now = Date.now() + skew), 30_000);
  });

  onDestroy(() => clearInterval(timer));
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Case" />
  <PageState kind="forbidden" message="Only owners and admins can review integrity." />
{:else if loadState !== 'ready' || !kase}
  <PageHeader
    title={loadState === 'missing' ? 'Case not found' : 'Case'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Case not found' : 'Case' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the case" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no case with that number, or you cannot review its contest." parent={{ href: '/admin/contests', label: 'Go to the contest list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <PageHeader
    title={kase.title}
    crumbs={[
      { label: 'Contests', href: '/admin/contests' },
      { label: contestTitle || 'Contest', href: `/admin/contests/${kase.contest_id}` },
      { label: 'Integrity', href: back },
      { label: kase.title },
    ]}
    state={{ word: caseStateWord(kase.state), kind: kase.state }}
  />
  <p class="muted small dates">
    {kase.source === 'manual' ? 'Opened from a submission' : 'Opened from a check'} <MonoDigits text={formatDateTime(kase.created_at)} />, last changed <MonoDigits
      text={formatDateTime(kase.updated_at)}
    />.
  </p>

  <div class="page-split case-split">
    <aside class="page-split-aside" aria-label="Decision">
      <section class="panel" aria-labelledby="decide-title">
        <div class="panel-head">
          <h2 id="decide-title" tabindex="-1">{decided ? 'Decided' : 'Decide'}</h2>
          {#if kase.overdue}<Badge tone="bad" text="Overdue" />{/if}
        </div>

        <dl class="clock small">
          {#each kase.users as u (u.user_id)}
            {#if u.respond_by}
              <dt>{u.name} replies by</dt>
              <dd><MonoDigits text={formatDateTime(u.respond_by)} />{#if u.first_opened_at}<span class="muted"><MonoDigits text={`, opened ${formatDateTime(u.first_opened_at)}`} /></span>{:else if u.shared_at !== null}<span class="muted">, not opened yet</span>{/if}</dd>
            {/if}
          {/each}
          {#if kase.ready_at}
            <dt>Ready to decide since</dt>
            <dd><MonoDigits text={formatDateTime(kase.ready_at)} /></dd>
          {/if}
          {#if kase.decide_by && !decided}
            <dt>Decide by</dt>
            <dd class:late={kase.overdue}><MonoDigits text={dueText(kase.decide_by, now, '')} /></dd>
          {/if}
        </dl>
        {#if kase.policy === 'referral_suggested'}
          <p class="policy small"><strong>Policy: referral suggested.</strong> A student in this case has a confirmed outcome above a warning in the last 2 years. A person still decides.</p>
        {/if}

        {#if pending}
          <div class="proposal" role="group" aria-labelledby="pending-title">
            <h3 id="pending-title">Waiting for a second reviewer</h3>
            <p class="small">
              {byName(pending) === 'you' ? 'You' : byName(pending)} proposed <strong>{outcomePhrase(pending.outcome, pending.problem_label)}</strong> for {joinWords(
                pending.user_ids.map(userName),
              )}, <MonoDigits text={formatDateTime(pending.created_at)} />.
            </p>
            <p class="small"><span class="muted">For reviewers:</span> {pending.note}</p>
            <p class="small"><span class="muted">The student reads:</span> {pending.reason}</p>
            <p class="rule small">{confirmRule(kase, pending, session.user?.id)}</p>
          </div>
        {/if}

        {#if lockedByOutcome}
          <p class="muted small">This case closed with a confirmed outcome. An outcome is changed only through an appeal.</p>
        {:else}
          <form class="decide" novalidate onsubmit={(e) => e.preventDefault()}>
            <div class="field">
              <label for="case-decision-note">Note for the log</label>
              <textarea
                id="case-decision-note"
                class="input"
                rows="3"
                bind:this={noteField}
                bind:value={note}
                aria-invalid={noteError ? 'true' : undefined}
                aria-describedby="case-decision-help case-decision-count{noteError ? ' case-decision-error' : ''}"
              ></textarea>
              <p id="case-decision-help" class="field-help">Every change needs a reason. Reviewers read it; students never do. Nothing in the log can be edited later.</p>
              <p id="case-decision-count" class="field-help">{noteCounter(note)}</p>
              {#if noteError}<p id="case-decision-error" class="field-error">{noteError}</p>{/if}
            </div>

            {#if pending && (kase.may_confirm || kase.sole_organiser)}
              {#if !kase.may_confirm && kase.sole_organiser}
                <div class="field">
                  <label for="sole-reason">Why you confirm alone</label>
                  <textarea id="sole-reason" class="input" rows="2" bind:value={soleReason} aria-invalid={soleError ? 'true' : undefined} aria-describedby={soleError ? 'sole-error' : undefined}></textarea>
                  {#if soleError}<p id="sole-error" class="field-error">{soleError}</p>{/if}
                </div>
              {/if}
              <div class="buttons">
                <button type="button" class="btn btn-primary" disabled={busy} onclick={() => askConfirm(!kase?.may_confirm)}>Confirm the outcome</button>
                <button type="button" class="btn btn-secondary" disabled={busy} onclick={() => ask('cleared')}>Clear the case</button>
              </div>
            {:else if decided}
              <div class="buttons">
                <button type="button" class="btn btn-secondary" disabled={busy} onclick={() => ask('open')}>Reopen the case</button>
              </div>
            {:else}
              <div class="buttons">
                {#if kase.state === 'open'}
                  <button type="button" class="btn btn-primary" disabled={busy} onclick={() => ask('awaiting_student')}>Share with the students</button>
                {/if}
                <button type="button" class="btn btn-secondary" disabled={busy} onclick={() => ask('cleared')}>Clear the case</button>
                {#if !pending}
                  <button
                    type="button"
                    class="btn btn-secondary btn-danger-secondary"
                    disabled={busy || !action.open || proposing}
                    aria-describedby={action.open ? undefined : 'propose-help'}
                    aria-expanded={proposing}
                    aria-controls="proposal-form"
                    onclick={openProposal}>Propose an outcome</button
                  >
                {/if}
              </div>
              {#if !pending && !action.open}
                <p id="propose-help" class="muted small">{proposeHelp}</p>
              {/if}
            {/if}
            {#if actionError}
              <div class="refused" role="alert">
                <p class="field-error">{actionError}</p>
                {#if stale}<button type="button" class="btn btn-secondary" onclick={reload}>Reload the case</button>{/if}
              </div>
            {/if}
          </form>

          {#if proposing && !pending}
            <form id="proposal-form" class="proposal-form" novalidate onsubmit={propose} aria-labelledby="proposal-title">
              <h3 id="proposal-title" tabindex="-1">Propose an outcome</h3>
              <p class="muted small">A second reviewer who did not open the case or propose it confirms it. Nothing reaches the students until then.</p>
              <fieldset class="choice-set" data-invalid={proposalErrors.outcome ? 'true' : undefined} aria-describedby={proposalErrors.outcome ? 'outcome-error' : undefined}>
                <legend>Outcome</legend>
                {#each OUTCOMES as o (o.id)}
                  <label class="choice">
                    <input type="radio" name="outcome" value={o.id} bind:group={outcome} aria-describedby="outcome-{o.id}-help" />
                    <span><span class="choice-word">{o.label}</span><span id="outcome-{o.id}-help" class="muted small help">{o.help}</span></span>
                  </label>
                {/each}
                {#if proposalErrors.outcome}<p id="outcome-error" class="field-error">{proposalErrors.outcome}</p>{/if}
              </fieldset>
              {#if outcome === 'zero_problem'}
                <div class="field">
                  <label for="outcome-problem">Problem</label>
                  <select id="outcome-problem" class="select" bind:value={problemLabel} aria-invalid={proposalErrors.problem ? 'true' : undefined} aria-describedby={proposalErrors.problem ? 'outcome-problem-error' : undefined}>
                    <option value="">Choose a problem</option>
                    {#each labels as l (l)}<option value={l}>Problem {l}</option>{/each}
                  </select>
                  {#if proposalErrors.problem}<p id="outcome-problem-error" class="field-error">{proposalErrors.problem}</p>{/if}
                </div>
              {/if}
              {#if kase.users.length > 1}
                <fieldset class="choice-set" data-invalid={proposalErrors.users ? 'true' : undefined} aria-describedby="users-help{proposalErrors.users ? ' users-error' : ''}">
                  <legend>For</legend>
                  {#each kase.users as u (u.user_id)}
                    <label class="choice"><input type="checkbox" value={u.user_id} bind:group={forUsers} /> {u.name}</label>
                  {/each}
                  <p id="users-help" class="muted small">Students left out are cleared by the outcome.</p>
                  {#if proposalErrors.users}<p id="users-error" class="field-error">{proposalErrors.users}</p>{/if}
                </fieldset>
              {/if}
              <div class="field">
                <label for="student-reason">Reason, for the student</label>
                <textarea
                  id="student-reason"
                  class="input"
                  rows="3"
                  bind:value={studentReason}
                  aria-invalid={proposalErrors.reason ? 'true' : undefined}
                  aria-describedby="student-reason-help student-reason-count{proposalErrors.reason ? ' student-reason-error' : ''}"
                ></textarea>
                <p id="student-reason-help" class="field-help">The student reads this on their standing. Say what happened, not who noticed it.</p>
                <p id="student-reason-count" class="field-help">{noteCounter(studentReason)}</p>
                {#if proposalErrors.reason}<p id="student-reason-error" class="field-error">{proposalErrors.reason}</p>{/if}
              </div>
              <p class="muted small">The note for the log above goes with the proposal, for reviewers.</p>
              <div class="buttons">
                <button type="submit" class="btn btn-primary" disabled={busy}>Propose the outcome</button>
                <button type="button" class="btn btn-quiet" disabled={busy} onclick={() => (proposing = false)}>Cancel</button>
              </div>
            </form>
          {/if}
        {/if}
      </section>
    </aside>

    <div class="page-split-main">
      <div class="note"><IntegrityNote /></div>

      <section class="section" aria-labelledby="students-title">
        <h2 id="students-title">Students</h2>
        <div class="table-wrap students">
          <table class="table table-dense table-stack">
            <caption class="visually-hidden">Students in this case</caption>
            <thead>
              <tr>
                <th scope="col">Student</th>
                <th scope="col">Shared</th>
                <th scope="col">Explanation</th>
              </tr>
            </thead>
            <tbody>
              {#each kase.users as u (u.user_id)}
                {@const waiting = waitingText(u)}
                {@const versions = u.explanations ?? []}
                <tr>
                  <th scope="row" class="s-name">
                    <a href="/admin/students/{u.user_id}">{u.name}</a>
                    {#if (u.prior_outcomes ?? 0) > 0}<span class="turn muted">{u.prior_outcomes === 1 ? '1 earlier outcome' : `${u.prior_outcomes} earlier outcomes`}</span>{/if}
                  </th>
                  <td class="s-shared">
                    {#if u.shared_at !== null}Shared <MonoDigits text={formatDateTime(u.asked_at ?? u.shared_at)} />{:else}<span class="muted">Not shared yet</span>{/if}
                    {#if u.shared_at !== null && (u.asked_at === null || u.asked_at === undefined)}<span class="turn muted">Reopened: share again for a new turn</span>{/if}
                    {#if waiting}<span class="turn muted">{waiting}</span>{/if}
                  </td>
                  <td class="s-text">
                    {#if versions.length > 1}
                      <ol class="versions" aria-label="{u.name}'s explanations, oldest first">
                        {#each versions as v, i (v.created_at)}
                          <li>
                            <p class="muted small">{i === versions.length - 1 ? 'Latest version' : `Version ${i + 1}`}, written <MonoDigits text={formatDateTime(v.created_at)} /></p>
                            <p class="prose">{v.text}</p>
                          </li>
                        {/each}
                      </ol>
                    {:else if u.explanation.trim() !== ''}
                      <p class="prose">{u.explanation}</p>
                      {#if u.explained_at !== null}<p class="muted small">Written <MonoDigits text={formatDateTime(u.explained_at)} /></p>{/if}
                    {:else}
                      <span class="muted">{u.shared_at !== null ? 'No explanation yet' : 'Can explain once shared'}</span>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </section>

      <section class="section" aria-labelledby="evidence-title">
        <h2 id="evidence-title">Evidence</h2>
        <p class="muted small">Copied when the case was opened; a later check does not change it. Each student sees only their own part.</p>
        {#if kase.evidence.length === 0}
          <p class="muted">No evidence in this case.</p>
        {/if}
        {#each manualEvidence as m, i (i)}
          {@const ev = (m.evidence ?? {}) as { notes?: { text: string; lines: { from: number; to: number }[]; by: number; created_at: number }[]; lines?: { from: number; to: number }[] }}
          <div class="manual">
            <p>
              Submission <span class="mono">{m.submission_a}</span> by {userName(m.user_a)}{run?.problems?.[String(m.problem_id)] ? ` to problem ${run.problems[String(m.problem_id)].label}` : ''}.
              {#if (ev.lines ?? []).length > 0}The student sees {linesText(ev.lines ?? [])} highlighted in their own code.{/if}
            </p>
            {#if (ev.notes ?? []).length > 0}
              <ul class="notes" aria-label="Notes copied with the submission">
                {#each ev.notes ?? [] as n, j (j)}
                  <li>
                    <p class="muted small"><MonoDigits text={formatDateTime(n.created_at)} />, {byName({ by: n.by })}{n.lines.length > 0 ? `, ${linesText(n.lines)}` : ''}</p>
                    <p class="prose">{n.text}</p>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/each}
        {#if signalEvidence.length > 0}
          <SignalTable signals={signalEvidence} {users} problems={run?.problems} contestId={kase.contest_id} caseId={kase.id} caption="Evidence in this case" showProblem />
        {/if}
      </section>

      {#if (kase.appeals ?? []).length > 0}
        <section class="section" aria-labelledby="appeals-title">
          <h2 id="appeals-title">Appeals</h2>
          <p class="muted small">A reviewer who took no part in the decision rules on each appeal within 14 days.</p>
          <div>
            {#each kase.appeals ?? [] as a (a.id)}
              <AppealRuling appeal={{ ...a, user: a.user ?? { name: userName(a.user_id), email: '', roll_number: '' } }} what={appealWhat(a)} mayRule={kase.may_rule} owner={isOwner} onruled={ruled} />
            {/each}
          </div>
        </section>
      {/if}

      <section class="section" aria-labelledby="log-title">
        <h2 id="log-title">Decision log</h2>
        {#if kase.decisions.length === 0 && (kase.proposals ?? []).length === 0}
          <p class="muted">Nothing recorded yet.</p>
        {:else}
          <ol class="log">
            {#each kase.decisions as d, i (i)}
              <li>
                <p class="log-head">
                  <span class="when"><MonoDigits text={formatDateTime(d.created_at)} /></span>
                  <span><strong>{decisionWord(d)}</strong>, by {byName(d)}</span>
                </p>
                <p class="prose">{d.note}</p>
                {#if d.sole_organiser}<p class="muted small">Confirmed alone as the only organiser: {d.sole_organiser}</p>{/if}
              </li>
            {/each}
            {#each (kase.proposals ?? []).filter((p) => p.state === 'superseded') as p (p.id)}
              <li>
                <p class="log-head">
                  <span class="when"><MonoDigits text={formatDateTime(p.created_at)} /></span>
                  <span><strong>Proposal not confirmed: {outcomePhrase(p.outcome, p.problem_label)}</strong>, by {byName(p)}</span>
                </p>
                <p class="prose">{p.note}</p>
              </li>
            {/each}
          </ol>
        {/if}
      </section>
    </div>
  </div>

  <ConfirmDialog
    open={confirm !== null}
    title={confirm?.title ?? ''}
    body={confirm?.body ?? ''}
    items={confirm?.items ?? []}
    confirmLabel={confirm?.label ?? ''}
    danger={confirm?.danger ?? false}
    {busy}
    onconfirm={() => confirm?.run()}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  .dates {
    margin-top: var(--space-2);
  }

  .case-split {
    margin-top: var(--space-5);
  }

  /* The tables need more room than a form: the work column takes what the aside leaves. */
  @media (min-width: 1280px) {
    .case-split {
      grid-template-columns: minmax(0, 1fr) minmax(0, var(--measure-aside));
    }
  }

  .small {
    font-size: var(--fs-0);
  }

  .panel h2 {
    font-size: var(--fs-section);
  }

  .section {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .section h2 {
    font-size: var(--fs-section);
  }

  .prose {
    max-width: 60ch;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .s-name {
    font-weight: 500;
    white-space: nowrap;
  }

  .s-name a {
    color: var(--ink);
  }

  .s-shared {
    white-space: nowrap;
  }

  .turn {
    display: block;
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .s-text {
    padding-block: var(--space-3);
  }

  .versions {
    display: grid;
    gap: var(--space-3);
    padding: 0;
    list-style: none;
  }

  .clock {
    display: grid;
    grid-template-columns: minmax(0, auto) minmax(0, 1fr);
    gap: var(--space-1) var(--space-3);
    margin: 0;
  }

  .clock dt {
    color: var(--ink-muted);
  }

  .clock dd {
    margin: 0;
  }

  .late {
    color: var(--bad);
    font-weight: 600;
  }

  .policy {
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--part);
    background: var(--part-tint);
  }

  .proposal {
    display: grid;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--paper);
  }

  .proposal h3,
  .proposal-form h3 {
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .rule {
    font-weight: 500;
  }

  .refused {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
  }

  .decide,
  .proposal-form {
    display: grid;
    gap: var(--space-3);
  }

  .proposal-form {
    padding-top: var(--space-4);
    border-top: var(--rule) solid var(--line);
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .choice-set {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .choice-set legend {
    margin-bottom: var(--space-1);
    font-weight: 500;
  }

  .choice {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .choice input {
    flex: none;
    width: var(--space-4);
    height: var(--space-4);
    margin: var(--space-1) 0 0;
    accent-color: var(--act);
  }

  .choice-word {
    display: block;
    font-weight: 500;
  }

  .help {
    display: block;
  }

  .manual {
    display: grid;
    gap: var(--space-2);
    padding: var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .notes {
    display: grid;
    gap: var(--space-2);
    padding: 0;
    list-style: none;
  }

  .log {
    display: grid;
    padding: 0;
    border: var(--rule) solid var(--line);
    background: var(--surface);
    list-style: none;
  }

  .log li {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .log li:last-child {
    border-bottom: 0;
  }

  .log-head {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    font-size: var(--fs-0);
  }

  .log-head strong {
    font-weight: 600;
  }

  .when {
    color: var(--ink-muted);
  }

  /* Phones: name and share state on one line, the explanation below. */
  @media (width < 768px) {
    .students tbody tr {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--space-1);
    }

    .s-shared {
      color: var(--ink-muted);
      white-space: normal;
    }
  }
</style>
