<!--
  ContestPage: one contest for a student, at /contests/{slug} and
  /contests/{slug}/problems/{label} (cache/v1/m4-plan.md section 5,
  design.md 5.3). The page follows the student's own phase on the server's
  clock (lib/contests.ts studentPhase):

  - Before the start, and before they agree (audit 2026-10-20 S3): the
    countdown as the page's display figure, then the honour code in a
    panel (what is recorded and for how long, D47) holding "I agree, start
    the contest" (POST /api/contests/{slug}/start). Beside it from 1280 px
    (page-split), below it on smaller screens: the description, the rules
    as sentences (with the three that teach Run and Submit) and "How the
    workspace works" with the real VerdictPanel (and its SubtaskStrip) on a
    fixed example, both closed once the student has started a contest on
    this device.
    The button waits for the start in a fixed-start contest; when the start
    arrives the announcer says so. A late arrival reads that their time is
    already running.
  - During their window: the shared Workspace on the contest's routes with
    the rail of problem letters (score and 2 px bar), the contest clock and
    freeze line, the Standings link and Messages. Exam contests show only
    "Submitted". Announcements and answered questions newer than the last
    time the student read the messages on this device (localStorage
    podium-messages-read:{slug}, audit S1) show in a quiet banner above the
    title and as the count on Messages (from 1024 px the
    editor never moves); a second open tab or a second session reported
    by the server shows a notice there too. Exam mode marks submitted
    problems "submitted" on the rail. "Rules" in the rail opens the same
    rules (D114). When an organiser moves a problem to a new revision
    (statement_updated_at, or the `statement` event), a line says
    "Statement updated 14:32. See what changed", which opens the line diff
    (StatementDiff, contract C3), and the open problem's statement reloads.
    A frozen contest reads "Live" to students (D84). "[" and "]" open the previous and
    next problem when focus is not in a text field (lib/shortcuts.ts).
  - After it (audit 2026-10-20 S9): the place as the display figure
    (PlaceFigure), one lead "Ended 21 Oct, 00:11. You scored 40 of 300.",
    each problem's score, the student's own submissions with the problem
    of the newest one open, the editorial once published, and practice
    when the organisers left it open (submissions then do not count
    towards the standings). Announcements and "Your questions" with their
    answers sit beside it from 1280 px. A board still frozen after the end
    gives the place only, never a frozen total beside the live score; when
    the clock ran out on this page, "Time is up" stays on screen. Once the
    board is final (full and every window closed, never while frozen or
    held), "Your result" reads GET /api/me/progress for the one problem
    to practise next and links "See your progress" (v2 P4 F4b.3); a
    problem a review decision set to zero says so, with a link to the
    case (D77, F9.2).

  Props: `slug`, and `label` (the problem letter from the address, or "").
