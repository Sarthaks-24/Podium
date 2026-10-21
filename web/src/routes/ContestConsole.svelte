<!--
  ContestConsole: the organiser's live view of one contest at
  /admin/contests/{id}/console (cache/v1/m4-plan.md section 4; laid out
  again in v2 P3 F6.1 and F6.2 and audit 2026-10-20 O3 and O5).

  - Attention strip: one ruled line of linked items (lib/contests
    attentionItems): time to the end, when the board freezes, judging on
    or paused with Pause or Resume, the queue and its oldest wait, judge
    errors, whether a judge is asking for work, unanswered questions,
    problems validated on another toolchain, and when the page last
    refreshed. An item is coloured only when it needs action. A failed
    refresh keeps the last rows and says so.
  - Beside the work from 1280 px, and first below that: Questions, each
    answered from a free reply or a standard answer picked from one list,
    privately or for everyone ("Send to everyone"); then Announcements,
    with a preview and a confirmation before anything is posted.
  - Submissions: the feed with filters for problem, student and verdict,
    pages, and a refresh every 10 s. Outcomes read as students read them
    ("Partial, 40 of 100" apart from a zero); the time is minutes from the
    start, with the clock time on hover; "Judge again" is a quiet button
    per row, and judging a whole problem again says what it does. Each
    student's name opens their progress page (web-quality #16). For
    reviewers each row also has "Notes" (private notes on the submission,
    components/SubmissionNotes, with "Open a case" through OpenCaseDialog;
    v2 P4 F9.3) and a link to the case opened from it.
  - Time: extend the contest for everyone (the confirmation names the new
    end), extra minutes for one student with a reason, and "End the
    contest now", which says its effect before it sets the end to now.
  - Problem versions, folded unless an update is available (C4, F10.1),
    and Changes, every action that can change results (extra time says
    whether a reason was written, v1.4.0; a review decision that changed
    the board is listed too).

  After Pause, Resume, Extend and Judge again the announcement box is
  filled from a template; it is never posted by itself. Students see a
  short explanation instead. Props: `id`.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onDestroy, onMount } from 'svelte';
  import Badge from '../components/Badge.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import Markdown from '../components/Markdown.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import OpenCaseDialog from '../components/OpenCaseDialog.svelte';
  import SubmissionNotes from '../components/SubmissionNotes.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import {
    ANNOUNCEMENT_TEMPLATES,
    CANNED_REPLIES,
    askerName,
    attentionItems,
    AUDIT_ACTIONS,
    auditSummary,
    feedOutcome,
    fillTemplate,
    sinceStart,
    type AdminContest,
    type Announcement,
    type AuditEntry,
    type Clarification,
    type ContestSettings,
    type FeedPage,
    type FeedRow,
  } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { feedQuery, pinChangeText, pinPath, updateEffect, updateLabel, type PinUpdate } from '../lib/history';
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
  /** When the shown rows arrived, and when a later refresh failed (C2). */
  let feedAt = $state(0);
  let feedFailedAt = $state(0);

  // Controls.
  let confirm = $state<null | { title: string; body: string; items?: string[]; label: string; danger: boolean; run: () => Promise<void> }>(null);
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

  // Changes (the contest audit log).
  let audit = $state<AuditEntry[]>([]);
  let auditState = $state<'loading' | 'ready' | 'error'>('loading');
  let auditAll = $state(false);
  let auditPage = $state(1);
  let auditMore = $state(false);
  let auditAction = $state('');
  let auditActor = $state('');
  const AUDIT_SHOWN = 10;

  // Problem versions.
  let pinNote = $state('');

  // Private notes and manual cases (F9.3).
  let notesFor = $state<{ id: number; name: string; label: string } | null>(null);
  let caseFor = $state<{ id: number; name: string; label: string } | null>(null);

  /** Keeps the feed row in step with the notes dialog. */
  function notesChanged(count: number, caseId: number | null) {
    const target = notesFor;
    if (!target) return;
    feed = feed.map((r) => (r.id === target.id ? { ...r, note_count: count, case_id: caseId } : r));
  }

  // Clarifications.
  let clarifications = $state<Clarification[]>([]);
  let replies = $state<Record<number, { text: string; public: boolean; error: string; busy: boolean }>>({});

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const labels = $derived((contest?.problems ?? []).map((p) => p.label));
  const closeAt = $derived(contest ? (contest.close_at ?? contest.ends_at) : 0);
  const inbox = $derived(
    [...clarifications].sort((a, b) => Number(Boolean(a.answer)) - Number(Boolean(b.answer)) || b.created_at - a.created_at),
  );
  const unanswered = $derived(clarifications.filter((c) => !c.answer).length);
  const attention = $derived(contest ? attentionItems(contest, ticker.now, unanswered) : []);
  const running = $derived(contest !== null && ticker.now >= contest.starts_at && ticker.now < closeAt);
  const updateWaiting = $derived((contest?.problems ?? []).some((p) => p.update_available || p.toolchain_drift));
  /** Problem versions stay folded until an update or a toolchain change needs them; they never fold by themselves. */
  let versionsOpen = $state(false);
  $effect(() => {
    if (updateWaiting) versionsOpen = true;
  });
  /** Names by user id, for change rows that name a student by id only. */
  const names = $derived(new Map([...students.map((s) => [s.user_id, s.name] as const), ...(contest?.extensions ?? []).map((x) => [x.user_id, x.name || x.email] as const)]));

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

  /** "14:02:10" in local time. */
  function clock(ms: number): string {
    const d = new Date(ms);
    return [d.getHours(), d.getMinutes(), d.getSeconds()].map((n) => String(n).padStart(2, '0')).join(':');
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
      setTitle(routeTitle('contest-console'), c.title);
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
      loadError = errorText(e);
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
      feedAt = serverClock.serverNow();
      feedFailedAt = 0;
    } catch (e) {
      if (handleAuth(e)) return;
      // A failed refresh keeps the rows already shown (C2).
      if (feedState === 'ready') {
        feedFailedAt = serverClock.serverNow();
        return;
      }
      feedError = errorText(e);
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

  /** Loads a page of changes; page 1 replaces the list, later pages add older rows. */
  async function loadAudit(next = 1) {
    try {
      const data = await getJSON<{ entries: AuditEntry[]; has_more?: boolean }>(
        `${base}/audit${feedQuery({ page: next, action: auditAction, actor: auditActor.trim() })}`,
      );
      audit = next === 1 ? (data.entries ?? []) : [...audit, ...(data.entries ?? [])];
      auditPage = next;
      auditMore = data.has_more === true;
      auditState = 'ready';
    } catch (e) {
      if (!handleAuth(e)) auditState = 'error';
    }
  }

  function filterAudit(event?: SubmitEvent) {
    event?.preventDefault();
    auditAll = false;
    void loadAudit(1);
  }

  /** The pinned revision of a problem, as the confirmation to judge again names it. */
  function pinnedRev(label: string): number | undefined {
    return contest?.problems.find((p) => p.label === label)?.rev;
  }

  function updatePin(label: string) {
    const p = contest?.problems.find((x) => x.label === label);
    if (!p || p.latest_revision_id === undefined || p.latest_rev === undefined) return;
    const target = { id: p.latest_revision_id, rev: p.latest_rev };
    const effect = updateEffect({ label, rev: p.rev, latest_rev: p.latest_rev, update_statement_only: p.update_statement_only });
    ask(`Update problem ${label} to revision ${target.rev}?`, effect, updateLabel(target.rev), false, async () => {
      const out = await sendJSON<PinUpdate<AdminContest>>('PUT', pinPath(id, label), { revision_id: target.id });
      if (out.contest && typeof out.contest === 'object') contest = out.contest;
      showToast(`Problem ${label} now uses revision ${target.rev}`);
      // The server words an announcement; posting it stays the organiser's choice.
      annTemplate = '';
      annBody = out.announcement_md ?? '';
      pinNote = out.rejudge_needed
        ? `The tests of problem ${label} changed. Read the announcement in Announcements, post it, then judge problem ${label} again.`
        : `Read the announcement in Announcements and post it, so students know to look at problem ${label}.`;
      if (out.rejudge_needed) rejudgeLabel = label;
      await loadAudit();
    });
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

  /** Shows only the submissions that ended in a judge error (from the attention strip). */
  function showJudgeErrors(event: MouseEvent) {
    event.preventDefault();
    fVerdict = 'error';
    page = 1;
    void loadFeed();
    document.getElementById('submissions-title')?.scrollIntoView({ block: 'start' });
  }

  function ask(title: string, body: string, label: string, danger: boolean, run: () => Promise<void>, items?: string[]) {
    confirm = { title, body, label, danger, run, items };
  }

  async function runConfirm() {
    if (!confirm) return;
    confirmBusy = true;
    try {
      await confirm.run();
      confirm = null;
    } catch (e) {
      confirm = null;
      if (!handleAuth(e)) showToast(errorText(e), 'error');
    } finally {
      confirmBusy = false;
    }
  }

  /** Puts a template in the announcement box; posting it stays the organiser's choice (C3). */
  function suggest(templateId: string, values: { label?: string; minutes?: number } = {}) {
    const t = ANNOUNCEMENT_TEMPLATES.find((x) => x.id === templateId);
    if (!t) return;
    annTemplate = templateId;
    annBody = fillTemplate(t.text, values);
  }

  function pause() {
    ask(
      'Pause judging?',
      'Nothing is judged until you resume. Students can keep submitting: their submissions wait in the queue and nothing is lost.',
      'Pause judging',
      true,
      async () => {
        await sendJSON<void>('POST', `${base}/pause`);
        showToast('Paused judging. An announcement is ready to post.');
        suggest('paused');
        await loadContest(true);
      },
    );
  }

  async function resume() {
    try {
      await sendJSON<void>('POST', `${base}/resume`);
      showToast('Resumed judging. An announcement is ready to post.');
      suggest('resumed');
      await loadContest(true);
    } catch (e) {
      if (!handleAuth(e)) showToast(errorText(e), 'error');
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
    ask(
      `Extend the contest by ${countOf(minutes, 'minute')}?`,
      `Everyone's contest then ends at ${formatWhen(closeAt + minutes * 60_000, ticker.now)}, and their clocks update at once.`,
      'Extend contest',
      false,
      async () => {
        await sendJSON<void>('POST', `${base}/extend`, { minutes });
        showToast(`Extended by ${countOf(minutes, 'minute')}. Post an announcement so students know.`);
        suggest('extended', { minutes });
        await loadContest(true);
      },
    );
  }

  /** The settings as the server holds them, for a change of one field. */
  function settingsOf(c: AdminContest): ContestSettings {
    return {
      slug: c.slug,
      title: c.title,
      description_md: c.description_md ?? '',
      starts_at: c.starts_at,
      ends_at: c.ends_at,
      start_mode: c.start_mode,
      duration_minutes: c.duration_minutes,
      freeze_minutes: c.freeze_minutes,
      scoring_mode: c.scoring_mode,
      access: c.access,
      feedback_mode: c.feedback_mode,
      graded: c.graded,
      published: c.published,
      practice_after_end: c.practice_after_end,
      stop_on_sample_fail: c.stop_on_sample_fail,
      min_gap_s: c.min_gap_s,
      max_per_problem: c.max_per_problem,
      languages: [...(c.languages ?? [])],
      honour_text: c.honour_text ?? '',
      hold_reveal: c.hold_reveal ?? false,
    };
  }

  /** Ends a running contest now (audit O5): the end moves to the server's now, minus any time added for everyone. */
  function endNow() {
    const c = contest;
    if (!c) return;
    const personal = (c.extensions ?? []).filter((x) => x.minutes > 0).length;
    const items = [
      'Queued submissions are still judged.',
      c.practice_after_end ? 'Students see Ended, and practice opens.' : 'Students see Ended.',
    ];
    if (personal > 0) items.push(`${countOf(personal, 'student')} with extra time keep${personal === 1 ? 's' : ''} it.`);
    ask(
      'End the contest now?',
      "Everyone's time ends now: the problems close for new contest submissions at once.",
      'End the contest',
      true,
      async () => {
        const now = Math.ceil(serverClock.serverNow() / 1000) * 1000;
        const ends = now - (c.extended_minutes ?? 0) * 60_000;
        if (ends <= c.starts_at) throw new ApiError(400, 'bad_request', 'The time added for everyone is longer than the time run so far, so the end cannot move before the start. Remove the extension first.');
        const updated = await sendJSON<AdminContest>('PUT', base, { ...settingsOf(c), ends_at: ends });
        if (updated && typeof updated === 'object' && 'id' in updated) contest = updated;
        showToast('Ended the contest');
        await loadContest(true);
      },
      items,
    );
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
      extError = e instanceof ApiError ? (e.fields.minutes ?? e.message) : errorText(e);
      extField = e instanceof ApiError && e.fields.minutes ? 'minutes' : 'user';
      document.getElementById(extField === 'minutes' ? 'ext-minutes' : 'ext-user')?.focus();
    }
  }

  async function rejudgeOne(row: FeedRow) {
    try {
      await sendJSON<void>('POST', `${base}/rejudge`, { submission_id: row.id });
      showToast(`Judging ${row.name}'s submission to ${row.problem_label ?? row.label ?? ''} again`);
      await Promise.all([loadFeed(), loadAudit()]);
    } catch (e) {
      if (!handleAuth(e)) showToast(errorText(e), 'error');
    }
  }

  function rejudgeProblem() {
    if (rejudgeLabel === '') return;
    const l = rejudgeLabel;
    const rev = pinnedRev(l);
    const against = rev !== undefined ? ` against revision ${rev}, the one students are served` : '';
    ask(
      `Judge problem ${l} again?`,
      `Every submission to problem ${l} is judged again${against}. New submissions still go first, so the queue keeps moving. Scores in the standings may change.`,
      'Judge again',
      true,
      async () => {
        const out = await sendJSON<{ queued?: number } | null>('POST', `${base}/rejudge`, { problem_label: l });
        const n = out && typeof out.queued === 'number' ? out.queued : null;
        showToast(n !== null ? `Judging ${countOf(n, 'submission')} to problem ${l} again` : `Judging problem ${l} again`);
        suggest('rejudge', { label: l });
        await Promise.all([loadFeed(), loadAudit()]);
      },
    );
  }

  function useTemplate() {
    const t = ANNOUNCEMENT_TEMPLATES.find((x) => x.id === annTemplate);
    if (t) annBody = fillTemplate(t.text, { label: labels[0] ?? 'A', minutes: Math.round(Number(extendMinutes)) || 10 });
  }

  /** Posting reaches every student at once, so it asks first (C3). */
  function postAnnouncement(event: SubmitEvent) {
    event.preventDefault();
    annError = '';
    if (annBody.trim() === '') {
      annError = 'Write the announcement first.';
      document.getElementById('ann-body')?.focus();
      return;
    }
    ask('Post to every student?', 'It shows at once as a banner over every workspace in this contest. It cannot be taken back.', 'Post announcement', false, sendAnnouncement);
  }

  async function sendAnnouncement() {
    annBusy = true;
    try {
      await sendJSON<unknown>('POST', `${base}/announcements`, { body_md: annBody.trim() });
      showToast('Posted the announcement');
      annBody = '';
      annTemplate = '';
      await loadSide();
    } catch (e) {
      if (handleAuth(e)) return;
      annError = e instanceof ApiError ? (e.fields.body_md ?? e.message) : errorText(e);
      document.getElementById('ann-body')?.focus();
    } finally {
      annBusy = false;
    }
  }

  const EMPTY_REPLY = { text: '', public: false, error: '', busy: false };

  function reply(c: Clarification) {
    return replies[c.id] ?? EMPTY_REPLY;
  }

  /** Puts a standard answer in the reply box from the one list (O3). */
  function insertStandard(c: Clarification, event: Event) {
    const select = event.currentTarget as HTMLSelectElement;
    const r = reply(c);
    if (select.value === '') return;
    r.text = CANNED_REPLIES[Number(select.value)] ?? r.text;
    select.value = '';
    document.getElementById(`reply-${c.id}`)?.focus();
  }

  async function answer(c: Clarification, overridePublic?: boolean) {
    const r = reply(c);
    r.error = '';
    const text = r.text.trim() || (c.answer ?? '');
    if (text === '') {
      r.error = 'Write an answer or insert a standard one.';
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
      r.error = e instanceof ApiError ? (e.fields.answer ?? e.message) : errorText(e);
      document.getElementById(`reply-${c.id}`)?.focus();
    } finally {
      r.busy = false;
    }
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
      // The contest carries the queue health (C10); loading it also refreshes the changes.
      void loadContest(true);
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
    setTitle(unanswered > 0 ? `(${unanswered}) ${routeTitle('contest-console')}` : routeTitle('contest-console'), contest.title);
  });

  onDestroy(() => {
    clearInterval(timer);
    stream?.stop();
  });
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Console" />
  <PageState kind="forbidden" message="Only admins can run contests." />
{:else if loadState !== 'ready' || !contest}
  <PageHeader
    title={loadState === 'missing' ? 'Contest not found' : 'Console'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Contest not found' : 'Console' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={() => loadContest()} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} published={contest.published} current="console" {unanswered} />

  <div class="strip" role="region" aria-label="Needs attention">
    <ul class="strip-items">
      {#each attention as item (item.key)}
        <li class="item tone-{item.tone}">
          {#if item.key === 'errors'}
            <a href={item.href} onclick={showJudgeErrors}><MonoDigits text={item.text} /></a>
          {:else}
            <a href={item.href}><MonoDigits text={item.text} /></a>
          {/if}
          {#if item.key === 'judging'}
            {#if contest.paused}
              <button type="button" class="btn btn-primary btn-compact" onclick={resume}>Resume judging</button>
            {:else}
              <button type="button" class="btn btn-quiet btn-compact" onclick={pause}>Pause judging</button>
            {/if}
          {/if}
        </li>
      {/each}
      {#if feedAt > 0}
        <li class="item" class:tone-part={feedFailedAt > 0}>
          {#if feedFailedAt > 0}
            <span role="status"><MonoDigits text={`Could not refresh at ${clock(feedFailedAt)}. Showing ${clock(feedAt)}. Retrying.`} /></span>
          {:else}
            <span class="muted"><MonoDigits text={`Updated ${clock(feedAt)}`} /></span>
          {/if}
        </li>
      {/if}
    </ul>
  </div>
  <p class="visually-hidden" aria-live="polite">{questionNews}</p>

  <div class="page-split console-split">
    <aside class="page-split-aside" aria-label="Questions and announcements">
      <section class="panel" aria-labelledby="clar-title">
        <div class="panel-head">
          <h2 id="clar-title">Questions</h2>
          {#if unanswered > 0}<Badge tone="bad">{countOf(unanswered, 'unanswered', 'unanswered')}</Badge>{/if}
        </div>
        {#if inbox.length === 0}
          <p class="muted small">No questions yet. They appear here the moment a student asks.</p>
        {:else}
          <ul class="items">
            {#each inbox as c (c.id)}
              {@const r = reply(c)}
              <li class="clar" class:open={!c.answer}>
                <p class="meta small">
                  <MonoDigits text={formatWhen(c.created_at, ticker.now)} />
                  {askerName(c)}, {c.problem_label ? `problem ${c.problem_label}` : 'general'}{c.public ? ', public' : ''}
                </p>
                <p class="q">{c.question}</p>
                {#if c.answer}
                  <p class="a">{c.answer}</p>
                  <button type="button" class="btn btn-quiet btn-compact" disabled={r.busy} onclick={() => answer(c, !c.public)}>
                    {c.public ? 'Make private' : 'Make public'}
                  </button>
                {:else}
                  <div class="field">
                    <label for="std-{c.id}">Insert a standard answer</label>
                    <select id="std-{c.id}" class="select" onchange={(e) => insertStandard(c, e)}>
                      <option value="">Choose one</option>
                      {#each CANNED_REPLIES as text, i (i)}<option value={String(i)}>{text}</option>{/each}
                    </select>
                  </div>
                  <div class="field">
                    <label for="reply-{c.id}">Answer</label>
                    <textarea id="reply-{c.id}" class="input" rows="2" bind:value={r.text} aria-invalid={r.error ? 'true' : undefined} aria-describedby={r.error ? `reply-${c.id}-error` : undefined}></textarea>
                    {#if r.error}<p id="reply-{c.id}-error" class="field-error">{r.error}</p>{/if}
                  </div>
                  <label class="choice"><input type="checkbox" bind:checked={r.public} /> Answer for everyone</label>
                  <button type="button" class="btn btn-secondary btn-compact" disabled={r.busy} onclick={() => answer(c)}>
                    {r.public ? 'Send to everyone' : 'Send answer'}
                  </button>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="panel" aria-labelledby="ann-title">
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
            <p id="ann-help" class="field-help">Every student sees it at once as a banner over their workspace.</p>
            {#if annError}<p id="ann-error" class="field-error">{annError}</p>{/if}
          </div>
          {#if annBody.trim() !== ''}
            <div class="ann-preview" role="group" aria-label="Preview">
              <p class="small muted">Preview</p>
              <Markdown source={annBody} compact />
            </div>
          {/if}
          <button type="submit" class="btn btn-secondary" disabled={annBusy}>Post announcement</button>
        </form>
        {#if announcements.length > 0}
          <ul class="items">
            {#each announcements as a (a.id)}
              <li><p class="muted small"><MonoDigits text={formatWhen(a.created_at, ticker.now)} /></p><Markdown source={a.body_md} compact /></li>
            {/each}
          </ul>
        {/if}
      </section>
    </aside>

    <div class="page-split-main">
      <section class="panel" aria-labelledby="submissions-title">
        <div class="panel-head">
          <h2 id="submissions-title">Submissions</h2>
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
            <table class="table table-dense table-stack">
              <thead>
                <tr>
                  <th scope="col">Time</th>
                  <th scope="col">Student</th>
                  <th scope="col">Problem</th>
                  <th scope="col">Language</th>
                  <th scope="col">Outcome</th>
                  <th scope="col"><span class="visually-hidden">Judge again</span></th>
                </tr>
              </thead>
              <tbody>
                {#each feed as row (row.id)}
                  {@const out = feedOutcome(row)}
                  {@const since = sinceStart(row.created_at, contest.starts_at)}
                  <tr>
                    <td class="when" title={formatWhen(row.created_at, ticker.now)}><MonoDigits text={since ?? formatWhen(row.created_at, ticker.now)} /></td>
                    <th scope="row" class="who">
                      <a href="/admin/users/{row.user_id}/progress">{row.name}</a>{#if row.practice}<span class="muted"> (practice)</span>{/if}
                      {#if row.case_id}<a class="case-link" href="/admin/integrity/cases/{row.case_id}">Case {row.case_id}</a>{/if}
                    </th>
                    <td class="mono plabel">{row.problem_label ?? row.label}</td>
                    <td class="lang">{languageLabel(row.language)}</td>
                    <td class="verdict"><Badge tone={out.tone}><MonoDigits text={out.text} /></Badge></td>
                    <td class="act">
                      {#if row.note_count !== undefined}
                        <button
                          type="button"
                          class="btn btn-quiet btn-compact again"
                          aria-label="Notes on {row.name}'s submission to problem {row.problem_label ?? row.label}, {formatWhen(row.created_at, ticker.now)}{row.note_count > 0 ? `, ${countOf(row.note_count, 'note')}` : ''}"
                          onclick={() => (notesFor = { id: row.id, name: row.name, label: row.problem_label ?? row.label ?? '' })}
                          >{row.note_count > 0 ? countOf(row.note_count, 'note') : 'Notes'}</button
                        >
                      {/if}
                      <button
                        type="button"
                        class="btn btn-quiet btn-compact again"
                        aria-label="Judge again: {row.name}, problem {row.problem_label ?? row.label}, {formatWhen(row.created_at, ticker.now)}"
                        onclick={() => rejudgeOne(row)}>Judge again</button
                      >
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <div class="pager">
            <button type="button" class="btn btn-secondary btn-compact" disabled={page <= 1} onclick={() => { page -= 1; void loadFeed(); }}>Newer</button>
            <span class="muted small">Page <span class="mono">{page}</span></span>
            <button type="button" class="btn btn-secondary btn-compact" disabled={!hasMore} onclick={() => { page += 1; void loadFeed(); }}>Older</button>
          </div>
        {/if}

        <div class="row">
          <div class="field">
            <label for="rejudge-label">Judge every submission to a problem again</label>
            <select id="rejudge-label" class="select" bind:value={rejudgeLabel}>
              <option value="">Choose a problem</option>
              {#each labels as l (l)}<option value={l}>Problem {l}</option>{/each}
            </select>
          </div>
          <button type="button" class="btn btn-secondary" disabled={rejudgeLabel === ''} onclick={rejudgeProblem}>Judge problem again</button>
        </div>
        {#if rejudgeLabel !== '' && pinnedRev(rejudgeLabel) !== undefined}
          <p class="muted small">Judged against revision <span class="mono">{pinnedRev(rejudgeLabel)}</span>, the one students are served.</p>
        {/if}
      </section>

      <section class="panel" aria-labelledby="time-title">
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
        {#if contest.extended_minutes > 0}<p class="muted small">Extended for everyone by <span class="mono">{contest.extended_minutes}</span> min so far.</p>{/if}
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
        {#if running}
          <div class="end-now">
            <button type="button" class="btn btn-secondary btn-danger-secondary" onclick={endNow}>End the contest now</button>
            <p class="muted small">Stops the contest for everyone at once. To give more time instead, extend it above.</p>
          </div>
        {/if}
      </section>

      <section class="panel" aria-labelledby="versions-title">
        <details class="versions-box" bind:open={versionsOpen}>
          <summary>
            <h2 id="versions-title" class="summary-title">Problem versions <Icon name="chevron-down" /></h2>
            <span class="muted small">{updateWaiting ? 'An update is available.' : 'Every problem is on its pinned revision.'}</span>
          </summary>
          <p class="muted small">Students are served the revision each problem is pinned to. An edit in the problem bank reaches them only when you update the contest here.</p>
          <ul class="items versions">
            {#each contest.problems as p (p.label)}
              <li>
                <p>
                  <span class="mono plabel">{p.label}</span>
                  {p.title}{#if p.rev !== undefined}, revision <span class="mono">{p.rev}</span>{/if}{#if p.reconstructed}<span class="muted"> (rebuilt at the upgrade)</span>{/if}
                </p>
                {#if p.toolchain_drift}
                  <p class="drift small">Validated on another judge toolchain. Validate it again in the problem bank before you judge it again.</p>
                {/if}
                {#if p.update_available && p.latest_rev !== undefined}
                  <p class="muted small">
                    Revision <span class="mono">{p.latest_rev}</span> is validated{p.update_statement_only ? ' and changes only the statement' : ' and changes the tests or limits'}.
                  </p>
                  <button type="button" class="btn btn-secondary btn-compact" onclick={() => updatePin(p.label)}>{updateLabel(p.latest_rev)}</button>
                {:else if p.latest_rev !== undefined && p.rev !== undefined && p.latest_rev > p.rev}
                  <p class="muted small">Revision <span class="mono">{p.latest_rev}</span> is not validated yet. Validate it in the problem bank to offer it here.</p>
                {/if}
              </li>
            {/each}
          </ul>
          {#if pinNote}<p class="banner banner-info pin-note">{pinNote}</p>{/if}
          {#if (contest.pin_history ?? []).length > 0}
            <details class="pins">
              <summary>Pin history</summary>
              <ul class="items small">
                {#each contest.pin_history ?? [] as h, i (i)}<li>{pinChangeText(h)}</li>{/each}
              </ul>
            </details>
          {/if}
        </details>
      </section>

      <section class="panel" aria-labelledby="audit-title">
        <h2 id="audit-title">Changes</h2>
        <p class="muted small">Every change that can affect results, with who made it. Nothing here can be edited or deleted.</p>
        <form class="row audit-filters" novalidate onsubmit={filterAudit}>
          <div class="field">
            <label for="audit-action">Kind of change</label>
            <select id="audit-action" class="select" bind:value={auditAction} onchange={() => filterAudit()}>
              <option value="">All changes</option>
              {#each AUDIT_ACTIONS as a (a.id)}<option value={a.id}>{a.label}</option>{/each}
            </select>
          </div>
          <div class="field grow">
            <label for="audit-actor">Made by</label>
            <input id="audit-actor" class="input" type="search" autocomplete="off" spellcheck="false" placeholder="ta@college.edu" bind:value={auditActor} />
          </div>
          <button type="submit" class="btn btn-secondary">Search</button>
        </form>
        {#if auditState === 'loading'}
          <p class="muted small" role="status">Loading the changes</p>
        {:else if auditState === 'error'}
          <p class="muted small">The changes could not be loaded. They refresh with the feed.</p>
        {:else if audit.length === 0}
          <p class="muted small">{auditAction !== '' || auditActor.trim() !== '' ? 'No change matches. Clear the filter to see everything.' : 'Nothing recorded yet.'}</p>
        {:else}
          <ul class="items audit">
            {#each auditAll ? audit : audit.slice(0, AUDIT_SHOWN) as e (e.id)}
              <li>
                <p class="meta small"><MonoDigits text={formatWhen(e.created_at, ticker.now)} /> {e.actor?.email ?? 'The server'}</p>
                <p class="entry">{auditSummary(e, names)}</p>
              </li>
            {/each}
          </ul>
          {#if audit.length > AUDIT_SHOWN}
            <button type="button" class="btn btn-quiet btn-compact" aria-expanded={auditAll} onclick={() => (auditAll = !auditAll)}>
              {auditAll ? 'Show the latest 10' : `Show all ${audit.length}`}
            </button>
          {/if}
          {#if auditMore && (auditAll || audit.length <= AUDIT_SHOWN)}
            <button type="button" class="btn btn-quiet btn-compact" onclick={() => loadAudit(auditPage + 1)}>Show older changes</button>
          {/if}
        {/if}
      </section>
    </div>
  </div>

  <SubmissionNotes
    open={notesFor !== null}
    submission={notesFor}
    onclose={() => (notesFor = null)}
    onchange={notesChanged}
    onopencase={() => {
      caseFor = notesFor;
      notesFor = null;
    }}
  />
  <OpenCaseDialog
    open={caseFor !== null}
    contestId={id}
    signals={[]}
    users={undefined}
    problems={undefined}
    submission={caseFor}
    onclose={() => (caseFor = null)}
    oncreated={(c) => {
      const target = caseFor;
      caseFor = null;
      if (target) feed = feed.map((r) => (r.id === target.id ? { ...r, case_id: c.id } : r));
      showToast('Opened the case. Share it with the student when you are ready.');
    }}
  />

  <ConfirmDialog
    open={confirm !== null}
    title={confirm?.title ?? ''}
    body={confirm?.body ?? ''}
    items={confirm?.items ?? []}
    confirmLabel={confirm?.label ?? ''}
    danger={confirm?.danger ?? false}
    busy={confirmBusy}
    onconfirm={runConfirm}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  /* The attention strip: one ruled line of linked items; colour only where action is needed. */
  .strip {
    margin-top: var(--space-4);
    padding-block: var(--space-2);
    border-block: var(--rule) solid var(--line);
  }

  .strip-items {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-5);
    padding: 0;
    list-style: none;
  }

  .item {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
  }

  .item a {
    color: var(--ink);
    text-decoration: none;
  }

  .item a:hover {
    text-decoration: underline;
  }

  .item.tone-part a,
  .item.tone-part {
    color: var(--part);
    font-weight: 600;
  }

  .item.tone-bad a {
    color: var(--bad);
    font-weight: 600;
  }

  .console-split {
    margin-top: var(--space-5);
  }

  /* The work column is wider here than a form: the feed needs the room. */
  @media (min-width: 1280px) {
    .console-split {
      grid-template-columns: minmax(0, 1fr) minmax(0, var(--measure-aside));
    }
  }

  .panel h2 {
    font-size: var(--fs-section);
  }

  .versions li {
    display: grid;
    gap: var(--space-1);
    justify-items: start;
  }

  .drift {
    color: var(--part);
    font-weight: 500;
  }

  .pin-note {
    margin-top: var(--space-3);
    font-size: var(--fs-0);
  }

  .pins {
    margin-top: var(--space-3);
  }

  .pins summary {
    width: fit-content;
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
    cursor: pointer;
  }

  .versions-box > summary {
    display: grid;
    gap: var(--space-1);
    width: fit-content;
    max-width: 100%;
    border-radius: var(--radius);
    cursor: pointer;
    list-style: none;
  }

  .versions-box > summary::-webkit-details-marker {
    display: none;
  }

  .versions-box[open] > summary {
    margin-bottom: var(--space-3);
  }

  .summary-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .summary-title :global(svg) {
    transition: transform var(--dur-fast) var(--ease);
  }

  .versions-box[open] .summary-title :global(svg) {
    transform: rotate(180deg);
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

  .who {
    font-weight: 500;
    white-space: nowrap;
  }

  .who a {
    color: var(--ink);
  }

  .who .case-link {
    color: var(--act);
    margin-inline-start: var(--space-2);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .act {
    white-space: nowrap;
  }

  .lang,
  .when,
  .verdict {
    white-space: nowrap;
  }

  /* Judge again is quiet in every row and full strength only on the row a pointer or focus is on (O3). */
  .again {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .feed tbody tr:hover .again,
  .again:focus-visible {
    color: var(--ink);
  }

  .extensions {
    display: grid;
    gap: var(--space-1);
    padding-left: var(--space-5);
    color: var(--ink-muted);
  }

  .end-now {
    display: grid;
    justify-items: start;
    gap: var(--space-1);
    margin-top: var(--space-2);
    padding-top: var(--space-4);
    border-top: var(--rule) solid var(--line);
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

  .ann .field,
  .ann-preview {
    width: 100%;
  }

  .ann-preview {
    padding: var(--space-3);
    border: var(--rule) dashed var(--line-strong);
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
    border: var(--rule) solid var(--line-strong);
    background: var(--paper);
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

  /* Phones: each feed row is time, student and problem, then the outcome and Judge again. */
  @media (width < 768px) {
    .feed tbody tr {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'when who plabel'
        'verdict verdict act';
      align-items: center;
      padding: var(--space-2) var(--space-3);
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

    .feed .act {
      grid-area: act;
      justify-self: end;
    }
  }
</style>
