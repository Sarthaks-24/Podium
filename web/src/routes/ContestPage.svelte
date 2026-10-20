<!--
  ContestPage: one contest for a student, at /contests/{slug} and
  /contests/{slug}/problems/{label} (cache/v1/m4-plan.md section 5,
  design.md 5.3). The page follows the student's own phase on the server's
  clock (lib/contests.ts studentPhase):

  - Before the start, and before they agree: the description, the rules as
    sentences, the honour code (what is recorded and for how long, D47) and
    "I agree, start the contest" (POST /api/contests/{slug}/start), with a
    countdown. The button waits for the start in a fixed-start contest.
  - During their window: the shared Workspace on the contest's routes with
    the rail of problem letters (score and 2 px bar), the contest clock and
    freeze line, the Standings link and Messages. Exam contests show only
    "Submitted". Announcements and answered questions arriving over the
    event stream show in a quiet banner above the title (from 1024 px the
    editor never moves); a second open tab or a second session reported
    by the server shows a notice there too. Exam mode marks submitted
    problems "Submitted" on the rail. "[" and "]" open the previous and
    next problem when focus is not in a text field (lib/shortcuts.ts).
  - After it: the result, each problem's score with the student's own
    submissions, the editorial once published, and practice when the
    organisers left it open (submissions then do not count towards the standings). A
    board still frozen after the end gives the place only, never a
    frozen total beside the live score; when the clock ran out on this
    page, "Time is up" stays on screen.

  Props: `slug`, and `label` (the problem letter from the address, or "").