-->
<script lang="ts">
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageState from '../components/PageState.svelte';
  import PlaceFigure from '../components/PlaceFigure.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import ContestBanner from '../components/ContestBanner.svelte';
  import RulesDialog from '../components/RulesDialog.svelte';
  import StatementDiff from '../components/StatementDiff.svelte';
  import VerdictPanel from '../components/VerdictPanel.svelte';
  import ContestClock from '../components/ContestClock.svelte';
  import Markdown from '../components/Markdown.svelte';
  import MessagesDialog from '../components/MessagesDialog.svelte';
  import MessageList from '../components/MessageList.svelte';
  import ProblemRail from '../components/ProblemRail.svelte';
  import StreamNotice from '../components/StreamNotice.svelte';
  import SubmissionList from '../components/SubmissionList.svelte';
  import Workspace from '../components/Workspace.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import {
    CONTEST_EVENTS,
    bannerItems,
    markMessagesRead,
    messagesReadAt,
    formatCountdown,
    formatSpan,
    freezeAt,
    hasAccepted,
    isForContest,
    lengthText,
    rulesSentences,
    startLine,
    studentPhase,
    studentState,
    studentStateWord,
    type Clarification,
    type Contest,
  } from '../lib/contests';
  import { polite } from '../lib/announce.svelte';
  import { keepHeadingFocus } from '../lib/focus';
  import { statementBannerKey, statementUpdatedText, type StatementEvent } from '../lib/history';
  import { formatDateTime, formatDayTime, scoreParts } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { navigate } from '../lib/router.svelte';
  import { adjacentLabel, isPlainKey, isTypingTarget } from '../lib/shortcuts';
  import { session } from '../lib/session.svelte';
  import { shell } from '../lib/shell.svelte';
  import { placeFigure, placeSentence, type Standings } from '../lib/standings';
  import { contestEntry, practiseNextIn, problemHref, type Progress, type ProgressContest } from '../lib/progress';
  import { EventStream, type StreamMode } from '../lib/stream';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';
  import { EXAMPLE_SUBMISSION, type Submission, type Workspace as WorkspaceData } from '../lib/workspace';

  let { slug, label = '' }: { slug: string; label?: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<Contest | null>(null);

  let startBusy = $state(false);
  let startError = $state('');
  let messagesOpen = $state(false);
  let dismissed = $state<Set<string>>(new Set());
  /** The last time the student read this contest's messages on this device (S1). */
  let lastRead = $state(0);
  let otherTab = $state(false);
  let sessionNoticeHidden = $state(false);
  let streamMode = $state<StreamMode>('connecting');
  let timeUp = $state(false);
  let rulesOpen = $state(false);
  let diffLabel = $state<string | null>(null);
  /** Raised when the open problem moved to a new revision, so its statement reloads. */
  let statementKey = $state(0);
  let workspaceSeen = $state(false);

  // After the end: the student's place and their submissions per problem.
  let standings = $state.raw<Standings | null>(null);
  let history = $state<Record<string, Submission[] | 'loading' | 'error'>>({});
  /** The problem whose disclosure opens first: the one with the newest submission (S9). */
  let latestLabel = $state('');

  const api = $derived(`/api/contests/${encodeURIComponent(slug)}`);
  const phase = $derived(contest ? studentPhase(contest, ticker.now) : null);
  const problems = $derived(contest?.problems ?? []);
  const current = $derived(label !== '' ? label : (problems[0]?.label ?? ''));
  const currentProblem = $derived(problems.find((p) => p.label === current) ?? null);
  const exam = $derived(contest?.rules.feedback_mode === 'exam');
  const practice = $derived(contest?.state === 'practice');
  const banner = $derived(contest ? bannerItems(contest, dismissed, lastRead) : []);
  const showWorkspace = $derived(
    contest !== null && currentProblem !== null && (phase === 'during' || (phase === 'after' && label !== '')),
  );
  const totalScore = $derived(problems.reduce((n, p) => n + (p.my_score ?? 0), 0));
  const totalMax = $derived(problems.reduce((n, p) => n + p.max_score, 0));
  const secondSession = $derived(!sessionNoticeHidden && (otherTab || contest?.second_session === true));
  // The newest statement change after the start that the student has not dismissed (C3).
  const statementNotice = $derived.by(() => {
    let best: { label: string; at: number } | null = null;
    for (const p of problems) {
      const at = p.statement_updated_at;
      if (typeof at !== 'number' || dismissed.has(statementBannerKey(p.label, at))) continue;
      if (!best || at > best.at || (at === best.at && p.label === current)) best = { label: p.label, at };
    }
    return best;
  });
  const startNow = $derived(contest ? startLine(contest, ticker.now) : { kind: 'before' as const, ms: 0 });
  const used = $derived(
    contest && currentProblem && phase === 'during' ? { count: currentProblem.my_submission_count ?? 0, max: contest.rules.max_per_problem } : null,
  );

  $effect(() => {
    shell.workspace = showWorkspace;
  });

  function dismissKey(): string {
    return `podium-banner-dismissed:${slug}`;
  }

  function readDismissed(): Set<string> {
    try {
      const raw = sessionStorage.getItem(dismissKey());
      const list: unknown = raw ? JSON.parse(raw) : [];
      return new Set(Array.isArray(list) ? list.filter((x): x is string => typeof x === 'string') : []);
    } catch {
      return new Set();
    }
  }

  function dismiss(key: string) {
    // Dismissing the last waiting message counts as reading them all.
    const waiting = banner.filter((i) => i.key !== key);
    if (banner.some((i) => i.key === key) && waiting.length === 0) lastRead = markMessagesRead(slug, Math.max(...banner.map((i) => i.at)));
    const next = new Set([...dismissed, key]);
    dismissed = next;
    try {
      sessionStorage.setItem(dismissKey(), JSON.stringify([...next]));
    } catch {
      // Dismissed for this page only.
    }
  }

  function openMessages() {
    // Opening the messages counts as reading everything the page has loaded.
    lastRead = markMessagesRead(slug, Math.max(contest?.server_now ?? 0, ...banner.map((i) => i.at)));
    messagesOpen = true;
  }

  function handleSessionError(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  async function load(quiet = false) {
    if (!quiet) loadState = 'loading';
    try {
      const sent = Date.now();
      const data = await getJSON<Contest>(api);
      serverClock.observe(data.server_now, sent, Date.now());
      refreshTicker();
      const restore = keepHeadingFocus();
      contest = data;
      setTitle(data.title);
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (handleSessionError(e)) return;
      if (quiet) return;
      if (e instanceof ApiError && e.status === 403) {
        loadError = e.message;
        loadState = 'forbidden';
        setTitle(routeTitle('contest'));
      } else if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Contest not found');
      } else {
        loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
        loadState = 'error';
        setTitle(routeTitle('contest'));
      }
    }
  }

  // Refetches are coalesced: a burst of events costs one request.
  let refetchTimer: ReturnType<typeof setTimeout> | undefined;
  function refetchSoon(ms = 400) {
    clearTimeout(refetchTimer);
    refetchTimer = setTimeout(() => void load(true), ms);
  }

  function onEvent(type: string, data: unknown) {
    if (!contest) return;
    if (type === 'submission') {
      const s = data as Partial<Submission>;
      // Exam submissions finish as "submitted"; the rail then marks the problem.
      if (s && (s.state === 'done' || s.state === 'error' || s.state === 'submitted')) refetchSoon();
      return;
    }
    if (!isForContest(data, contest)) return;
    if (type === 'statement') {
      const ev = data as Partial<StatementEvent>;
      if (ev.label === current) statementKey++;
      if (ev.statement_changed) polite(`The statement of problem ${ev.label} was updated. See what changed is above the problem.`);
      refetchSoon(0);
      return;
    }
    if (type === 'announcement' || type === 'clarification' || type === 'contest') refetchSoon(type === 'contest' ? 0 : 400);
  }

  async function start() {
    startError = '';
    startBusy = true;
    try {
      const sent = Date.now();
      const data = await sendJSON<Contest>('POST', `${api}/start`, { accept_honour: true });
      if (data && typeof data === 'object' && 'slug' in data) {
        serverClock.observe(data.server_now, sent, Date.now());
        refreshTicker();
        contest = { ...data, accepted: data.accepted ?? true };
        markWorkspaceSeen();
      } else {
        await load(true);
      }
    } catch (e) {
      if (handleSessionError(e)) return;
      startError = e instanceof ApiError ? (e.fields.accept_honour ?? e.message) : 'Something went wrong. Try again in a moment.';
    } finally {
      startBusy = false;
    }
  }

  function onAsked(c: Clarification | null) {
    if (c && contest) contest = { ...contest, my_clarifications: [c, ...(contest.my_clarifications ?? [])] };
    else refetchSoon(0);
  }

  // A phase change (the start arriving, the window closing) reloads the contest.
  let lastPhase: string | null = null;
  $effect(() => {
    const p = phase;
    if (p === null) return;
    if (lastPhase !== null && lastPhase !== p) untrack(() => refetchSoon(0));
    // The clock ran out on this page: say so on the page that replaces the workspace.
    // Focus goes to the new page's h1 (P5 F7.4); the workspace under it is being replaced.
    if (lastPhase === 'during' && p === 'after') {
      timeUp = true;
      void tick().then(() => document.querySelector<HTMLElement>('main h1')?.focus());
    }
    // The start arrived while the student waited on this page (audit 01-16).
    if (lastPhase === 'upcoming' && p === 'ready') polite('The contest has started. You can start now.');
    lastPhase = p;
  });

  // Outside the workspace this page keeps its own stream for contest events.
  let stream: EventStream | null = null;
  $effect(() => {
    const needStream = loadState === 'ready' && !showWorkspace;
    untrack(() => {
      if (needStream && !stream) {
        stream = new EventStream({
          url: '/api/events',
          events: ['submission', ...CONTEST_EVENTS],
          onEvent,
          onReconnect: () => refetchSoon(0),
          poll: () => void load(true),
          onMode: (m) => (streamMode = m),
        });
        stream.start();
      } else if (!needStream && stream) {
        stream.stop();
        stream = null;
      }
    });
  });

  // After the end: where the student finished, if the board is open to them.
  $effect(() => {
    if (phase !== 'after' || label !== '') return;
    untrack(async () => {
      try {
        standings = await getJSON<Standings>(`${api}/standings`);
      } catch {
        standings = null;
      }
    });
  });

  // Once the board is final, this contest's entry in "Your progress" (F4b.3).
  // Never asked while the board is frozen or held: those results are not final.
  let progressEntry = $state.raw<ProgressContest | null>(null);
  let progressAsked = false;
  $effect(() => {
    const s = standings;
    if (!s || s.variant !== 'full' || !s.final || progressAsked) return;
    progressAsked = true;
    untrack(async () => {
      try {
        progressEntry = contestEntry(await getJSON<Progress>('/api/me/progress'), slug);
      } catch {
        progressEntry = null;
      }
    });
  });
  const practiseNext = $derived(practiseNextIn(progressEntry));

  /** The student's own mark on a problem a review decision set to zero (contract C9). */
  function zeroCase(p: object): number | null {
    const own = p as { zero_by_decision?: boolean; zero_case_id?: number | null };
    if (own.zero_by_decision !== true) return null;
    return typeof own.zero_case_id === 'number' ? own.zero_case_id : 0;
  }

  async function loadHistory(l: string) {
    if (history[l] && history[l] !== 'error') return;
    history[l] = 'loading';
    try {
      const ws = await getJSON<WorkspaceData>(`${api}/problems/${encodeURIComponent(l)}`);
      history[l] = ws.submissions ?? [];
    } catch {
      history[l] = 'error';
    }
  }

  // After the end: load the submissions of every problem the student tried
  // and open the one with the newest submission (S9).
  $effect(() => {
    if (phase !== 'after' || label !== '') return;
    const tried = problems.filter((p) => (p.my_submission_count ?? 0) > 0).map((p) => p.label);
    untrack(async () => {
      await Promise.all(tried.map((l) => loadHistory(l)));
      let best = { label: '', at: -1 };
      for (const l of tried) {
        const h = history[l];
        const at = Array.isArray(h) ? Math.max(-1, ...h.map((s) => s.created_at)) : -1;
        if (at > best.at) best = { label: l, at };
      }
      latestLabel = best.label;
    });
  });

  function onToggle(event: Event, l: string) {
    if ((event.currentTarget as HTMLDetailsElement).open) void loadHistory(l);
  }

  // "[" and "]" open the previous and next problem, unless the student is
  // typing (both are everyday characters in code) or a dialog is open.
  function onKeydown(event: KeyboardEvent) {
    if (!showWorkspace || event.defaultPrevented || !isPlainKey(event)) return;
    if (event.key !== '[' && event.key !== ']') return;
    if (isTypingTarget(event.target) || document.querySelector('dialog[open]')) return;
    const next = adjacentLabel(
      problems.map((p) => p.label),
      current,
      event.key === '[' ? -1 : 1,
    );
    if (next === null) return;
    event.preventDefault();
    navigate(`/contests/${encodeURIComponent(slug)}/problems/${encodeURIComponent(next)}`);
  }

  // "How the workspace works" stays open until the student first starts a contest on this device.
  function seenKey(): string {
    return `podium-workspace-seen:${session.user?.id ?? 0}`;
  }

  function markWorkspaceSeen() {
    try {
      localStorage.setItem(seenKey(), '1');
    } catch {
      // Shown open again next time; nothing else depends on it.
    }
  }

  // A second tab with this contest open on this device.
  let channel: BroadcastChannel | null = null;

  onMount(() => {
    dismissed = readDismissed();
    lastRead = messagesReadAt(slug);
    try {
      workspaceSeen = localStorage.getItem(seenKey()) === '1';
    } catch {
      workspaceSeen = false;
    }
    // Load the Markdown renderer now, so the rules never flash as raw text.
    import('../lib/markdown').catch(() => undefined);
    void load();
    const stopTicker = useTicker();
    if (typeof BroadcastChannel !== 'undefined') {
      try {
        channel = new BroadcastChannel(`podium-contest:${slug}`);
        channel.onmessage = (ev) => {
          if (ev.data === 'hello') channel?.postMessage('here');
          if (ev.data === 'hello' || ev.data === 'here') otherTab = true;
        };
        channel.postMessage('hello');
      } catch {
        channel = null;
      }
    }
    return stopTicker;
  });

  onDestroy(() => {
    clearTimeout(refetchTimer);
    stream?.stop();
    channel?.close();
    shell.workspace = false;
  });
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet clockSnippet(compact: boolean)}
  {#if contest?.my_window}
    <ContestClock
      endsAt={contest.my_window.ends_at}
      freeze={freezeAt(contest)}
      {compact}
      onexpire={() => {
        timeUp = true;
        refetchSoon(0);
      }}
    />
  {/if}
{/snippet}

{#snippet bannerSnippet()}
  {#if secondSession}
    <div class="banner session-notice" role="status">
      <p>
        {otherTab
          ? 'This contest is also open in another tab or window on this device. Keep to one, so your code stays in one place.'
          : 'Your account is in this contest from another session as well. Organisers can see both sessions.'}
      </p>
      <button type="button" class="btn btn-quiet btn-compact" onclick={() => (sessionNoticeHidden = true)}>Dismiss</button>
    </div>
  {/if}
  {#if phase === 'during'}
    {#if statementNotice}
      {@const n = statementNotice}
      <div class="banner banner-info statement-notice">
        <p>
          {n.label === current ? statementUpdatedText(n.at) : `Problem ${n.label}: s${statementUpdatedText(n.at).slice(1)}`}
          <button type="button" class="link-button" onclick={() => (diffLabel = n.label)}>See what changed</button>
        </p>
        <button type="button" class="btn btn-quiet btn-compact" onclick={() => dismiss(statementBannerKey(n.label, n.at))}>Dismiss</button>
      </div>
    {/if}
    <ContestBanner items={banner} ondismiss={dismiss} onopen={openMessages} />
  {/if}
{/snippet}

{#snippet railSnippet(layout: string)}
  <ProblemRail
    {problems}
    {slug}
    current={current}
    {layout}
    {exam}
    unread={banner.length}
    during={phase === 'during'}
    onmessages={openMessages}
    onrules={() => (rulesOpen = true)}
  />
{/snippet}

{#if loadState !== 'ready' || !contest}
  <PageHeader title={loadState === 'missing' ? 'Contest not found' : 'Contest'} back={{ href: '/', label: 'Your contests' }} />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no contest at this address." parent={{ href: '/', label: 'Go to your contests.' }} />
  {:else if loadState === 'forbidden'}
    <p class="lead">{loadError}</p>
  {:else if loadState === 'error'}
    <PageState kind="error" message={loadError} onretry={() => load()} />
  {/if}
{:else if showWorkspace && currentProblem}
  {#key `${current}:${phase}`}
    <Workspace
      base="{api}/problems/{encodeURIComponent(current)}"
      backHref={phase === 'during' ? '/' : `/contests/${encodeURIComponent(slug)}`}
      backLabel={phase === 'during' ? 'Your contests' : contest.title}
      label={current}
      context={contest.title}
      feedback={contest.rules.feedback_mode}
      showScores={phase === 'after'}
      locked={phase === 'after' && !practice ? 'The contest has ended. Submissions are closed.' : ''}
      problemScore={exam && phase === 'during' ? null : { score: currentProblem.my_score, max: currentProblem.max_score }}
      {used}
      minGapS={phase === 'during' ? contest.rules.min_gap_s : 0}
      {statementKey}
      extraEvents={CONTEST_EVENTS}
      onevent={onEvent}
      clock={phase === 'during' ? clockSnippet : undefined}
      note={phase === 'during'
        ? ''
        : timeUp
          ? 'Time is up. Your submissions are saved.'
          : practice
            ? 'Practice is open. Submissions you make now do not count towards the standings.'
            : 'The contest has ended. You can read the problem and your submissions.'}
      banner={bannerSnippet}
      rail={railSnippet}
    />
  {/key}
  <MessagesDialog
    open={messagesOpen}
    {contest}
    {current}
    canAsk={phase === 'during'}
    onclose={() => (messagesOpen = false)}
    onasked={onAsked}
  />
  <RulesDialog open={rulesOpen} {contest} onclose={() => (rulesOpen = false)} />
  <StatementDiff open={diffLabel !== null} {slug} label={diffLabel ?? current} onclose={() => (diffLabel = null)} />
{:else if phase === 'upcoming' || phase === 'ready'}
  <div class="page page-wide">
    <PageHeader title={contest.title} back={{ href: '/', label: 'Your contests' }}>
      <p class="meta">
        <span><MonoDigits text={formatDateTime(contest.starts_at)} />, {lengthText(contest)}, {contest.problem_count}
          {contest.problem_count === 1 ? 'problem' : 'problems'}.</span>
        <span class="state-word" data-state={studentState(contest.state)}>{studentStateWord(contest.state)}</span>
      </p>
    </PageHeader>

    <div class="page-split before">
      <div class="page-split-main">
        <!-- S3: the countdown is the page's display figure, then what the student agrees to, then the button. -->
        {#if phase === 'upcoming' || startNow.kind === 'before'}
          <p class="countdown"><span class="count-lead">Starts in</span> <span class="mono figure" role="timer">{startNow.ms >= 86_400_000 ? formatSpan(startNow.ms) : formatCountdown(startNow.ms)}</span></p>
        {:else if startNow.kind === 'window'}
          <p class="countdown">
            <span class="count-lead">Start any time before <MonoDigits text={formatDateTime(contest.ends_at)} />. The contest closes in</span>
            <span class="mono figure" role="timer">{formatCountdown(startNow.ms)}</span>
          </p>
        {:else}
          <p class="countdown">
            <span class="count-lead">The contest started at <span class="mono">{formatDateTime(contest.starts_at).slice(-5)}</span> and your time is running. It ends in</span>
            <span class="mono figure" role="timer">{formatCountdown(startNow.ms)}</span>
          </p>
        {/if}

        <section class="panel honour-panel" aria-labelledby="honour-title">
          <h2 id="honour-title">Honour code</h2>
          <div class="honour">
            <Markdown source={contest.honour_text.trim() !== '' ? contest.honour_text : 'Work alone, and do not share code or ideas until the contest ends.'} />
          </div>
          <div class="start">
            <button type="button" class="btn btn-primary" disabled={phase === 'upcoming' || startBusy} aria-describedby="start-help" onclick={start}>
              I agree, start the contest
            </button>
            <p id="start-help" class="muted small">
              {#if phase === 'upcoming'}
                You can start at <MonoDigits text={formatDateTime(contest.starts_at)} />.
              {:else if contest.start_mode === 'window'}
                Your clock starts when you press it and runs {lengthText(contest).replace(' each', '')}.
              {:else}
                Pressing it records that you agreed, then opens the problems.
              {/if}
            </p>
            {#if startError}<p class="field-error" role="alert">{startError}</p>{/if}
          </div>
        </section>
        <StreamNotice mode={streamMode} onresume={() => stream?.resume()} />
      </div>

      <aside class="page-split-aside" aria-label="About this contest">
        {#if contest.description_md.trim() !== ''}
          <div class="description"><Markdown source={contest.description_md} /></div>
        {/if}

        <section aria-labelledby="rules-title">
          <details class="how" open={!workspaceSeen}>
            <summary><h2 id="rules-title">Rules</h2></summary>
            <ul class="rules">
              {#each rulesSentences(contest) as rule, i (i)}<li>{rule}</li>{/each}
            </ul>
          </details>
        </section>

        <section aria-labelledby="how-title">
          <details class="how" open={!workspaceSeen}>
            <summary><h2 id="how-title">How the workspace works</h2></summary>
            <div class="how-body">
              <p>
                Each problem opens with its statement beside the editor. <strong>Run on samples</strong> shows your output next to the
                expected one. <strong>Submit</strong> sends your code to the judge, and the result appears under the editor.
              </p>
              {#if exam}
                <p>This contest is an exam: after Submit you see only "Submitted". Your scores appear when the contest ends.</p>
              {:else}
                <p>After Submit, a result looks like the example below. Each bar is one subtask: it fills when every test in it passes, and its points count only then.</p>
                <figure class="example" aria-labelledby="example-caption">
                  <figcaption id="example-caption" class="muted small">An example result, not one of yours</figcaption>
                  <div class="example-panel"><VerdictPanel submission={EXAMPLE_SUBMISSION} onjump={null} idPrefix="example" /></div>
                </figure>
              {/if}
            </div>
          </details>
        </section>
      </aside>
    </div>
  </div>
{:else}
  {@const endedAt = contest.my_window && hasAccepted(contest) ? contest.my_window.ends_at : contest.ends_at}
  {@const place = standings && !(standings.variant === 'frozen') ? placeFigure(standings) : null}
  <div class="page page-wide">
    <PageHeader
      title={contest.title}
      back={{ href: '/', label: 'Your contests' }}
      state={contest.state !== 'ended' ? { word: studentStateWord(contest.state), kind: studentState(contest.state) } : null}
    />

    {#if timeUp}<p class="time-up" role="status">Time is up. Your submissions are saved.</p>{/if}

    {#if label !== '' && !currentProblem}
      <p class="lead">This contest has no problem {label}.</p>
    {/if}

    <div class="page-split after">
      <div class="page-split-main">
        <!-- S9: one lead, the place as the display figure, then the problems. -->
        <section class="result" aria-labelledby="result-title">
          <h2 id="result-title">Your result</h2>
          {#if problems.length === 0}
            <p class="result-lead">Ended <MonoDigits text={formatDayTime(endedAt)} />. You did not take part in this contest.</p>
          {:else}
            {#if place}<PlaceFigure {place} />{/if}
            <p class="result-lead">
              Ended <MonoDigits text={formatDayTime(endedAt)} />. You scored <MonoDigits parts={scoreParts(totalScore, totalMax)} />.
            </p>
            {#if standings && standings.variant === 'frozen'}<p>{placeSentence(standings, true)}</p>{/if}
            {#if practice}<p class="muted">Practice is open: new submissions do not count.</p>{/if}
            {#if practiseNext}
              <p>
                To practise next: problem <span class="mono">{practiseNext.label}</span>, {practiseNext.title}, where you scored <MonoDigits parts={scoreParts(practiseNext.score ?? 0, practiseNext.max_score)} />.
                {#if practiseNext.practice_open}<a href={problemHref(slug, practiseNext.label)}>Practise problem {practiseNext.label}</a>{/if}
              </p>
            {/if}
            <div class="action-row">
              <a class="btn btn-secondary" href="/contests/{encodeURIComponent(slug)}/standings">See the standings</a>
              {#if progressEntry}<a class="btn btn-quiet" href="/progress">See your progress</a>{/if}
            </div>
            <div class="table-wrap results">
              <table class="table">
                <caption class="visually-hidden">Your score per problem</caption>
                <thead>
                  <tr>
                    <th scope="col">Problem</th>
                    <th scope="col" class="right">Score</th>
                    <th scope="col"><span class="visually-hidden">Open</span></th>
                  </tr>
                </thead>
                <tbody>
                  {#each problems as p (p.label)}
                    {@const zeroed = zeroCase(p)}
                    <tr>
                      <th scope="row">
                        <span class="mono letter">{p.label}</span>{p.title}
                        {#if zeroed !== null}
                          <span class="zeroed">Zero on this problem by decision. <a href={zeroed > 0 ? `/integrity/cases/${zeroed}` : '/integrity'}>See your case</a></span>
                        {/if}
                      </th>
                      <td class="right score"><MonoDigits parts={scoreParts(p.my_score ?? 0, p.max_score)} /></td>
                      <td class="open">
                        <a class="btn btn-link btn-compact" href="/contests/{encodeURIComponent(slug)}/problems/{encodeURIComponent(p.label)}" aria-label="{practice ? 'Practise' : 'Review'} problem {p.label}">
                          {practice ? 'Practise' : 'Review'}
                        </a>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </section>

        {#if problems.length > 0}
          <section class="section" aria-labelledby="subs-title">
            <h2 id="subs-title">Your submissions</h2>
            <div>
              {#each problems as p (p.label)}
                {@const h = history[p.label]}
                <details class="subs" open={latestLabel === p.label} ontoggle={(e) => onToggle(e, p.label)}>
                  <summary>Problem {p.label}: {p.title}</summary>
                  {#if h === 'loading' || h === undefined}
                    <p class="muted small" role="status">Loading submissions</p>
                  {:else if h === 'error'}
                    <p class="field-error">Your submissions could not load. Close and open this again.</p>
                  {:else}
                    <SubmissionList submissions={h} onjump={null} exam={exam} showScores />
                  {/if}
                </details>
              {/each}
            </div>
          </section>
        {/if}

        {#if contest.editorial_md && contest.editorial_md.trim() !== ''}
          <section class="section" aria-labelledby="editorial-title">
            <h2 id="editorial-title">Editorial</h2>
            <Markdown source={contest.editorial_md} />
          </section>
        {/if}
      </div>

      {#if (contest.announcements ?? []).length > 0 || (contest.my_clarifications ?? []).length > 0 || (contest.public_clarifications ?? []).length > 0}
        <aside class="page-split-aside" aria-label="Messages">
          <MessageList {contest} level={2} idPrefix="page-msg" framed />
        </aside>
      {/if}
    </div>
  </div>
{/if}

<style>
  .page {
    display: grid;
    gap: var(--space-2);
    max-width: 48rem;
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 var(--space-2);
    color: var(--ink-muted);
  }


  /* The before-start page fills the width; its split holds the measure. */
  .page-wide {
    max-width: none;
  }

  .before {
    margin-top: var(--space-5);
  }

  /* S3: the countdown is the page's display figure, digits in mono, words in Grotesk. */
  .countdown {
    display: grid;
    gap: var(--space-1);
  }

  .count-lead {
    color: var(--ink-muted);
    font-size: var(--fs-2);
  }

  .countdown .figure {
    font-size: var(--fs-5);
    line-height: var(--lh-tight);
  }

  .description {
    max-width: var(--measure-prose);
  }

  .section {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-5);
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .rules {
    display: grid;
    gap: var(--space-2);
    padding-left: var(--space-5);
  }

  .how summary {
    width: fit-content;
    border-radius: var(--radius);
    cursor: pointer;
  }

  .how summary h2 {
    display: inline;
  }

  .how-body {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }

  .example {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .statement-notice {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
    padding-block: var(--space-2);
    font-size: var(--fs-0);
  }

  .link-button {
    padding: 0;
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: var(--act);
    font: inherit;
    font-weight: 500;
    text-decoration: underline;
    text-decoration-thickness: var(--rule);
    text-underline-offset: 0.2em;
    cursor: pointer;
  }

  .start {
    display: grid;
    justify-items: start;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding-top: var(--space-4);
    border-top: var(--rule) solid var(--line);
  }

  .small {
    font-size: var(--fs-0);
  }

  .time-up {
    margin-top: var(--space-3);
    font-weight: 600;
  }

  .session-notice {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
    padding-block: var(--space-2);
    border-color: var(--part);
    background: var(--part-tint);
    font-size: var(--fs-0);
  }

  .after {
    margin-top: var(--space-5);
  }

  .result {
    display: grid;
    gap: var(--space-3);
  }

  .zeroed {
    display: block;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .result-lead {
    font-size: var(--fs-2);
  }

  .results {
    margin-top: var(--space-2);
  }

  .letter {
    margin-right: var(--space-3);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .score {
    white-space: nowrap;
  }

  .open {
    width: 1%;
    white-space: nowrap;
  }

  .subs {
    border-bottom: var(--rule) solid var(--line);
  }

  .subs summary {
    min-height: var(--control-h-touch);
    padding-block: var(--space-2);
    border-radius: var(--radius);
    font-weight: 500;
    line-height: var(--control-h);
    cursor: pointer;
  }

  .subs[open] {
    padding-bottom: var(--space-3);
  }
</style>
