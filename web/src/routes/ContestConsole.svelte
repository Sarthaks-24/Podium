<!--
  ContestConsole: the organiser's live view of one contest at
  /admin/contests/{id}/console (cache/v1/m4-plan.md section 4).

  - Status: the state, the time to the end on the server's clock, and
    judging running or paused with Pause judging (asks first; submissions
    keep queuing) and Resume judging.
  - Time: extend the contest for everyone (asks first, then reminds to
    announce it), and give one student extra minutes with a reason.
  - Submissions: the feed from GET .../submissions with filters for
    problem, student and verdict, pages, a refresh every 10 s while the
    page is open, and Rejudge for one submission or a whole problem.
  - Announcements: post one, starting from a template.
  - Questions: the clarifications inbox, unanswered first, each answered
    from a free reply or a canned one, privately or for everyone; an
    answered question can be made public or private later. Questions come
    in over the event stream (and the 10 s refresh); the status strip and
    the tab title count the unanswered ones, and a polite live region names
    each new one.
  - Audit log: every organiser action that can change results (settings,
    problems, roster, extensions, extend, pause and resume, rejudges, the
    reveal, the editorial, co-organisers), newest first, from
    GET .../audit, refreshed with the feed and after each action here.

  From 1024 px the feed sits beside the questions and announcements, the
  questions first. Under 768 px each feed row reads as two lines.
  Students see a short explanation instead. Props: `id`.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import Markdown from '../components/Markdown.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import {
    ANNOUNCEMENT_TEMPLATES,
    CANNED_REPLIES,
    askerName,
    auditSummary,
    fillTemplate,
    formatCountdown,
    type AdminContest,
    type Announcement,
    type AuditEntry,
    type Clarification,
    type FeedPage,
    type FeedRow,
  } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { countOf } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { verdictLabel } from '../lib/problems';
  import { session } from '../lib/session.svelte';
  import type { Standings } from '../lib/standings';
  import { EventStream } from '../lib/stream';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { formatWhen, languageLabel } from '../lib/workspace';

  let { id }: { id: string } = $props();

  const VERDICTS = ['AC', 'WA', 'TLE', 'MLE', 'RE', 'CE'];

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<AdminContest | null>(null);

  // Feed.
  let feed = $state<FeedRow[]>([]);
  let feedState = $state<'loading' | 'ready' | 'error'>('loading');
  let feedError = $state('');
  let hasMore = $state(false);
  let page = $state(1);
  let fProblem = $state('');
  let fUser = $state('');
  let fVerdict = $state('');
  let autoRefresh = $state(true);

  // Controls.
  let confirm = $state<null | { title: string; body: string; label: string; danger: boolean; run: () => Promise<void> }>(null);
  let confirmBusy = $state(false);
  let extendMinutes = $state('10');
  let extendError = $state('');
  // Questions seen so far, so only new ones are announced.
  let seenQuestions: Set<number> | null = null;
  let questionNews = $state('');
  let extUser = $state('');
  let extMinutes = $state('15');
  let extReason = $state('');
  let extError = $state('');
  /** Which extension field the error is about, so only that one is marked and focused. */
  let extField = $state<'user' | 'minutes' | ''>('');
  let rejudgeLabel = $state('');
  let students = $state<{ user_id: number; name: string }[]>([]);

  // Announcements.
  let announcements = $state<Announcement[]>([]);
  let annBody = $state('');
  let annTemplate = $state('');
  let annError = $state('');
  let annBusy = $state(false);

  // Audit log.
  let audit = $state<AuditEntry[]>([]);
  let auditState = $state<'loading' | 'ready' | 'error'>('loading');
  let auditAll = $state(false);
  const AUDIT_SHOWN = 10;

  // Clarifications.
  let clarifications = $state<Clarification[]>([]);
  let replies = $state<Record<number, { text: string; public: boolean; error: string; busy: boolean }>>({});

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const labels = $derived((contest?.problems ?? []).map((p) => p.label));
  const closeAt = $derived(contest ? (contest.close_at ?? contest.ends_at) : 0);
  const left = $derived(contest ? closeAt - ticker.now : 0);
  const inbox = $derived(
    [...clarifications].sort((a, b) => Number(Boolean(a.answer)) - Number(Boolean(b.answer)) || b.created_at - a.created_at),
  );
  const unanswered = $derived(clarifications.filter((c) => !c.answer).length);

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
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  async function loadContest(quiet = false) {
    if (!quiet) loadState = 'loading';
    try {
      const sent = Date.now();
      const c = await getJSON<AdminContest>(base);
      if (typeof c.server_now === 'number') serverClock.observe(c.server_now, sent, Date.now());
      refreshTicker();
      const restore = keepHeadingFocus();
      contest = c;
      setTitle(c.title, routeTitle('contest-console'));
      loadState = 'ready';
      void restore();
      // Every action here reloads the contest, so the log follows it.
      void loadAudit();
    } catch (e) {
      if (handleAuth(e) || quiet) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Contest not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
    }
  }

  async function loadFeed() {
    const params: [string, string][] = [];
    if (fProblem) params.push(['problem', fProblem]);
    if (fUser !== '') params.push(['user', fUser]);
    if (fVerdict) params.push(['verdict', fVerdict]);
    params.push(['page', String(page)]);
    const q = params.map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&');
    try {
      const data = await getJSON<FeedPage>(`${base}/submissions?${q}`);
      feed = data.submissions ?? [];
      hasMore = data.has_more ?? false;
      feedState = 'ready';
    } catch (e) {
      if (handleAuth(e)) return;
      feedError = messageOf(e);
      feedState = 'error';
    }
  }

  async function loadSide() {
    try {
      const [a, c] = await Promise.all([
        getJSON<{ announcements: Announcement[] }>(`${base}/announcements`),
        getJSON<{ clarifications: Clarification[] }>(`${base}/clarifications`),
      ]);
      announcements = [...(a.announcements ?? [])].sort((x, y) => y.created_at - x.created_at);
      clarifications = c.clarifications ?? [];
      const fresh = clarifications.filter((q) => !q.answer && seenQuestions !== null && !seenQuestions.has(q.id));
      if (fresh.length === 1) {
        const q = fresh[0];
        questionNews = `New question from ${askerName(q)}${q.problem_label ? ` about problem ${q.problem_label}` : ''}.`;
      } else if (fresh.length > 1) {
        questionNews = `${fresh.length} new questions.`;
      }
      seenQuestions = new Set(clarifications.map((q) => q.id));
      // Each question gets its reply draft here, never while rendering.
      const next: typeof replies = {};
      for (const q of clarifications) next[q.id] = replies[q.id] ?? { text: q.answer ?? '', public: q.public, error: '', busy: false };
      replies = next;
    } catch (e) {
      handleAuth(e);
    }
  }

  async function loadAudit() {
    try {
      const data = await getJSON<{ entries: AuditEntry[] }>(`${base}/audit`);
      audit = data.entries ?? [];
      auditState = 'ready';
    } catch (e) {
      if (!handleAuth(e)) auditState = 'error';
    }
  }

  async function loadStudents() {
    try {
      const s = await getJSON<Standings>(`${base}/standings`);
      students = s.rows.map((r) => ({ user_id: r.user_id, name: r.name })).sort((a, b) => a.name.localeCompare(b.name));
    } catch {
      students = [];
    }
  }

  function applyFilters(event: SubmitEvent) {
    event.preventDefault();
    page = 1;
    void loadFeed();
  }

  function ask(title: string, body: string, label: string, danger: boolean, run: () => Promise<void>) {
    confirm = { title, body, label, danger, run };
  }

  async function runConfirm() {
    if (!confirm) return;
    confirmBusy = true;
    try {
      await confirm.run();
      confirm = null;
    } catch (e) {
      confirm = null;
      if (!handleAuth(e)) showToast(messageOf(e), 'error');
    } finally {
      confirmBusy = false;
    }
  }

  function pause() {
    ask('Pause judging?', 'Submissions keep queuing and nothing is lost. Nothing is judged until you resume.', 'Pause judging', true, async () => {
      await sendJSON<void>('POST', `${base}/pause`);
      showToast('Paused judging');
      await loadContest(true);
    });
  }

  async function resume() {
    try {
      await sendJSON<void>('POST', `${base}/resume`);
      showToast('Resumed judging');
      await loadContest(true);
    } catch (e) {
      if (!handleAuth(e)) showToast(messageOf(e), 'error');
    }
  }

  function extendAll(event: SubmitEvent) {
    event.preventDefault();
    extendError = '';
    const minutes = Math.round(Number(extendMinutes));
    if (!Number.isFinite(minutes) || minutes < 1) {
      extendError = 'Enter the minutes to add, 1 or more.';
      document.getElementById('extend-minutes')?.focus();
      return;
    }
    ask(`Extend the contest by ${countOf(minutes, 'minute')}?`, 'Everyone gets the extra time, and their clocks update at once.', 'Extend contest', false, async () => {
      await sendJSON<void>('POST', `${base}/extend`, { minutes });
      showToast(`Extended by ${countOf(minutes, 'minute')}. Post an announcement so students know.`);
      annTemplate = 'extended';
      annBody = fillTemplate(ANNOUNCEMENT_TEMPLATES.find((t) => t.id === 'extended')!.text, { minutes });
      await loadContest(true);
    });
  }

  async function extendOne(event: SubmitEvent) {
    event.preventDefault();
    extError = '';
    extField = '';
    const minutes = Math.round(Number(extMinutes));
    if (extUser === '') {
      extError = 'Choose a student.';
      extField = 'user';
      document.getElementById('ext-user')?.focus();
      return;
    }
    if (String(extMinutes).trim() === '' || !Number.isFinite(minutes) || minutes < 0) {
      extError = 'Enter the minutes, 0 to remove an extension.';
      extField = 'minutes';
      document.getElementById('ext-minutes')?.focus();
      return;
    }
    try {
      await sendJSON<void>('PUT', `${base}/extensions/${encodeURIComponent(extUser)}`, { minutes, reason: extReason.trim() });
      const who = students.find((s) => String(s.user_id) === extUser)?.name ?? 'the student';
      showToast(minutes === 0 ? `Removed the extension for ${who}` : `Gave ${who} ${countOf(minutes, 'more minute', 'more minutes')}`);
      extReason = '';
      await loadContest(true);
    } catch (e) {
      if (handleAuth(e)) return;
      extError = e instanceof ApiError ? (e.fields.minutes ?? e.message) : messageOf(e);
      extField = e instanceof ApiError && e.fields.minutes ? 'minutes' : 'user';
      document.getElementById(extField === 'minutes' ? 'ext-minutes' : 'ext-user')?.focus();
    }
  }

  async function rejudgeOne(row: FeedRow) {
    try {
      await sendJSON<void>('POST', `${base}/rejudge`, { submission_id: row.id });
      showToast(`Rejudging ${row.name}'s submission to ${row.problem_label ?? row.label ?? ''}`);
      await Promise.all([loadFeed(), loadAudit()]);
    } catch (e) {
      if (!handleAuth(e)) showToast(messageOf(e), 'error');
    }
  }

  function rejudgeProblem() {
    if (rejudgeLabel === '') return;
    const l = rejudgeLabel;
    ask(`Rejudge problem ${l}?`, 'Every submission to it is judged again. Scores in the standings may change.', 'Rejudge problem', true, async () => {
      await sendJSON<void>('POST', `${base}/rejudge`, { problem_label: l });
      showToast(`Rejudging problem ${l}`);
      await Promise.all([loadFeed(), loadAudit()]);
    });
  }

  function useTemplate() {
    const t = ANNOUNCEMENT_TEMPLATES.find((x) => x.id === annTemplate);
    if (t) annBody = fillTemplate(t.text, { label: labels[0] ?? 'A', minutes: Math.round(Number(extendMinutes)) || 10 });
  }

  async function postAnnouncement(event: SubmitEvent) {
    event.preventDefault();
    annError = '';
    if (annBody.trim() === '') {
      annError = 'Write the announcement first.';
      document.getElementById('ann-body')?.focus();
      return;
    }
    annBusy = true;
    try {
      await sendJSON<unknown>('POST', `${base}/announcements`, { body_md: annBody.trim() });
      showToast('Posted the announcement');
      annBody = '';
      annTemplate = '';
      await loadSide();
    } catch (e) {
      if (handleAuth(e)) return;
      annError = e instanceof ApiError ? (e.fields.body_md ?? e.message) : messageOf(e);
      document.getElementById('ann-body')?.focus();
    } finally {
      annBusy = false;
    }
  }

  const EMPTY_REPLY = { text: '', public: false, error: '', busy: false };

  function reply(c: Clarification) {
    return replies[c.id] ?? EMPTY_REPLY;
  }

  async function answer(c: Clarification, overridePublic?: boolean) {
    const r = reply(c);
    r.error = '';
    const text = r.text.trim() || (c.answer ?? '');
    if (text === '') {
      r.error = 'Write an answer or pick a canned reply.';
      document.getElementById(`reply-${c.id}`)?.focus();
      return;
    }
    r.busy = true;
    const pub = overridePublic ?? r.public;
    try {
      await sendJSON<unknown>('PUT', `${base}/clarifications/${c.id}`, { answer: text, public: pub });
      showToast(c.answer ? (pub ? 'Made the answer public' : 'Made the answer private') : pub ? 'Sent the answer to everyone' : 'Sent the answer');
      replies[c.id] = { text: '', public: pub, error: '', busy: false };
      await loadSide();
    } catch (e) {
      if (handleAuth(e)) return;
      r.error = e instanceof ApiError ? (e.fields.answer ?? e.message) : messageOf(e);
      document.getElementById(`reply-${c.id}`)?.focus();
    } finally {
      r.busy = false;
    }
  }

  function stateText(row: FeedRow): string {
    if (row.state === 'submitted') return 'Submitted';
    if (row.state === 'queued') return 'Queued';
    if (row.state === 'running') return 'Judging';
    if (row.state === 'error') return 'Judge error';
    return verdictLabel(row.verdict);
  }

  let timer: ReturnType<typeof setInterval> | undefined;

  onMount(() => {
    setTitle(routeTitle('contest-console'));
    if (!isOrganiser) {
      loadState = 'forbidden';
      return;
    }
    void loadContest().then(() => {
      if (loadState !== 'ready') return;
      void loadFeed();
      void loadSide();
      void loadStudents();
    });
    timer = setInterval(() => {
      if (!autoRefresh || loadState !== 'ready' || document.visibilityState === 'hidden') return;
      void loadFeed();
      void loadSide();
      void loadAudit();
    }, 10_000);
    // New questions arrive at once over the stream; the refresh above is the fallback.
    stream = new EventStream({
      url: '/api/events',
      events: ['clarification'],
      onEvent: () => {
        if (loadState === 'ready') void loadSide();
      },
      onReconnect: () => void loadSide(),
      poll: () => undefined,
    });
    stream.start();
    return useTicker();
  });

  let stream: EventStream | null = null;

  // The tab title carries the unanswered count, so a waiting question shows from another tab.
  $effect(() => {
    if (!contest) return;
    setTitle(unanswered > 0 ? `(${unanswered}) ${contest.title}` : contest.title, routeTitle('contest-console'));
  });

  onDestroy(() => {
    clearInterval(timer);
    stream?.stop();
  });
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Console" />
  <p class="lead">Only admins can run contests. Ask an owner if you need access.</p>
{:else if loadState !== 'ready' || !contest}
  <PageHeader
    title={loadState === 'missing' ? 'Contest not found' : 'Console'}
    crumbs={[{ label: 'Contest admin', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Contest not found' : 'Console' }]}
  />
  {#if loadState === 'loading'}
    <p class="lead muted" role="status">Loading the contest</p>
  {:else if loadState === 'missing'}
    <p class="lead">There is no contest with that number. <a href="/admin/contests">Go back to the list.</a></p>
  {:else}
    <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
    <p class="retry"><button type="button" class="btn btn-secondary" onclick={() => loadContest()}>Try again</button></p>
  {/if}
{:else}
  <ContestAdminNav {id} title={contest.title} state={contest.state} current="console" />

  <div class="status">
    <p>
      {#if left > 0 && ticker.now >= contest.starts_at}Ends in <span class="mono">{formatCountdown(left)}</span>{:else if ticker.now < contest.starts_at}Starts
        in <span class="mono">{formatCountdown(contest.starts_at - ticker.now)}</span>{:else}Ended{/if}
      {#if contest.extended_minutes > 0}<span class="muted">, extended by {contest.extended_minutes} min</span>{/if}
    </p>
    <p class="judging" class:paused={contest.paused}>
      <Icon name={contest.paused ? 'lock' : 'clock'} />
      {contest.paused ? 'Judging is paused. Submissions are queuing.' : 'Judging is running.'}
    </p>
    {#if contest.paused}
      <button type="button" class="btn btn-primary btn-compact" onclick={resume}>Resume judging</button>
    {:else}
      <button type="button" class="btn btn-secondary btn-compact" onclick={pause}>Pause judging</button>
    {/if}
    {#if unanswered > 0}
      <a class="questions-link" href="#clar-title">{unanswered} unanswered {unanswered === 1 ? 'question' : 'questions'}</a>
    {:else}
      <span class="muted">No unanswered questions</span>
    {/if}
  </div>
  <p class="visually-hidden" aria-live="polite">{questionNews}</p>

  <div class="layout">
    <div class="main-col">
      <section class="section" aria-labelledby="time-title">
        <h2 id="time-title">Time</h2>
        <form class="row" novalidate onsubmit={extendAll}>
          <div class="field small-field">
            <label for="extend-minutes">Minutes for everyone</label>
            <input
              id="extend-minutes"
              class="input mono"
              type="number"
              min="1"
              inputmode="numeric"
              bind:value={extendMinutes}
              aria-invalid={extendError ? 'true' : undefined}
              aria-describedby={extendError ? 'extend-error' : undefined}
            />
          </div>
          <button type="submit" class="btn btn-secondary">Extend contest</button>
        </form>
        {#if extendError}<p id="extend-error" class="field-error">{extendError}</p>{/if}
        <form class="row" novalidate onsubmit={extendOne}>
          <div class="field grow">
            <label for="ext-user">Student</label>
            <select id="ext-user" class="select" bind:value={extUser} aria-invalid={extError && extField === 'user' ? 'true' : undefined} aria-describedby={extError && extField === 'user' ? 'ext-error' : undefined}>
              <option value="">Choose a student</option>
              {#each students as s (s.user_id)}<option value={String(s.user_id)}>{s.name}</option>{/each}
            </select>
          </div>
          <div class="field small-field">
            <label for="ext-minutes">Extra minutes</label>
            <input id="ext-minutes" class="input mono" type="number" min="0" inputmode="numeric" bind:value={extMinutes} aria-invalid={extError && extField === 'minutes' ? 'true' : undefined} aria-describedby={extError && extField === 'minutes' ? 'ext-error' : undefined} />
          </div>
          <div class="field grow">
            <label for="ext-reason">Reason</label>
            <input id="ext-reason" class="input" type="text" autocomplete="off" placeholder="Laptop crashed" bind:value={extReason} />
          </div>
          <button type="submit" class="btn btn-secondary">Give extra time</button>
        </form>
        {#if extError}<p id="ext-error" class="field-error">{extError}</p>{/if}
        {#if (contest.extensions ?? []).length > 0}
          <ul class="extensions small">
            {#each contest.extensions ?? [] as x (x.user_id)}
              <li>{x.name || x.email}: <span class="mono">{x.minutes}</span> min{x.reason ? `, ${x.reason}` : ''}</li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="section" aria-labelledby="feed-title">
        <div class="section-head">
          <h2 id="feed-title">Submissions</h2>
          <label class="choice"><input type="checkbox" bind:checked={autoRefresh} /> Refresh every 10 s</label>
        </div>
        <form class="row filters" novalidate onsubmit={applyFilters}>
          <div class="field">
            <label for="f-problem">Problem</label>
            <select id="f-problem" class="select" bind:value={fProblem}>
              <option value="">All</option>
              {#each labels as l (l)}<option value={l}>{l}</option>{/each}
            </select>
          </div>
          <div class="field grow">
            <label for="f-user">Student</label>
            <select id="f-user" class="select" bind:value={fUser}>
              <option value="">Everyone</option>
              {#each students as s (s.user_id)}<option value={String(s.user_id)}>{s.name}</option>{/each}
            </select>
          </div>
          <div class="field">
            <label for="f-verdict">Verdict</label>
            <select id="f-verdict" class="select" bind:value={fVerdict}>
              <option value="">All</option>
              <option value="pending">Waiting or judging</option>
              <option value="error">Judge error</option>
              {#each VERDICTS as v (v)}<option value={v}>{verdictLabel(v)}</option>{/each}
            </select>
          </div>
          <button type="submit" class="btn btn-secondary">Filter</button>
          <button type="button" class="btn btn-quiet" onclick={loadFeed}><Icon name="refresh-cw" /> Refresh</button>
        </form>

        {#if feedState === 'loading'}
          <p class="muted" role="status">Loading submissions</p>
        {:else if feedState === 'error'}
          <div class="banner banner-error" role="alert"><p>{feedError}</p></div>
        {:else if feed.length === 0}
          <p class="muted">No submissions match. They appear here as students submit.</p>
        {:else}
          <div class="table-wrap feed">
            <table class="table">
              <thead>
                <tr>
                  <th scope="col">Time</th>
                  <th scope="col">Student</th>
                  <th scope="col">Problem</th>
                  <th scope="col">Language</th>
                  <th scope="col">Verdict</th>
                  <th scope="col" class="right">Score</th>
                  <th scope="col"><span class="visually-hidden">Rejudge</span></th>
                </tr>
              </thead>
              <tbody>
                {#each feed as row (row.id)}
                  <tr>
                    <td class="num when">{formatWhen(row.created_at)}</td>
                    <th scope="row" class="who">{row.name}{#if row.practice}<span class="muted"> (practice)</span>{/if}</th>
                    <td class="mono plabel">{row.problem_label ?? row.label}</td>
                    <td class="lang">{languageLabel(row.language)}</td>
                    <td class="verdict v-{row.state === 'done' ? row.verdict : row.state}">{stateText(row)}</td>
                    <td class="num right fscore">{row.state === 'done' ? `${row.score} of ${row.max_score}` : ''}</td>
                    <td class="act">
                      <button
                        type="button"
                        class="btn btn-quiet btn-compact"
                        aria-label="Rejudge submission by {row.name} to {row.problem_label ?? row.label} at {formatWhen(row.created_at)}"
                        onclick={() => rejudgeOne(row)}>Rejudge</button
                      >
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <div class="pager">
            <button type="button" class="btn btn-secondary btn-compact" disabled={page <= 1} onclick={() => { page -= 1; void loadFeed(); }}>Newer</button>
            <span class="muted small">Page {page}</span>
            <button type="button" class="btn btn-secondary btn-compact" disabled={!hasMore} onclick={() => { page += 1; void loadFeed(); }}>Older</button>
          </div>
        {/if}

        <div class="row">
          <div class="field">
            <label for="rejudge-label">Rejudge every submission to</label>
            <select id="rejudge-label" class="select" bind:value={rejudgeLabel}>
              <option value="">Choose a problem</option>
              {#each labels as l (l)}<option value={l}>Problem {l}</option>{/each}
            </select>
          </div>
          <button type="button" class="btn btn-secondary" disabled={rejudgeLabel === ''} onclick={rejudgeProblem}>Rejudge problem</button>
        </div>
      </section>
    </div>

    <div class="side-col">
      <section class="section" aria-labelledby="clar-title">
        <h2 id="clar-title">Questions{#if unanswered > 0}<span class="count mono">{unanswered}</span><span class="visually-hidden"> unanswered</span>{/if}</h2>
        {#if inbox.length === 0}
          <p class="muted small">No questions yet.</p>
        {:else}
          <ul class="items">
            {#each inbox as c (c.id)}
              {@const r = reply(c)}
              <li class="clar" class:open={!c.answer}>
                <p class="meta small">
                  <span class="mono">{formatWhen(c.created_at)}</span>
                  {askerName(c)}, {c.problem_label ? `problem ${c.problem_label}` : 'general'}{c.public ? ', public' : ''}
                </p>
                <p class="q">{c.question}</p>
                {#if c.answer}
                  <p class="a">{c.answer}</p>
                  <button type="button" class="btn btn-quiet btn-compact" disabled={r.busy} onclick={() => answer(c, !c.public)}>
                    {c.public ? 'Make private' : 'Make public'}
                  </button>
                {:else}
                  <div class="canned" role="group" aria-label="Canned replies">
                    {#each CANNED_REPLIES as text, i (i)}
                      <button type="button" class="btn btn-secondary btn-compact" onclick={() => (r.text = text)}>{text}</button>
                    {/each}
                  </div>
                  <div class="field">
                    <label for="reply-{c.id}">Answer</label>
                    <textarea id="reply-{c.id}" class="input" rows="2" bind:value={r.text} aria-invalid={r.error ? 'true' : undefined} aria-describedby={r.error ? `reply-${c.id}-error` : undefined}></textarea>
                    {#if r.error}<p id="reply-{c.id}-error" class="field-error">{r.error}</p>{/if}
                  </div>
                  <label class="choice"><input type="checkbox" bind:checked={r.public} /> Answer for everyone</label>
                  <button type="button" class="btn btn-primary btn-compact" disabled={r.busy} onclick={() => answer(c)}>Send answer</button>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="section" aria-labelledby="ann-title">
        <h2 id="ann-title">Announcements</h2>
        <form class="ann" novalidate onsubmit={postAnnouncement}>
          <div class="field">
            <label for="ann-template">Start from a template</label>
            <select id="ann-template" class="select" bind:value={annTemplate} onchange={useTemplate}>
              <option value="">No template</option>
              {#each ANNOUNCEMENT_TEMPLATES as t (t.id)}<option value={t.id}>{t.label}</option>{/each}
            </select>
          </div>
          <div class="field">
            <label for="ann-body">Announcement (Markdown)</label>
            <textarea id="ann-body" class="input" rows="3" bind:value={annBody} aria-invalid={annError ? 'true' : undefined} aria-describedby="ann-help{annError ? ' ann-error' : ''}"></textarea>
            <p id="ann-help" class="field-help">Students see it at once as a banner over their workspace.</p>
            {#if annError}<p id="ann-error" class="field-error">{annError}</p>{/if}
          </div>
          <button type="submit" class="btn btn-primary" disabled={annBusy}>Post announcement</button>
        </form>
        {#if announcements.length > 0}
          <ul class="items">
            {#each announcements as a (a.id)}
              <li><p class="mono muted small">{formatWhen(a.created_at)}</p><Markdown source={a.body_md} compact /></li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="section" aria-labelledby="audit-title">
        <h2 id="audit-title">Audit log</h2>
        <p class="muted small">Every change that can affect results, with who made it. Nothing here can be edited or deleted.</p>
        {#if auditState === 'loading'}
          <p class="muted small" role="status">Loading the audit log</p>
        {:else if auditState === 'error'}
          <p class="muted small">The audit log could not be loaded. It refreshes with the feed.</p>
        {:else if audit.length === 0}
          <p class="muted small">Nothing recorded yet.</p>
        {:else}
          <ul class="items audit">
            {#each auditAll ? audit : audit.slice(0, AUDIT_SHOWN) as e (e.id)}
              <li>
                <p class="meta small"><span class="mono">{formatWhen(e.created_at)}</span> {e.actor.email}</p>
                <p class="entry">{auditSummary(e)}</p>
              </li>
            {/each}
          </ul>
          {#if audit.length > AUDIT_SHOWN}
            <button type="button" class="btn btn-quiet btn-compact" aria-expanded={auditAll} onclick={() => (auditAll = !auditAll)}>
              {auditAll ? 'Show the latest 10' : `Show all ${audit.length}`}
            </button>
          {/if}
        {/if}
      </section>
    </div>
  </div>

  <ConfirmDialog
    open={confirm !== null}
    title={confirm?.title ?? ''}
    body={confirm?.body ?? ''}
    confirmLabel={confirm?.label ?? ''}
    danger={confirm?.danger ?? false}
    busy={confirmBusy}
    onconfirm={runConfirm}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .retry {
    margin-top: var(--space-4);
  }

  .status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-5);
    margin-top: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .judging {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--ink-muted);
  }

  .judging.paused {
    color: var(--part);
    font-weight: 600;
  }

  .layout {
    display: grid;
    gap: 0 var(--space-6);
  }

  .main-col,
  .side-col {
    display: grid;
    align-content: start;
    min-width: 0;
  }

  .section {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-6);
    min-width: 0;
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .section-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
  }

  .grow {
    flex: 1 1 12rem;
  }

  .small-field {
    width: 9rem;
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-size: var(--fs-0);
    cursor: pointer;
  }

  .choice input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .feed .table {
    font-size: var(--fs-0);
  }

  .who {
    font-weight: 500;
    white-space: nowrap;
  }

  .lang {
    white-space: nowrap;
  }

  .verdict {
    white-space: nowrap;
  }

  .v-AC {
    color: var(--ok);
  }

  .v-WA,
  .v-TLE,
  .v-MLE,
  .v-RE,
  .v-CE,
  .v-error {
    color: var(--bad);
  }

  .right,
  .table .right {
    text-align: right;
  }

  .extensions {
    display: grid;
    gap: var(--space-1);
    padding-left: var(--space-5);
    color: var(--ink-muted);
  }

  .pager {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .small {
    font-size: var(--fs-0);
  }

  .ann {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
  }

  .ann .field {
    width: 100%;
  }

  .items {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .items > li {
    display: grid;
    gap: var(--space-2);
    justify-items: start;
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
    font-size: var(--fs-0);
  }

  .items .field {
    width: 100%;
  }

  .clar.open {
    padding-inline: var(--space-3);
    background: var(--surface);
  }

  .meta {
    color: var(--ink-muted);
  }

  .q {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .a {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .audit .entry {
    overflow-wrap: anywhere;
  }

  .canned {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .canned .btn {
    max-width: 100%;
    white-space: normal;
    text-align: left;
  }

  .count {
    margin-left: var(--space-2);
    padding: 0 var(--space-2);
    border-radius: var(--radius);
    background: var(--act);
    color: var(--act-ink);
    font-size: var(--fs-1);
    vertical-align: middle;
  }

  .questions-link {
    font-weight: 600;
  }

  /* Phones: each feed row is time, student and problem, then verdict, score and Rejudge. */
  @media (max-width: 767px) {
    .feed thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
    }

    .feed tbody tr {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'when who plabel'
        'verdict fscore act';
      align-items: center;
      gap: var(--space-1) var(--space-3);
      padding: var(--space-2) var(--space-3);
      border-bottom: var(--rule) solid var(--line);
    }

    .feed tbody :is(th, td) {
      height: auto;
      padding: 0;
      border: 0;
    }

    .feed .when {
      grid-area: when;
    }

    .feed .who {
      grid-area: who;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .feed .plabel {
      grid-area: plabel;
      text-align: right;
    }

    .feed .lang {
      display: none;
    }

    .feed .verdict {
      grid-area: verdict;
    }

    .feed .fscore {
      grid-area: fscore;
      text-align: left;
    }

    .feed .act {
      grid-area: act;
      justify-self: end;
    }
  }

  @media (min-width: 1024px) {
    .layout {
      grid-template-columns: minmax(0, 1.7fr) minmax(20rem, 1fr);
    }
  }
</style>