-->
<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import ContestBanner from '../components/ContestBanner.svelte';
  import ContestClock from '../components/ContestClock.svelte';
  import Markdown from '../components/Markdown.svelte';
  import MessagesDialog from '../components/MessagesDialog.svelte';
  import ProblemRail from '../components/ProblemRail.svelte';
  import StreamNotice from '../components/StreamNotice.svelte';
  import SubmissionList from '../components/SubmissionList.svelte';
  import Workspace from '../components/Workspace.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import {
    CONTEST_EVENTS,
    bannerItems,
    formatCountdown,
    formatSpan,
    freezeAt,
    hasAccepted,
    isForContest,
    lengthText,
    rulesSentences,
    stateWord,
    studentPhase,
    type Clarification,
    type Contest,
  } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { navigate } from '../lib/router.svelte';
  import { adjacentLabel, isPlainKey, isTypingTarget } from '../lib/shortcuts';
  import { session } from '../lib/session.svelte';
  import { shell } from '../lib/shell.svelte';
  import { placeSentence, type Standings } from '../lib/standings';
  import { EventStream, type StreamMode } from '../lib/stream';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';
  import type { Submission, Workspace as WorkspaceData } from '../lib/workspace';

  let { slug, label = '' }: { slug: string; label?: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<Contest | null>(null);

  let startBusy = $state(false);
  let startError = $state('');
  let messagesOpen = $state(false);
  let dismissed = $state<Set<string>>(new Set());
  let since = $state(0);
  let otherTab = $state(false);
  let sessionNoticeHidden = $state(false);
  let streamMode = $state<StreamMode>('connecting');
  let timeUp = $state(false);

  // After the end: the student's place and their submissions per problem.
  let standings = $state.raw<Standings | null>(null);
  let history = $state<Record<string, Submission[] | 'loading' | 'error'>>({});

  const api = $derived(`/api/contests/${encodeURIComponent(slug)}`);
  const phase = $derived(contest ? studentPhase(contest, ticker.now) : null);
  const problems = $derived(contest?.problems ?? []);
  const current = $derived(label !== '' ? label : (problems[0]?.label ?? ''));
  const currentProblem = $derived(problems.find((p) => p.label === current) ?? null);
  const exam = $derived(contest?.rules.feedback_mode === 'exam');
  const practice = $derived(contest?.state === 'practice');
  const banner = $derived(contest ? bannerItems(contest, dismissed, since) : []);
  const showWorkspace = $derived(
    contest !== null && currentProblem !== null && (phase === 'during' || (phase === 'after' && label !== '')),
  );
  const totalScore = $derived(problems.reduce((n, p) => n + (p.my_score ?? 0), 0));
  const totalMax = $derived(problems.reduce((n, p) => n + p.max_score, 0));
  const secondSession = $derived(!sessionNoticeHidden && (otherTab || contest?.second_session === true));

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
    const next = new Set([...dismissed, key]);
    dismissed = next;
    try {
      sessionStorage.setItem(dismissKey(), JSON.stringify([...next]));
    } catch {
      // Dismissed for this page only.
    }
  }

  function openMessages() {
    // Opening the messages counts as reading what the banner holds.
    for (const item of banner) dismiss(item.key);
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
      if (since === 0) since = data.server_now;
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
    if (lastPhase === 'during' && p === 'after') timeUp = true;
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

  // A second tab with this contest open on this device.
  let channel: BroadcastChannel | null = null;

  onMount(() => {
    dismissed = readDismissed();
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
    <ContestBanner items={banner} ondismiss={dismiss} onopen={openMessages} />
  {/if}
{/snippet}

{#snippet railSnippet(layout: string)}
  <ProblemRail {problems} {slug} current={current} {layout} {exam} unread={banner.length} onmessages={openMessages} />
{/snippet}

{#if loadState !== 'ready' || !contest}
  <a class="back" href="/">Your contests</a>
  <h1 tabindex="-1">{loadState === 'missing' ? 'Contest not found' : 'Contest'}</h1>
  {#if loadState === 'loading'}
    <p class="lead muted" role="status">Loading the contest</p>
  {:else if loadState === 'missing'}
    <p class="lead">There is no contest at this address. <a href="/">Go to your contests.</a></p>
  {:else if loadState === 'forbidden'}
    <p class="lead">{loadError}</p>
  {:else if loadState === 'error'}
    <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
    <p class="retry"><button type="button" class="btn btn-secondary" onclick={() => load()}>Try again</button></p>
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
{:else if phase === 'upcoming' || phase === 'ready'}
  <div class="page">
    <a class="back" href="/">Your contests</a>
    <h1 tabindex="-1">{contest.title}</h1>
    <p class="meta">
      {formatDateTime(contest.starts_at)}, {lengthText(contest)}, {contest.problem_count}
      {contest.problem_count === 1 ? 'problem' : 'problems'}. <span class="state">{stateWord(contest.state)}</span>
    </p>

    {#if phase === 'upcoming'}
      <p class="countdown">Starts in <span class="mono big" role="timer">{contest.starts_at - ticker.now >= 86_400_000 ? formatSpan(contest.starts_at - ticker.now) : formatCountdown(contest.starts_at - ticker.now)}</span></p>
    {:else if contest.start_mode === 'window'}
      <p class="countdown">
        Start any time before <span class="mono">{formatDateTime(contest.ends_at)}</span>. The contest closes in
        <span class="mono" role="timer">{formatCountdown(contest.ends_at - ticker.now)}</span>.
      </p>
    {:else}
      <p class="countdown">Started. Ends in <span class="mono big" role="timer">{formatCountdown(contest.ends_at - ticker.now)}</span></p>
    {/if}

    {#if contest.description_md.trim() !== ''}
      <div class="section"><Markdown source={contest.description_md} /></div>
    {/if}

    <section class="section" aria-labelledby="rules-title">
      <h2 id="rules-title">Rules</h2>
      <ul class="rules">
        {#each rulesSentences(contest) as line, i (i)}<li>{line}</li>{/each}
      </ul>
    </section>

    <section class="section" aria-labelledby="honour-title">
      <h2 id="honour-title">Honour code</h2>
      <div class="honour">
        <Markdown source={contest.honour_text.trim() !== '' ? contest.honour_text : 'Work alone, and do not share code or ideas until the contest ends.'} />
      </div>
    </section>

    <div class="start">
      <button type="button" class="btn btn-primary" disabled={phase === 'upcoming' || startBusy} aria-describedby="start-help" onclick={start}>
        I agree, start the contest
      </button>
      <p id="start-help" class="muted small">
        {#if phase === 'upcoming'}
          You can start at {formatDateTime(contest.starts_at)}.
        {:else if contest.start_mode === 'window'}
          Your clock starts when you press it and runs {lengthText(contest).replace(' each', '')}.
        {:else}
          Pressing it records that you agreed, then opens the problems.
        {/if}
      </p>
      {#if startError}<p class="field-error" role="alert">{startError}</p>{/if}
    </div>
    <StreamNotice mode={streamMode} onresume={() => stream?.resume()} />
  </div>
{:else}
  <div class="page">
    <a class="back" href="/">Your contests</a>
    <h1 tabindex="-1">{contest.title}</h1>
    <p class="meta">
      {#if contest.my_window && hasAccepted(contest)}Your time ended at {formatDateTime(contest.my_window.ends_at)}.{:else}Ended
        {formatDateTime(contest.ends_at)}.{/if}
      {#if contest.state !== 'ended'}<span class="state">{stateWord(contest.state)}</span>{/if}
    </p>

    {#if timeUp}<p class="time-up" role="status">Time is up. Your submissions are saved.</p>{/if}

    {#if label !== '' && !currentProblem}
      <p class="lead">This contest has no problem {label}.</p>
    {/if}

    <section class="section" aria-labelledby="result-title">
      <h2 id="result-title">Your result</h2>
      {#if problems.length === 0}
        <p class="muted">You did not take part in this contest.</p>
      {:else}
        <p class="total"><span class="mono">{totalScore}</span> <span class="muted">of {totalMax} points</span></p>
        {#if standings}<p>{placeSentence(standings, contest.state === 'ended' || contest.state === 'practice')}</p>{/if}
        <p><a href="/contests/{encodeURIComponent(slug)}/standings">See the standings</a></p>
        {#if practice}
          <p class="muted small">Practice is open. Submissions you make now do not count towards the standings.</p>
        {/if}
        <div class="table-wrap results">
          <table class="table">
            <thead>
              <tr>
                <th scope="col">Problem</th>
                <th scope="col" class="right">Score</th>
                <th scope="col"><span class="visually-hidden">Open</span></th>
              </tr>
            </thead>
            <tbody>
              {#each problems as p (p.label)}
                <tr>
                  <th scope="row"><span class="mono letter">{p.label}</span>{p.title}</th>
                  <td class="num right">{p.my_score ?? 0} of {p.max_score}</td>
                  <td class="open">
                    <a href="/contests/{encodeURIComponent(slug)}/problems/{encodeURIComponent(p.label)}" aria-label="{practice ? 'Practise' : 'Review'} problem {p.label}">
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
        {#each problems as p (p.label)}
          {@const h = history[p.label]}
          <details class="subs" ontoggle={(e) => onToggle(e, p.label)}>
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
      </section>
    {/if}

    {#if contest.editorial_md && contest.editorial_md.trim() !== ''}
      <section class="section" aria-labelledby="editorial-title">
        <h2 id="editorial-title">Editorial</h2>
        <Markdown source={contest.editorial_md} />
      </section>
    {/if}

    {#if (contest.announcements ?? []).length > 0}
      <section class="section" aria-labelledby="ann-title">
        <h2 id="ann-title">Announcements</h2>
        <ul class="announcements">
          {#each [...contest.announcements].sort((a, b) => b.created_at - a.created_at) as a (a.id)}
            <li><p class="mono muted small">{formatDateTime(a.created_at)}</p><Markdown source={a.body_md} compact /></li>
          {/each}
        </ul>
      </section>
    {/if}
  </div>
{/if}

<style>
  .page {
    display: grid;
    gap: var(--space-2);
    max-width: 48rem;
  }

  .back {
    width: fit-content;
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .retry {
    margin-top: var(--space-4);
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 var(--space-2);
    color: var(--ink-muted);
  }

  .state {
    color: var(--ink);
    font-weight: 600;
  }

  .countdown {
    margin-top: var(--space-3);
  }

  .big {
    font-size: var(--fs-3);
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

  .honour {
    padding: var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .start {
    display: grid;
    justify-items: start;
    gap: var(--space-2);
    margin-top: var(--space-5);
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

  .total {
    font-size: var(--fs-2);
  }

  .total .mono {
    font-size: var(--fs-5);
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

  .announcements {
    display: grid;
    gap: var(--space-3);
    padding: 0;
    list-style: none;
  }
</style>
