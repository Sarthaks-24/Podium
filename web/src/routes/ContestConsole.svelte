<!--
  ContestConsole: the organiser's live view of one contest at
  /admin/contests/{id}/console, laid out for contest day. One sentence
  says how it is going, with a late judge or a slow queue first; the
  actions (Announce, Extend, Extra time, Pause judging) are dialogs that
  stay closed until used, and banners appear only when something needs
  action. Questions sit beside the overview and the submissions feed from
  1024 px and become tabs on phones. Actions that change what students see
  open the announcement dialog with a template, which is never posted by
  itself. New questions arrive over the tab's shared live channel and the
  rest refreshes every 10 s; the browser tab's title carries the
  unanswered count, so a waiting question shows from another tab.
-->
<script lang="ts">
  import { organises } from '../lib/roles';
  import { onDestroy, onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import DecisionDialog from '../components/DecisionDialog.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import Tabs from '../components/Tabs.svelte';
  import AnnounceDialog from '../components/console/AnnounceDialog.svelte';
  import ChangesDialog from '../components/console/ChangesDialog.svelte';
  import ExtendDialog from '../components/console/ExtendDialog.svelte';
  import ExtraTimeDialog from '../components/console/ExtraTimeDialog.svelte';
  import Feed from '../components/console/Feed.svelte';
  import Overview from '../components/console/Overview.svelte';
  import Questions from '../components/console/Questions.svelte';
  import TroubleDialog from '../components/console/TroubleDialog.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { loader } from '../lib/loader.svelte';
  import { media } from '../lib/breakpoints';
  import { serverClock } from '../lib/clock';
  import {
    ANNOUNCEMENT_TEMPLATES,
    askerName,
    consoleSentence,
    endNowFreeze,
    fillTemplate,
    formatCountdown,
    type AdminContest,
    type Announcement,
    type Clarification,
    type ContestSettings,
    type ContestStats,
    type FeedRow,
  } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { countOf } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { pinPath, updateEffect, type PinUpdate } from '../lib/history';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { studentChoice, type Standings } from '../lib/standings';
  import { LiveSubscription } from '../lib/stream';
  import { refreshTicker, ticker, useTicker } from '../lib/ticker.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { formatWhen } from '../lib/workspace';

  let { id }: { id: string } = $props();

  let contest = $state.raw<AdminContest | null>(null);
  let stats = $state.raw<ContestStats | null>(null);
  let statsError = $state('');
  let clarifications = $state.raw<Clarification[]>([]);
  let announcements = $state.raw<Announcement[]>([]);
  let students = $state.raw<{ user_id: number; name: string }[]>([]);
  let autoRefresh = $state(true);
  let feed: ReturnType<typeof Feed> | undefined = $state();

  // Questions seen so far, so only new ones are announced.
  let seenQuestions: Set<number> | null = null;
  let questionNews = $state('');

  // The one confirmation for pause, resume, end now, updates and every "Judge again".
  let confirm = $state<null | { title: string; body: string; items?: string[]; label: string; danger: boolean; run: () => Promise<void> }>(null);
  let confirmBusy = $state(false);
  let confirmError = $state('');

  // Dialogs.
  let announceOpen = $state(false);
  let annBody = $state('');
  let annTemplate = $state('');
  /** A template is filled in and the dialog opens once the dialog that asked for it has closed. */
  let announcePending = false;
  let extendOpen = $state(false);
  let extraOpen = $state(false);
  let changesOpen = $state(false);
  let changesKey = $state(0);
  let troubleOpen = $state(false);
  let deciding = $state<{ id: number; name: string } | null>(null);
  /** A problem whose tests changed with an update and still needs judging again. */
  let rejudgeNeeded = $state('');

  // Phones show one of three tabs (design/organiser.md); wider screens show all three.
  type Tab = 'questions' | 'submissions' | 'overview';
  let tab = $state<Tab>('questions');
  let phone = $state(false);

  const isOrganiser = $derived(organises(session.user?.role));
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const labels = $derived((contest?.problems ?? []).map((p) => p.label));
  const closeAt = $derived(contest ? (contest.close_at ?? contest.ends_at) : 0);
  const unanswered = $derived(clarifications.filter((c) => !c.answer).length);
  const running = $derived(contest !== null && ticker.now >= contest.starts_at && ticker.now < closeAt);
  const ended = $derived(contest !== null && ticker.now >= closeAt);
  const sentence = $derived(contest ? consoleSentence(contest, ticker.now, { unanswered, started: stats?.started ?? null }) : null);
  /** Names by user id, for change rows that name a student by id only. */
  const names = $derived(new Map([...students.map((s) => [s.user_id, s.name] as const), ...(contest?.extensions ?? []).map((x) => [x.user_id, x.name || x.email] as const)]));
  // Edits waiting for students; after the close they no longer need anyone.
  const updates = $derived(contest && !ended ? contest.problems.filter((p) => p.update_available) : []);
  const judgeErrors = $derived(contest?.queue?.errors ?? 0);
  const judgeTrouble = $derived(sentence !== null && /judge/i.test(sentence.trouble));
  const hasBanners = $derived(updates.length > 0 || judgeErrors > 0 || judgeTrouble || rejudgeNeeded !== '');

  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) return true;
    if (e.status === 403 && e.code === 'forbidden') {
      page.forbid();
      return true;
    }
    return false;
  }

  const page = loader(
    async () => {
      const sent = Date.now();
      const c = await getJSON<AdminContest>(base);
      if (typeof c.server_now === 'number') serverClock.observe(c.server_now, sent, Date.now());
      return c;
    },
    (c) => {
      refreshTicker();
      const restore = keepHeadingFocus();
      contest = c;
      void restore();
      // Every action here reloads the contest, so an open history follows it.
      changesKey += 1;
    },
  );

  async function loadContest(quiet = false) {
    await page.load({ quiet });
    if (page.live && page.state === 'missing') setTitle('Contest not found');
  }

  async function loadStats() {
    try {
      stats = await getJSON<ContestStats>(`${base}/stats`);
      statsError = '';
    } catch (e) {
      if (!handleAuth(e) && stats === null) statsError = 'The numbers could not be loaded. They refresh every 10 s.';
    }
  }

  async function loadSide() {
    try {
      const [a, c] = await Promise.all([
        getJSON<{ announcements: Announcement[] }>(`${base}/announcements`),
        getJSON<{ clarifications: Clarification[] }>(`${base}/clarifications`),
      ]);
      announcements = [...(a.announcements ?? [])].sort((x, y) => y.created_at - x.created_at);
      const list = c.clarifications ?? [];
      const fresh = list.filter((q) => !q.answer && seenQuestions !== null && !seenQuestions.has(q.id));
      if (fresh.length === 1) {
        const q = fresh[0];
        questionNews = `New question from ${askerName(q)}${q.problem_label ? ` about problem ${q.problem_label}` : ''}.`;
      } else if (fresh.length > 1) {
        questionNews = `${fresh.length} new questions.`;
      }
      seenQuestions = new Set(list.map((q) => q.id));
      clarifications = list;
    } catch (e) {
      handleAuth(e);
    }
  }

  async function loadStudents() {
    try {
      const s = await getJSON<Standings>(`${base}/standings`);
      students = s.rows.map((r) => ({ user_id: r.user_id, name: studentChoice(r) })).sort((a, b) => a.name.localeCompare(b.name));
    } catch {
      students = [];
    }
  }

  function ask(title: string, body: string, label: string, danger: boolean, run: () => Promise<void>, items?: string[]) {
    confirmError = '';
    confirm = { title, body, label, danger, run, items };
  }

  async function runConfirm() {
    if (!confirm) return;
    confirmBusy = true;
    confirmError = '';
    try {
      await confirm.run();
      confirm = null;
      void openAnnouncePending();
    } catch (e) {
      // The refusal stays in the dialog, where it can be read over the backdrop.
      if (handleAuth(e)) confirm = null;
      else confirmError = errorText(e);
    } finally {
      confirmBusy = false;
    }
  }

  /** Fills the announcement from a template; the dialog opens after the current one closes. Posting stays the organiser's choice. */
  function suggest(templateId: string, values: { label?: string; minutes?: number } = {}) {
    const t = ANNOUNCEMENT_TEMPLATES.find((x) => x.id === templateId);
    if (!t) return;
    annTemplate = templateId;
    annBody = fillTemplate(t.text, values);
    announcePending = true;
  }

  /** Opens a filled-in announcement once the closing dialog has handed focus back. */
  async function openAnnouncePending() {
    if (!announcePending) return;
    announcePending = false;
    await tick();
    announceOpen = true;
  }

  function pause() {
    ask(
      'Pause judging?',
      'Nothing is judged until you resume. Students can keep submitting: their submissions wait in the queue and nothing is lost.',
      'Pause judging',
      true,
      async () => {
        await sendJSON<void>('POST', `${base}/pause`);
        showToast('Paused judging');
        await loadContest(true);
        suggest('paused');
      },
    );
  }

  function resume() {
    ask('Resume judging?', 'The submissions waiting in the queue are judged in the order they came in, then new ones as they arrive.', 'Resume judging', false, async () => {
      await sendJSON<void>('POST', `${base}/resume`);
      showToast('Resumed judging');
      await loadContest(true);
      suggest('resumed');
    });
  }

  /** The settings as the server holds them, for a change of one field. */
  function settingsOf(c: AdminContest): ContestSettings {
    return {
      title: c.title,
      description_md: c.description_md ?? '',
      starts_at: c.starts_at,
      ends_at: c.ends_at,
      freeze_minutes: c.freeze_minutes,
      published: c.published,
      practice_after_end: c.practice_after_end,
      min_gap_s: c.min_gap_s,
      max_per_problem: c.max_per_problem,
      languages: [...(c.languages ?? [])],
      honour_text: c.honour_text ?? '',
      hold_reveal: c.hold_reveal ?? false,
    };
  }

  /** Ends a running contest now: the end moves to the server's now, minus any time added for everyone. */
  function endNow() {
    const c = contest;
    if (!c) return;
    const personal = (c.extensions ?? []).filter((x) => x.minutes > 0).length;
    const items = [
      'Queued submissions are still judged.',
      c.practice_after_end ? 'Students see Ended, and practice opens.' : 'Students see Ended.',
    ];
    if (personal > 0) items.push(`${countOf(personal, 'student')} with extra time keep${personal === 1 ? 's' : ''} it.`);
    // The freeze is counted back from the end: say what happens to the board.
    const frozenAt = c.freeze_minutes > 0 ? closeAt - c.freeze_minutes * 60_000 : null;
    if (frozenAt !== null) {
      // Name the instant that will be stored: the freeze is kept in whole minutes.
      const now = serverClock.serverNow();
      const keptAt = now - endNowFreeze(c, now - (c.extended_minutes ?? 0) * 60_000, now) * 60_000;
      items.push(
        now < frozenAt
          ? 'The board was not frozen yet, so it stays as students see it now: the freeze and any held reveal are dropped.'
          : `The board stays frozen as it was at ${formatWhen(keptAt, ticker.now)}.`,
      );
    }
    ask(
      'End the contest now?',
      "Everyone's time ends now: the problems close for new contest submissions at once.",
      'End the contest',
      true,
      async () => {
        const now = Math.ceil(serverClock.serverNow() / 1000) * 1000;
        const ends = now - (c.extended_minutes ?? 0) * 60_000;
        if (ends <= c.starts_at) throw new ApiError(400, 'bad_request', 'The time added for everyone is longer than the time run so far, so the end cannot move before the start. Remove the extension first.');
        const updated = await sendJSON<AdminContest>('PUT', base, { ...settingsOf(c), ends_at: ends, freeze_minutes: endNowFreeze(c, ends, now) });
        if (updated && typeof updated === 'object' && 'id' in updated) contest = updated;
        showToast('Ended the contest');
        await loadContest(true);
      },
      items,
    );
  }

  function rejudgeOne(row: FeedRow) {
    const label = row.problem_label;
    ask(
      `Judge ${row.name}'s submission again?`,
      `This submission to problem ${label} is judged again with the tests students are served now. Its score in the standings may change.`,
      'Judge again',
      false,
      async () => {
        await sendJSON<void>('POST', `${base}/rejudge`, { submission_id: row.id });
        showToast(`Judging ${row.name}'s submission to ${label} again`);
        await Promise.all([feed?.load(), loadContest(true)]);
      },
    );
  }

  function rejudgeProblem(label: string) {
    ask(
      `Judge problem ${label} again?`,
      `Every submission to problem ${label} is judged again with the tests students are served now. New submissions still go first, so the queue keeps moving. Scores in the standings may change.`,
      'Judge again',
      true,
      async () => {
        const out = await sendJSON<{ queued?: number } | null>('POST', `${base}/rejudge`, { problem_label: label });
        const n = out && typeof out.queued === 'number' ? out.queued : null;
        showToast(n !== null ? `Judging ${countOf(n, 'submission')} to problem ${label} again` : `Judging problem ${label} again`);
        if (rejudgeNeeded === label) rejudgeNeeded = '';
        await Promise.all([feed?.load(), loadContest(true)]);
        suggest('rejudge', { label });
      },
    );
  }

  function updatePin(label: string) {
    const p = contest?.problems.find((x) => x.label === label);
    if (!p || p.latest_revision_id === undefined) return;
    const target = p.latest_revision_id;
    ask(`Update students now to the edited problem ${label}?`, updateEffect({ label, update_statement_only: p.update_statement_only }), 'Update students now', false, async () => {
      const out = await sendJSON<PinUpdate<AdminContest>>('PUT', pinPath(id, label), { revision_id: target });
      if (out.contest && typeof out.contest === 'object') contest = out.contest;
      showToast(`Students now get the edited problem ${label}`);
      if (out.rejudge_needed) rejudgeNeeded = label;
      changesKey += 1;
      // The server words an announcement; posting it stays the organiser's choice.
      annTemplate = '';
      annBody = out.announcement_md ?? '';
      announcePending = annBody !== '';
    });
  }

  async function afterExtend(minutes: number) {
    extendOpen = false;
    showToast(`Extended by ${countOf(minutes, 'minute')}`);
    await loadContest(true);
    suggest('extended', { minutes });
    await openAnnouncePending();
  }

  async function afterExtra(message: string) {
    extraOpen = false;
    showToast(message);
    await loadContest(true);
  }

  async function afterPost() {
    announceOpen = false;
    showToast('Posted the announcement');
    await loadSide();
  }

  function showJudgeErrors() {
    tab = 'submissions';
    feed?.showErrors();
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  let stream: LiveSubscription | null = null;
  let unwatch: (() => void) | undefined;

  onMount(() => {
    setTitle(routeTitle('contest-console'));
    const tablet = media('tablet');
    phone = !tablet.matches;
    const onWidth = () => (phone = !tablet.matches);
    tablet.addEventListener('change', onWidth);
    unwatch = () => tablet.removeEventListener('change', onWidth);
    if (!isOrganiser) {
      page.forbid();
      return;
    }
    void loadContest().then(() => {
      if (page.state !== 'ready') return;
      void loadSide();
      void loadStats();
      void loadStudents();
    });
    const refresh = () => {
      if (!autoRefresh || page.state !== 'ready' || document.visibilityState === 'hidden') return;
      void feed?.load();
      void loadSide();
      void loadStats();
      // The contest carries the queue and the judge's last request for work.
      void loadContest(true);
    };
    timer = setInterval(refresh, 10_000);
    // A hidden tab skips the refresh, so coming back to it refreshes at once.
    document.addEventListener('visibilitychange', refresh);
    const stopWidth = unwatch;
    unwatch = () => {
      stopWidth?.();
      document.removeEventListener('visibilitychange', refresh);
    };
    // New questions arrive at once over the tab's shared stream; the refresh above is the fallback.
    stream = new LiveSubscription({
      events: ['clarification'],
      onEvent: () => {
        if (page.state === 'ready') void loadSide();
      },
      onReconnect: () => void loadSide(),
      poll: () => undefined,
    });
    stream.start();
    return useTicker();
  });

  // The feed loads once it is drawn (it is drawn with the contest).
  let feedLoaded = false;
  $effect(() => {
    if (feed && !feedLoaded) {
      feedLoaded = true;
      void feed.load();
    }
  });

  // The tab title carries the unanswered count, so a waiting question shows from another tab.
  $effect(() => {
    if (!contest) return;
    setTitle(unanswered > 0 ? `(${unanswered}) ${routeTitle('contest-console')}` : routeTitle('contest-console'), contest.title);
  });

  onDestroy(() => {
    page.stop();
    clearInterval(timer);
    stream?.stop();
    unwatch?.();
  });
</script>

{#snippet timeActions()}
  {#if contest}
    {#if ticker.now < contest.starts_at}
      <p class="countdown">Starts in <MonoDigits text={formatCountdown(contest.starts_at - ticker.now)} /></p>
    {:else if running}
      <p class="countdown">Ends in <MonoDigits text={formatCountdown(closeAt - ticker.now)} /></p>
      <button type="button" class="btn btn-secondary btn-danger-secondary btn-compact" onclick={endNow}>End now</button>
    {:else}
      <p class="countdown">Ended <MonoDigits text={formatWhen(closeAt, ticker.now)} /></p>
    {/if}
  {/if}
{/snippet}

{#if page.state === 'forbidden'}
  <PageHeader title={routeTitle('contest-console')} />
  <PageState kind="forbidden" message="Only organisers can run contests." />
{:else if page.state !== 'ready' || !contest}
  <PageHeader
    title={page.state === 'missing' ? 'Contest not found' : routeTitle('contest-console')}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: page.state === 'missing' ? 'Contest not found' : routeTitle('contest-console') }]}
  />
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if page.state === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={page.error} onretry={() => loadContest()} />
  {/if}
{:else}
  <ContestAdminNav
    {id}
    slug={contest.slug}
    title={contest.title}
    state={contest.state}
    published={contest.published}
    current="console"
    {unanswered}
    actions={timeActions}
  />

  <div class="top">
    {#if sentence}
      <p class="sentence" id="console-sentence">
        {#if sentence.trouble}<strong class="trouble">{sentence.trouble}</strong>{/if}{sentence.trouble ? ` ${sentence.text}` : sentence.text}
      </p>
    {/if}
    <div class="actions" role="group" aria-label="Contest actions">
      <button type="button" class="btn btn-secondary btn-compact" onclick={() => (announceOpen = true)}>Announce</button>
      <button type="button" class="btn btn-secondary btn-compact" onclick={() => (extendOpen = true)}>Extend</button>
      <button type="button" class="btn btn-secondary btn-compact" onclick={() => (extraOpen = true)}>Extra time</button>
      {#if contest.paused}
        <button type="button" class="btn btn-primary btn-compact" onclick={resume}>Resume judging</button>
      {:else}
        <button type="button" class="btn btn-quiet btn-compact" onclick={pause}>Pause judging</button>
      {/if}
      <span class="links">
        <button type="button" class="btn btn-link btn-compact" onclick={() => (changesOpen = true)}>History of changes</button>
        <button type="button" class="btn btn-link btn-compact" onclick={() => (troubleOpen = true)}>If something goes wrong</button>
      </span>
    </div>
  </div>

  {#if hasBanners}
    <div class="banners" id="versions-title" role="region" aria-label="Needs attention" tabindex="-1">
      {#each updates as p (p.label)}
        <div class="banner banner-info">
          <p>Problem {p.label} was edited. Students still see the old {p.update_statement_only ? 'statement' : 'statement and tests'}.</p>
          <button type="button" class="btn btn-secondary btn-compact" onclick={() => updatePin(p.label)}
            >Update students now<span class="visually-hidden"> for problem {p.label}</span></button
          >
        </div>
      {/each}
      {#if rejudgeNeeded !== ''}
        <div class="banner banner-info">
          <p>The tests of problem {rejudgeNeeded} changed. Judge it again so every score uses the new tests.</p>
          <button type="button" class="btn btn-secondary btn-compact" onclick={() => rejudgeProblem(rejudgeNeeded)}>Judge problem {rejudgeNeeded} again</button>
        </div>
      {/if}
      {#if judgeTrouble}
        <div class="banner banner-error">
          <p>The judge is not taking work. Pause judging, restart the judge, then judge again.</p>
          <button type="button" class="btn btn-secondary btn-compact" onclick={() => (troubleOpen = true)}>What to do</button>
        </div>
      {/if}
      {#if judgeErrors > 0}
        <div class="banner banner-error">
          <p>{countOf(judgeErrors, 'judge error')}. Those students see an error and can submit again.</p>
          <button type="button" class="btn btn-secondary btn-compact" onclick={showJudgeErrors}>Show<span class="visually-hidden"> the judge errors</span></button>
        </div>
      {/if}
    </div>
  {/if}
  <p class="visually-hidden" aria-live="polite">{questionNews}</p>

  {#if phone}
    <div class="phone-tabs">
      <Tabs
        tabs={[
          { id: 'questions', label: 'Questions', count: unanswered || undefined },
          { id: 'submissions', label: 'Submissions' },
          { id: 'overview', label: 'Overview' },
        ]}
        selected={tab}
        onselect={(t: Tab) => (tab = t)}
        label="Live view"
        idPrefix="console-tab"
        controls={(t: Tab) => `console-${t}`}
        compact
      />
    </div>
  {/if}

  <div class="live">
    <div
      class="area-questions"
      id="console-questions"
      role={phone ? 'tabpanel' : undefined}
      aria-labelledby={phone ? 'console-tab-questions' : undefined}
      hidden={phone && tab !== 'questions'}
    >
      <Questions base={base} {clarifications} onanswered={loadSide} />
    </div>
    <div
      class="area-overview"
      id="console-overview"
      role={phone ? 'tabpanel' : undefined}
      aria-labelledby={phone ? 'console-tab-overview' : undefined}
      hidden={phone && tab !== 'overview'}
    >
      <Overview {stats} error={statsError} problems={contest.problems} queue={contest.queue} onrejudge={rejudgeProblem} />
    </div>
    <div
      class="area-feed"
      id="console-submissions"
      role={phone ? 'tabpanel' : undefined}
      aria-labelledby={phone ? 'console-tab-submissions' : undefined}
      hidden={phone && tab !== 'submissions'}
    >
      <Feed
        bind:this={feed}
        {base}
        startsAt={contest.starts_at}
        {labels}
        {students}
        exam={contest}
        bind:autoRefresh
        onrejudge={rejudgeOne}
        ondecide={(row) => (deciding = { id: row.user_id, name: row.name })}
      />
    </div>
  </div>

  <AnnounceDialog
    open={announceOpen}
    {base}
    {announcements}
    {labels}
    minutesLeft={Math.max(1, Math.round((closeAt - ticker.now) / 60_000))}
    bind:body={annBody}
    bind:template={annTemplate}
    onclose={() => (announceOpen = false)}
    onposted={afterPost}
  />
  <ExtendDialog open={extendOpen} {base} {closeAt} {ended} onclose={() => (extendOpen = false)} ondone={afterExtend} />
  <ExtraTimeDialog
    open={extraOpen}
    {base}
    {students}
    extensions={contest.extensions ?? []}
    {ended}
    onclose={() => (extraOpen = false)}
    ondone={afterExtra}
  />
  <ChangesDialog open={changesOpen} {base} {names} refresh={changesKey} onclose={() => (changesOpen = false)} />
  <TroubleDialog open={troubleOpen} onclose={() => (troubleOpen = false)} />

  <DecisionDialog
    open={deciding !== null}
    contestId={id}
    student={deciding}
    onclose={() => (deciding = null)}
    ondecided={() => {
      deciding = null;
      showToast('Recorded the decision');
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
    error={confirmError}
    onconfirm={runConfirm}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  .countdown {
    white-space: nowrap;
  }

  .top {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  /* The one sentence: body size, the trouble in the error colour (state is also the words). */
  .sentence {
    font-size: var(--fs-2);
    line-height: var(--lh-tight);
  }

  .trouble {
    color: var(--bad);
    font-weight: 600;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .links {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 0 var(--space-3);
  }

  .banners {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }

  .banners .banner {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
  }

  .phone-tabs {
    margin-top: var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  /* At 320 px the three tabs do not fit one row; they wrap rather than scroll the page. */
  .phone-tabs :global(.tabs) {
    flex-wrap: wrap;
  }

  /* Questions first below 1024 px; from there beside the work. */
  .live {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas:
      'questions'
      'overview'
      'feed';
    gap: var(--space-5);
    align-items: start;
    margin-top: var(--space-5);
  }

  .area-questions {
    grid-area: questions;
  }

  .area-overview {
    grid-area: overview;
  }

  .area-feed {
    grid-area: feed;
  }

  .live > [hidden] {
    display: none;
  }

  /* Phones: the two links share one line under the buttons. */
  @media (width < 768px) {
    .live {
      margin-top: var(--space-3);
    }

    .links {
      flex-basis: 100%;
      gap: 0;
    }

    .links .btn-link {
      padding-inline: var(--space-2);
    }
  }

  @media (min-width: 1024px) {
    .live {
      grid-template-columns: minmax(0, 1fr) minmax(0, var(--measure-aside));
      grid-template-areas:
        'overview questions'
        'feed questions';
    }
  }
</style>
