<!--
  ContestEditor: an organiser's settings for one contest at
  /admin/contests/{id} (cache/v1/m4-plan.md sections 1, 2 and 4; laid out
  again in v2 P3 F6.3 and audit 2026-10-20 O1).

  - Title row: the state word ("Draft" until students can see it), one
    status line ("Not published. 3 problems, starts 21 Oct 2026 18:00")
    and "Publish contest" or "Unpublish contest", which asks first and
    names what students will see. Unsaved settings are saved with it.
  - From 1280 px a sticky column beside the form lists what the contest
    needs before it is published (lib/contests publishChecks); below that
    it comes first.
  - Basics: title, the contest's address as a read-only line (the server
    makes it from the title when the contest is created, V3-15), and
    Starts and Ends on one row.
  - Problems: the contest's problems with their letters, points and the
    revision students get, picked from validated problems in the bank.
    Adding, removing and reordering save at once, one request each; the
    letters follow the order. A contest refuses a problem the AI
    wizard made when no person has written a solution for it; the section
    then asks for a reason, and "Grade as the AI wrote it" saves again with it.
    A problem whose hidden tests practice showed
    says so in the picker, and publishing a contest with one
    names it in the confirm (D115).
  - "Rules and scoring", folded: freeze, pacing, languages and the
    options. Holding the board for a reveal needs a freeze.
  - Description and honour code, then Save changes for everything above.
    While the contest is live (published and past its start; an
    unpublished one past its start says students cannot see it yet),
    saving a change to the times or the freeze asks first. Leaving with
    unsaved settings asks first.
  - More: a clone (new times; the server names it from the title) and
    archiving.

  Students see a short explanation instead. Props: `id`.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import {
    fromLocalInput,
    labelFor,
    publishChecks,
    toLocalInput,
    type AdminContest,
    type AdminContestProblem,
    type ContestSettings,
  } from '../lib/contests';
  import { focusFirstInvalid, keepHeadingFocus } from '../lib/focus';
  import { countOf, formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { LANGUAGES, type ProblemSummary } from '../lib/problems';
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  type Form = Omit<ContestSettings, 'starts_at' | 'ends_at' | 'freeze_minutes' | 'min_gap_s' | 'max_per_problem'> & {
    starts_at: string;
    ends_at: string;
    freeze_minutes: string;
    min_gap_s: string;
    max_per_problem: string;
  };

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<AdminContest | null>(null);
  let form = $state<Form | null>(null);
  let formBaseline = $state('');
  let fields = $state<Record<string, string>>({});
  let formError = $state('');
  let formProblems = $state<readonly string[]>([]);
  let saveBusy = $state(false);
  let publishError = $state('');
  let leaveTo = $state<string | null>(null);

  let picked = $state<AdminContestProblem[]>([]);
  let bank = $state<ProblemSummary[]>([]);
  let addId = $state('');
  let problemsError = $state('');
  let problemsBusy = $state(false);
  // AI problems the server refused for a contest, and the reason to grade them as written.
  let aiLabels = $state<string[]>([]);
  let aiReason = $state('');
  let aiError = $state('');

  let cloneStart = $state('');
  let cloneEnd = $state('');
  let cloneError = $state('');
  /** The clone field the error is about, so it is marked and takes focus. */
  let cloneField = $state<'start' | 'end' | null>(null);

  // One confirmation dialog for every action that asks first.
  let confirm = $state<null | { title: string; body: string; items?: string[]; label: string; danger: boolean; run: () => Promise<void> }>(null);
  let confirmBusy = $state(false);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  // The server's state ignores `published`, so an unpublished contest past
  // its start reads "live" there; students cannot see it (C2-015).
  const running = $derived(contest?.state === 'live' || contest?.state === 'frozen');
  const live = $derived(!!contest?.published && running);
  const startedOrOver = $derived(running || contest?.state === 'ended' || contest?.state === 'practice');
  const pastStartUnpublished = $derived(!!contest && !contest.published && startedOrOver);
  const dirty = $derived(form !== null && JSON.stringify(form) !== formBaseline);
  const available = $derived(bank.filter((p) => p.validated && !p.archived && !picked.some((x) => x.problem_id === p.id)));

  /** The checklist, from the settings as typed and the problems as saved. */
  const checks = $derived.by(() => {
    if (!contest || !form) return [];
    return publishChecks(
      {
        problems: contest.problems ?? [],
        languages: form.languages,
        starts_at: fromLocalInput(form.starts_at) ?? contest.starts_at,
        freeze_minutes: num(form.freeze_minutes),
        hold_reveal: form.hold_reveal,
      },
      startedOrOver && !!contest.published,
      contest.server_now ?? Date.now(),
    );
  });
  const blocked = $derived(checks.some((c) => c.tone === 'bad'));
  const freezeOff = $derived(form !== null && !(num(form.freeze_minutes) > 0));

  /** The title row's line: whether students can see it, then what and when. */
  const statusLine = $derived.by(() => {
    if (!contest) return '';
    const n = (contest.problems ?? []).length;
    const what = countOf(n, 'problem');
    if (!contest.published) return `Not published. ${what}, starts ${formatDateTime(contest.starts_at)}.`;
    if (live) return `Published and live. Ends ${formatDateTime(contest.close_at ?? contest.ends_at)}.`;
    if (startedOrOver) return `Published. Ended ${formatDateTime(contest.close_at ?? contest.ends_at)}.`;
    return `Published. Students see it in their list. ${what}, starts ${formatDateTime(contest.starts_at)}.`;
  });

  function toForm(c: AdminContest): Form {
    return {
      title: c.title,
      description_md: c.description_md ?? '',
      starts_at: toLocalInput(c.starts_at),
      ends_at: toLocalInput(c.ends_at),
      freeze_minutes: String(c.freeze_minutes ?? 0),
      published: c.published,
      practice_after_end: c.practice_after_end,
      min_gap_s: String(c.min_gap_s),
      max_per_problem: String(c.max_per_problem),
      languages: [...(c.languages ?? [])],
      honour_text: c.honour_text ?? '',
      hold_reveal: c.hold_reveal ?? false,
    };
  }

  /** Takes a fresh copy of the contest; unsaved settings are kept unless `replace` is set. */
  function adopt(c: AdminContest, replace = true) {
    const keep = !replace && dirty && form !== null;
    contest = c;
    const saved = toForm(c);
    formBaseline = JSON.stringify(saved);
    if (!keep) form = saved;
    picked = (c.problems ?? []).map((p) => ({ ...p }));
    setTitle(routeTitle('admin-contest'), c.title);
    if (cloneStart === '') {
      cloneStart = toLocalInput(c.starts_at + 7 * 86_400_000);
      cloneEnd = toLocalInput(c.ends_at + 7 * 86_400_000);
    }
  }

  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) return true;
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const data = await getJSON<AdminContest>(base);
      const restore = keepHeadingFocus();
      adopt(data);
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Contest not found');
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
    }
    try {
      const data = await getJSON<{ problems: ProblemSummary[] }>('/api/admin/problems');
      bank = data.problems ?? [];
    } catch {
      bank = [];
    }
  }

  /** Takes a response that is the contest, or reloads it. Unsaved settings stay unless `replace`. */
  async function settle(value: unknown, replace = false) {
    if (value && typeof value === 'object' && 'id' in value && 'problems' in value) adopt(value as AdminContest, replace);
    else {
      try {
        adopt(await getJSON<AdminContest>(base), replace);
      } catch (e) {
        handleAuth(e);
      }
    }
  }

  function num(v: string): number {
    const n = Number(v);
    return Number.isFinite(n) ? Math.round(n) : NaN;
  }

  /** The PUT body from the form, or null after marking the fields that cannot be sent. */
  async function buildBody(): Promise<ContestSettings | null> {
    if (!form) return null;
    fields = {};
    formError = '';
    formProblems = [];
    const starts = fromLocalInput(form.starts_at);
    const ends = fromLocalInput(form.ends_at);
    const local: Record<string, string> = {};
    if (form.title.trim() === '') local.title = 'Enter a title.';
    if (starts === null) local.starts_at = 'Enter the start date and time.';
    if (ends === null) local.ends_at = 'Enter the end date and time.';
    else if (starts !== null && ends <= starts) local.ends_at = 'The contest must end after it starts.';
    if (form.languages.length === 0) local.languages = 'Allow at least one language.';
    for (const k of ['freeze_minutes', 'min_gap_s', 'max_per_problem'] as const) {
      if (Number.isNaN(num(form[k]))) local[k] = 'Enter a whole number.';
    }
    if (Object.keys(local).length > 0) {
      fields = local;
      await revealFirstInvalid();
      return null;
    }
    return {
      ...form,
      title: form.title.trim(),
      starts_at: starts as number,
      ends_at: ends as number,
      freeze_minutes: num(form.freeze_minutes),
      min_gap_s: num(form.min_gap_s),
      max_per_problem: num(form.max_per_problem),
    };
  }

  /** Opens the folded rules when the error is in them, then focuses the first marked field. */
  async function revealFirstInvalid() {
    const rules = document.getElementById('rules') as HTMLDetailsElement | null;
    if (rules && Object.keys(fields).some((k) => document.getElementById(`f-${k}`)?.closest('#rules') || k === 'languages')) rules.open = true;
    await tick();
    const first = document.querySelector<HTMLElement>('.settings [aria-invalid="true"]');
    if (first) first.focus();
    else if (fields.languages) document.querySelector<HTMLElement>('.settings .checks input')?.focus();
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!contest) return;
    const body = await buildBody();
    if (!body) return;
    const c = contest;
    // The form works in whole minutes, so times are compared to the minute.
    const minute = (ms: number) => Math.floor(ms / 60_000);
    const liveChange =
      minute(body.starts_at) !== minute(c.starts_at) ||
      minute(body.ends_at) !== minute(c.ends_at) ||
      body.freeze_minutes !== c.freeze_minutes;
    if (live && liveChange) {
      confirm = {
        title: 'Change a live contest?',
        body: 'Students see the change at once: new times or a new freeze can change their clocks and the board. To give more time, Extend contest in the console is safer; to stop now, use End the contest now there.',
        label: 'Save changes',
        danger: true,
        run: () => putSettings(body, 'Saved changes'),
      };
      return;
    }
    await putSettings(body, 'Saved changes');
  }

  async function putSettings(body: ContestSettings, done: string) {
    if (!contest) return;
    saveBusy = true;
    try {
      await settle(await sendJSON<AdminContest>('PUT', base, body), true);
      showToast(done);
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError) {
        publishError = e.fields.published ?? '';
        const rest = Object.fromEntries(Object.entries(e.fields).filter(([k]) => k !== 'published'));
        fields = rest;
        formProblems = e.problems;
        const known = Object.keys(rest).filter((k) => document.getElementById(`f-${k}`));
        if (!publishError && (known.length < Object.keys(rest).length || known.length === 0)) formError = e.message;
      } else {
        formError = errorText(e);
      }
      if (Object.keys(fields).length > 0) await revealFirstInvalid();
      else if (publishError) {
        await tick();
        document.getElementById('publish-error')?.focus();
      }
    } finally {
      saveBusy = false;
    }
  }

  /** The picked problems whose hidden tests a contest already showed (D115, C1-015). */
  const shownLabels = $derived(picked.filter((p) => bank.find((b) => b.id === p.problem_id)?.shown_in_practice).map((p) => p.label));

  /** The publish confirm's line for those problems. */
  function shownWarning(): string[] {
    if (shownLabels.length === 0) return [];
    const which = shownLabels.length === 1 ? `Problem ${shownLabels[0]} was` : `Problems ${shownLabels.join(', ')} were`;
    return [`${which} open for practice after a contest, so students may know the hidden tests.`];
  }

  /** Publishing and unpublishing ask first and say what students will see (C5, audit O1). */
  async function togglePublish() {
    if (!contest) return;
    publishError = '';
    const body = await buildBody();
    if (!body) return;
    const title = body.title || contest.title;
    if (!contest.published) {
      body.published = true;
      confirm = {
        title: `Publish ${title}?`,
        body: `Students from the allowed domains see it in their contest list at once${dirty ? ', with your unsaved settings saved' : ''}.`,
        items: [...checks.map((c) => c.text), ...shownWarning()],
        label: 'Publish contest',
        danger: false,
        run: () => putSettings(body, 'Published'),
      };
      return;
    }
    body.published = false;
    confirm = {
      title: `Unpublish ${title}?`,
      body: live
        ? 'The contest disappears from students at once, in the middle of it: they can no longer open the problems or submit. Submissions so far are kept.'
        : 'Students no longer see it in their list. Nothing is deleted, and you can publish it again.',
      label: 'Unpublish contest',
      danger: live,
      run: () => putSettings(body, 'Unpublished'),
    };
  }

  function toggleLanguage(lang: string, on: boolean) {
    if (!form) return;
    form.languages = on ? [...form.languages, lang].filter((v, i, a) => a.indexOf(v) === i) : form.languages.filter((l) => l !== lang);
  }

  // Problems: every change is saved at once (audit O1).
  async function addProblem() {
    const p = bank.find((x) => String(x.id) === addId);
    if (!p) return;
    const used = new Set(picked.map((x) => x.label));
    let i = 0;
    while (used.has(labelFor(i))) i++;
    const label = labelFor(i);
    picked = [...picked, { problem_id: p.id, title: p.title || p.slug, label, ready: true, max_score: p.points }];
    addId = '';
    if (await putProblems('', `Added ${p.title || p.slug} as ${label}`)) document.getElementById('add-problem')?.focus();
  }

  /** Problem changes run one after another, so a quick second press waits for the first save. */
  let problemQueue: Promise<unknown> = Promise.resolve();
  function queued(fn: () => Promise<unknown>): Promise<unknown> {
    problemQueue = problemQueue.then(fn, fn);
    return problemQueue;
  }

  // Buttons stay enabled while a save runs, so focus is never dropped.
  function move(i: number, by: number) {
    void queued(() => moveNow(i, by));
  }

  async function moveNow(i: number, by: number) {
    const j = i + by;
    if (j < 0 || j >= picked.length) return;
    const next = picked.slice();
    [next[i], next[j]] = [next[j], next[i]];
    // Letters follow the order, so A is always the first problem.
    picked = next.map((p, k) => ({ ...p, label: labelFor(k) }));
    // At the top or bottom the same arrow is disabled, so focus takes the other one.
    await tick();
    const same = document.getElementById(`move-${by < 0 ? 'up' : 'down'}-${j}`) as HTMLButtonElement | null;
    const other = document.getElementById(`move-${by < 0 ? 'down' : 'up'}-${j}`) as HTMLButtonElement | null;
    (same && !same.disabled ? same : other)?.focus();
    await putProblems('', `Moved ${picked[j].title} to ${picked[j].label}`);
  }

  function removeProblem(i: number) {
    const p = picked[i];
    const apply = async () => {
      picked = picked.filter((_, k) => k !== i).map((x, k) => ({ ...x, label: labelFor(k) }));
      // The removed row took the focused button with it: focus the row now in
      // its place, or the problem picker when the list is empty.
      await tick();
      const target = () => document.getElementById(`remove-${Math.min(i, picked.length - 1)}`) ?? document.getElementById('add-problem');
      target()?.focus();
      await putProblems('', `Removed ${p.title}`);
      // After a confirmation the dialog hands focus back to the removed button, so take it again.
      requestAnimationFrame(() => target()?.focus());
    };
    if (startedOrOver && (contest?.problems ?? []).some((x) => x.problem_id === p.problem_id)) {
      confirm = {
        title: `Remove problem ${p.label}?`,
        body: `This takes ${p.title} out of the contest, and its submissions leave the board.`,
        label: 'Remove problem',
        danger: true,
        run: () => queued(apply).then(() => undefined),
      };
      return;
    }
    void queued(apply);
  }

  /** Saves the letters after one is typed. */
  function onLetterChange() {
    void putProblems('', 'Saved the letters');
  }

  /** Saves the problems again with the reason to grade the refused AI problems as written. */
  async function gradeAsWritten() {
    aiError = '';
    if (aiReason.trim().length < 10) {
      aiError = 'Write a reason of at least 10 characters.';
      document.getElementById('ai-reason')?.focus();
      return;
    }
    const which = aiLabels.length === 1 ? `Problem ${aiLabels[0]} is` : `Problems ${aiLabels.join(', ')} are`;
    await putProblems(aiReason.trim(), `Saved problems. ${which} graded as the AI wrote ${aiLabels.length === 1 ? 'it' : 'them'}.`);
  }

  function cancelAi() {
    aiLabels = [];
    aiReason = '';
    aiError = '';
    problemsError = '';
    picked = (contest?.problems ?? []).map((p) => ({ ...p }));
  }

  /** Sends the problem list; true when the server took it. On a refusal the list goes back to what is saved. */
  async function putProblems(reason: string, done: string): Promise<boolean> {
    problemsError = '';
    const labels = picked.map((p) => p.label.trim().toUpperCase());
    if (labels.some((l) => !/^[A-Z]{1,3}$/.test(l))) {
      problemsError = 'Each letter must be one to three capital letters.';
      return false;
    }
    if (new Set(labels).size !== labels.length) {
      problemsError = 'Each problem needs its own letter.';
      return false;
    }
    problemsBusy = true;
    try {
      const value = await sendJSON<unknown>(
        'PUT',
        `${base}/problems`,
        picked.map((p, i) =>
          reason !== '' && aiLabels.includes(labels[i])
            ? { problem_id: p.problem_id, label: labels[i], ai_reason: reason }
            : { problem_id: p.problem_id, label: labels[i] },
        ),
      );
      await settle(value);
      showToast(done);
      aiLabels = [];
      aiReason = '';
      return true;
    } catch (e) {
      if (handleAuth(e)) return false;
      if (e instanceof ApiError && e.code === 'ai_unconfirmed') {
        // The new row stays until the reason is given or the step is cancelled.
        aiLabels = [...e.problems];
        aiError = reason !== '' ? e.message : '';
        problemsError = reason !== '' ? '' : e.message;
        await tick();
        document.getElementById('ai-reason')?.focus();
        return false;
      }
      problemsError = e instanceof ApiError && e.problems.length > 0 ? `${e.message} ${e.problems.join(' ')}` : errorText(e);
      picked = (contest?.problems ?? []).map((p) => ({ ...p }));
      return false;
    } finally {
      problemsBusy = false;
    }
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

  // Clone and archive.
  async function cloneContest(event: SubmitEvent) {
    event.preventDefault();
    cloneError = '';
    cloneField = null;
    const starts = fromLocalInput(cloneStart);
    const ends = fromLocalInput(cloneEnd);
    if (starts === null) [cloneField, cloneError] = ['start', 'Enter the start date and time.'];
    else if (ends === null || ends <= starts) [cloneField, cloneError] = ['end', 'Enter an end after the start.'];
    if (cloneField !== null) {
      await focusFirstInvalid('#clone-form');
      return;
    }
    try {
      const made = await sendJSON<AdminContest>('POST', `${base}/clone`, { starts_at: starts, ends_at: ends });
      showToast(`Cloned as ${made.slug}`);
      formBaseline = JSON.stringify(form);
      navigate(`/admin/contests/${made.id}`);
    } catch (e) {
      if (handleAuth(e)) return;
      cloneError = e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : errorText(e);
      const key = e instanceof ApiError ? Object.keys(e.fields)[0] : undefined;
      cloneField = key === 'starts_at' ? 'start' : key === 'ends_at' ? 'end' : null;
      if (cloneField !== null) await focusFirstInvalid('#clone-form');
    }
  }

  function archive() {
    if (!contest) return;
    confirm = {
      title: `Archive ${contest.title}?`,
      body: 'Students will no longer see it. Results and submissions are kept.',
      label: 'Archive contest',
      danger: true,
      run: async () => {
        await sendJSON<void>('DELETE', base);
        showToast('Archived');
        formBaseline = JSON.stringify(form);
        navigate('/admin/contests');
      },
    };
  }

  function leave() {
    const to = leaveTo;
    leaveTo = null;
    if (to === null) return;
    formBaseline = JSON.stringify(form);
    navigate(to);
  }

  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
  }

  /** A short reading of the folded rules, so they need not be opened to be checked. */
  const rulesSummary = $derived.by(() => {
    if (!form) return '';
    const freeze = num(form.freeze_minutes);
    const line = [
      freeze > 0 ? `board freezes ${freeze} minutes before the end` : 'no freeze',
      countOf(form.languages.length, 'language'),
    ].join(', ');
    return line.charAt(0).toUpperCase() + line.slice(1);
  });

  onMount(() => {
    setTitle(routeTitle('admin-contest'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
    return setNavigationGuard((to) => {
      if (!dirty) return true;
      leaveTo = to;
      return false;
    });
  });
</script>

<svelte:window onbeforeunload={onBeforeUnload} />

{#snippet err(key: string)}
  {#if fields[key]}<p id="f-{key}-error" class="field-error">{fields[key]}</p>{/if}
{/snippet}

{#if loadState === 'forbidden'}
  <PageHeader title="Contest settings" />
  <PageState kind="forbidden" message="Only organisers can manage contests." />
{:else if loadState !== 'ready' || !contest || !form}
  <PageHeader
    title={loadState === 'missing' ? 'Contest not found' : 'Contest settings'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Contest not found' : 'Contest settings' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} published={contest.published} current="settings">
    {#snippet actions()}
      <div class="publish-row">
        <p class="publish-line"><MonoDigits text={statusLine} /></p>
        <button
          type="button"
          class="btn {contest?.published ? 'btn-secondary' : 'btn-primary'}"
          disabled={saveBusy || (!contest?.published && blocked)}
          aria-describedby={!contest?.published && blocked ? 'publish-checks' : undefined}
          onclick={togglePublish}
        >
          {contest?.published ? 'Unpublish contest' : dirty ? 'Save and publish' : 'Publish contest'}
        </button>
      </div>
    {/snippet}
  </ContestAdminNav>

  {#if live}
    <div class="banner banner-info live-note" role="note">
      <p>
        This contest is live. Changes here reach students at once. To give more time or to end it now, use the
        <a href="/admin/contests/{encodeURIComponent(id)}/console">console</a>.
      </p>
    </div>
  {:else if pastStartUnpublished}
    <div class="banner banner-info live-note" role="note">
      <p>The start time has passed. Students cannot see this contest until you publish it, so move the start or publish now.</p>
    </div>
  {/if}

  <div class="page-split contest-split">
    <aside class="page-split-aside" aria-label="Before you publish">
      <section class="panel" aria-labelledby="checks-title">
        <h2 id="checks-title" class="panel-title">{contest.published ? 'What students get' : 'Before you publish'}</h2>
        <ul id="publish-checks" class="checks-list">
          {#each checks as c, i (i)}<li class="check-{c.tone}"><MonoDigits text={c.text} /></li>{/each}
        </ul>
        {#if publishError}
          <p id="publish-error" class="field-error" tabindex="-1" role="alert">{publishError}</p>
        {/if}
        {#if dirty}
          <p class="unsaved">Unsaved changes. Save changes at the end of the form{contest.published ? '' : ', or publish to save them too'}.</p>
        {/if}
      </section>
    </aside>

    <div class="page-split-main editor-main">
      <form class="settings" novalidate onsubmit={save} aria-label="Contest settings">
        <section class="section" aria-labelledby="basics-title">
          <h2 id="basics-title">Basics</h2>
          <p class="field-help">All three are required.</p>
          <div class="field">
            <label for="f-title">Title</label>
            <input id="f-title" aria-required="true" class="input" type="text" autocomplete="off" bind:value={form.title} aria-invalid={fields.title ? 'true' : undefined} aria-describedby={fields.title ? 'f-title-error' : undefined} />
            {@render err('title')}
          </div>
          <p class="muted small">Address <span class="mono">/contests/{contest.slug}</span>, made from the title when the contest was created. It does not change.</p>
          <div class="times">
            <div class="field">
              <label for="f-starts_at">Starts</label>
              <input id="f-starts_at" aria-required="true" class="input" type="datetime-local" bind:value={form.starts_at} aria-invalid={fields.starts_at ? 'true' : undefined} aria-describedby={fields.starts_at ? 'f-starts_at-error' : undefined} />
              {@render err('starts_at')}
            </div>
            <div class="field">
              <label for="f-ends_at">Ends</label>
              <input id="f-ends_at" aria-required="true" class="input" type="datetime-local" bind:value={form.ends_at} aria-invalid={fields.ends_at ? 'true' : undefined} aria-describedby={fields.ends_at ? 'f-ends_at-error' : undefined} />
              {@render err('ends_at')}
            </div>
          </div>
        </section>

        <section class="section" aria-labelledby="problems-title">
          <div class="section-head">
            <h2 id="problems-title">Problems</h2>
            {#if problemsBusy}<span class="muted small" role="status">Saving</span>{/if}
          </div>
          <p class="muted small">Only validated problems can be added. Each change is saved at once and pins the revision students get.</p>
          {#if picked.length === 0}
            <p class="muted">No problems yet. Add validated problems from the bank.</p>
          {:else}
            <ol class="picked">
              {#each picked as p, i (p.problem_id)}
                <li class="pick">
                  <label class="visually-hidden" for="label-{i}">Letter for {p.title}</label>
                  <input
                    id="label-{i}"
                    class="input mono letter"
                    type="text"
                    maxlength="3"
                    bind:value={p.label}
                    onchange={onLetterChange}
                    onkeydown={(e) => {
                      if (e.key === 'Enter') e.preventDefault();
                    }}
                  />
                  <span class="pick-title">
                    {p.title}
                    <span class="pick-meta">
                      {#if p.max_score !== undefined}<MonoDigits text={countOf(p.max_score, 'point')} />{/if}{#if p.ready === false}
                        <span class="warn"><Icon name="alert-triangle" /> Changed since it was added. Validate it, then add it again.</span>
                      {:else if p.rev}
                        <span><MonoDigits text={`, validated on revision ${p.rev}`} /></span>
                      {/if}
                    </span>
                  </span>
                  <span class="buttons">
                    <button id="move-up-{i}" type="button" class="btn btn-quiet btn-compact" aria-label="Move {p.title} up" title="Move up" disabled={i === 0} onclick={() => move(i, -1)}><Icon name="chevron-up" /></button>
                    <button id="move-down-{i}" type="button" class="btn btn-quiet btn-compact" aria-label="Move {p.title} down" title="Move down" disabled={i === picked.length - 1} onclick={() => move(i, 1)}><Icon name="chevron-down" /></button>
                    <button id="remove-{i}" type="button" class="btn btn-danger-quiet btn-compact" aria-label="Remove {p.title}" title="Remove" onclick={() => removeProblem(i)}><Icon name="trash" /></button>
                  </span>
                </li>
              {/each}
            </ol>
          {/if}
          <div class="row">
            <div class="field grow">
              <label for="add-problem">Add a validated problem</label>
              <select id="add-problem" class="select" bind:value={addId}>
                <option value="">Choose a problem</option>
                {#each available as p (p.id)}<option value={String(p.id)}>{p.title || p.slug} ({p.points} points){p.shown_in_practice ? ', hidden tests shown in practice' : ''}</option>{/each}
              </select>
            </div>
            <button type="button" class="btn btn-secondary" disabled={addId === '' || problemsBusy} onclick={addProblem}>Add problem</button>
          </div>
          {#if bank.length > 0 && available.length === 0}
            <p class="muted small">Every validated problem is in this contest. <a href="/admin/problems">Go to the problem bank</a> to validate more.</p>
          {/if}
          {#if problemsError}<p class="field-error" role="alert">{problemsError}</p>{/if}
          {#if aiLabels.length > 0}
            <div class="ai-confirm" role="group" aria-label="Grade as the AI wrote it">
              <div class="field">
                <label for="ai-reason">Reason to grade {aiLabels.length === 1 ? `problem ${aiLabels[0]}` : `problems ${aiLabels.join(', ')}`} as the AI wrote it</label>
                <textarea
                  id="ai-reason"
                  class="input"
                  rows="2"
                  bind:value={aiReason}
                  aria-invalid={aiError ? 'true' : undefined}
                  aria-describedby="ai-reason-help{aiError ? ' ai-reason-error' : ''}"
                ></textarea>
                <p id="ai-reason-help" class="field-help">
                  Better: write your own solution in the problem editor and validate it. The reason is kept in this contest's audit log.
                </p>
                {#if aiError}<p id="ai-reason-error" class="field-error">{aiError}</p>{/if}
              </div>
              <div class="row">
                <button type="button" class="btn btn-danger" disabled={problemsBusy} onclick={gradeAsWritten}>Grade as the AI wrote it</button>
                <button type="button" class="btn btn-secondary" onclick={cancelAi}>Cancel</button>
              </div>
            </div>
          {/if}
        </section>

        <section class="section" aria-labelledby="rules-title">
          <details id="rules" class="rules">
            <summary>
              <h2 id="rules-title" class="summary-title">Rules and scoring <Icon name="chevron-down" /></h2>
              <span class="muted small summary-line">{rulesSummary}</span>
            </summary>
            <div class="rules-body">
              <div class="grid">
                <div class="field">
                  <label for="f-freeze_minutes">Freeze the board, minutes before the end</label>
                  <input id="f-freeze_minutes" class="input mono" type="number" min="0" inputmode="numeric" bind:value={form.freeze_minutes} aria-invalid={fields.freeze_minutes ? 'true' : undefined} aria-describedby="f-freeze-help{fields.freeze_minutes ? ' f-freeze_minutes-error' : ''}" />
                  <p id="f-freeze-help" class="field-help">0 for no freeze.</p>
                  {@render err('freeze_minutes')}
                </div>
                <div class="field">
                  <label for="f-min_gap_s">Seconds between submissions</label>
                  <input id="f-min_gap_s" class="input mono" type="number" min="0" inputmode="numeric" bind:value={form.min_gap_s} aria-invalid={fields.min_gap_s ? 'true' : undefined} aria-describedby={fields.min_gap_s ? 'f-min_gap_s-error' : undefined} />
                  {@render err('min_gap_s')}
                </div>
                <div class="field">
                  <label for="f-max_per_problem">Submissions per problem</label>
                  <input id="f-max_per_problem" class="input mono" type="number" min="1" inputmode="numeric" bind:value={form.max_per_problem} aria-invalid={fields.max_per_problem ? 'true' : undefined} aria-describedby={fields.max_per_problem ? 'f-max_per_problem-error' : undefined} />
                  {@render err('max_per_problem')}
                </div>
              </div>

              <fieldset class="group" aria-describedby={fields.languages ? 'f-languages-error' : undefined}>
                <legend>Languages</legend>
                <div class="checks">
                  {#each LANGUAGES as l (l.id)}
                    <label class="choice">
                      <input type="checkbox" checked={form.languages.includes(l.id)} onchange={(e) => toggleLanguage(l.id, e.currentTarget.checked)} />
                      {l.label}
                    </label>
                  {/each}
                </div>
                {@render err('languages')}
              </fieldset>

              <fieldset class="group">
                <legend>Options</legend>
                <label class="choice"><input type="checkbox" bind:checked={form.practice_after_end} /> Open the problems for practice after the end. Practice shows students the first hidden test they fail.</label>
                <label class="choice" class:off={freezeOff && !form.hold_reveal}>
                  <input type="checkbox" bind:checked={form.hold_reveal} disabled={freezeOff && !form.hold_reveal} aria-describedby="f-hold-help" />
                  Hold the full board after the end until you reveal it
                </label>
                <p id="f-hold-help" class="field-help hold-help" class:warn-text={freezeOff && form.hold_reveal}>
                  Needs a freeze; with no freeze there is nothing to reveal.
                </p>
              </fieldset>
            </div>
          </details>
        </section>

        <section class="section" aria-labelledby="texts-title">
          <h2 id="texts-title">Description and honour code</h2>
          <div class="field">
            <label for="f-description_md">Description (Markdown)</label>
            <textarea id="f-description_md" class="input" rows="4" bind:value={form.description_md} aria-invalid={fields.description_md ? 'true' : undefined} aria-describedby={fields.description_md ? 'f-description_md-error' : undefined}></textarea>
            {@render err('description_md')}
          </div>
          <div class="field">
            <label for="f-honour_text">Honour code (Markdown)</label>
            <textarea
              id="f-honour_text"
              class="input"
              rows="6"
              placeholder={contest.default_honour_text ?? ''}
              bind:value={form.honour_text}
              aria-describedby="f-honour-help{fields.honour_text ? ' f-honour_text-error' : ''}"
            ></textarea>
            <p id="f-honour-help" class="field-help">Leave empty for the default, which lists what is recorded and for how long.</p>
            {@render err('honour_text')}
          </div>
        </section>

        {#if formError}
          <div class="banner banner-error" role="alert">
            <p>{formError}</p>
            {#if formProblems.length > 0}<ul>{#each formProblems as p, i (i)}<li>{p}</li>{/each}</ul>{/if}
          </div>
        {/if}
        <div class="actions save-row">
          <button type="submit" class="btn {dirty ? 'btn-primary' : 'btn-secondary'}" disabled={saveBusy}>Save changes</button>
          {#if dirty}<span class="unsaved">Unsaved changes</span>{/if}
        </div>
      </form>

      <section class="section more" aria-labelledby="more-title">
        <h2 id="more-title">More</h2>

        <div class="sub" role="group" aria-labelledby="clone-title">
          <h3 id="clone-title">Clone</h3>
          <p class="muted small">Copies the settings and problems into a new, unpublished contest with the same title.</p>
          <p class="field-help">Both are required.</p>
          <form id="clone-form" class="grid" novalidate onsubmit={cloneContest}>
            <div class="field">
              <label for="clone-start">Starts</label>
              <input id="clone-start" aria-required="true" class="input" type="datetime-local" bind:value={cloneStart} aria-invalid={cloneField === 'start' ? 'true' : undefined} aria-describedby={cloneField === 'start' ? 'clone-error' : undefined} />
            </div>
            <div class="field">
              <label for="clone-end">Ends</label>
              <input id="clone-end" aria-required="true" class="input" type="datetime-local" bind:value={cloneEnd} aria-invalid={cloneField === 'end' ? 'true' : undefined} aria-describedby={cloneField === 'end' ? 'clone-error' : undefined} />
            </div>
            <div class="actions span">
              <button type="submit" class="btn btn-secondary">Clone contest</button>
              {#if cloneError}<p id="clone-error" class="field-error" role="alert">{cloneError}</p>{/if}
            </div>
          </form>
        </div>

        <div class="sub" role="group" aria-labelledby="archive-title">
          <h3 id="archive-title">Archive</h3>
          <p class="muted small">Archived contests disappear from students' lists. Results and submissions are kept.</p>
          <div class="actions">
            <button type="button" class="btn btn-danger-quiet" onclick={archive}>Archive contest</button>
          </div>
        </div>
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
    busy={confirmBusy}
    onconfirm={runConfirm}
    oncancel={() => (confirm = null)}
  />

  <ConfirmDialog
    open={leaveTo !== null}
    title="Leave without saving?"
    body="Your changes to the settings will be lost. Problems are saved already."
    confirmLabel="Leave page"
    danger
    onconfirm={leave}
    oncancel={() => (leaveTo = null)}
  />
{/if}

<style>
  .publish-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-2) var(--space-4);
  }

  .publish-line {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .live-note {
    margin-top: var(--space-4);
    max-width: var(--measure-form);
  }

  .contest-split {
    margin-top: var(--space-5);
  }

  /* One column that never grows past the page. */
  .editor-main {
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }

  .panel-title {
    font-size: var(--fs-sub);
  }

  .checks-list {
    display: grid;
    gap: var(--space-2);
    padding-left: var(--space-5);
  }

  .check-ok::marker {
    color: var(--ok);
  }

  .check-part {
    color: var(--part);
  }

  .check-bad {
    color: var(--bad);
    font-weight: 500;
  }

  .unsaved {
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .section {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: var(--space-6);
    border-top: var(--rule) solid var(--line);
  }

  .settings > .section:first-child {
    margin-top: 0;
    padding-top: 0;
    border-top: 0;
  }

  .section h2,
  .summary-title {
    font-size: var(--fs-section);
    font-weight: 600;
    line-height: var(--lh-tight);
  }

  .section h3 {
    font-size: var(--fs-sub);
  }

  .section-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-3);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr));
    gap: var(--space-4);
  }

  /* Starts and Ends on one row from 768 px. */
  .times {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-4);
  }

  .span {
    grid-column: 1 / -1;
  }

  .rules summary {
    display: grid;
    gap: var(--space-1);
    width: fit-content;
    max-width: 100%;
    border-radius: var(--radius);
    cursor: pointer;
    list-style: none;
  }

  .rules summary::-webkit-details-marker {
    display: none;
  }

  .summary-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .summary-title :global(svg) {
    transition: transform var(--dur-fast) var(--ease);
  }

  .rules[open] .summary-title :global(svg) {
    transform: rotate(180deg);
  }

  .rules-body {
    display: grid;
    gap: var(--space-5);
    margin-top: var(--space-4);
  }

  .group {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    padding: 0;
    border: 0;
  }

  .group legend {
    margin-bottom: var(--space-2);
    padding: 0;
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .checks {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-5);
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .choice.off {
    color: var(--ink-faint);
    cursor: default;
  }

  .choice input {
    flex: none;
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .hold-help {
    margin-top: calc(-1 * var(--space-2));
    padding-left: var(--space-6);
  }

  .warn-text {
    color: var(--part);
    font-weight: 500;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  .save-row {
    margin-top: var(--space-6);
  }

  .banner {
    margin-top: var(--space-5);
  }

  .banner ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .ai-confirm {
    display: grid;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .picked {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .pick {
    display: grid;
    grid-template-columns: 4rem minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--space-3);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .letter {
    text-align: center;
    text-transform: uppercase;
  }

  .pick-title {
    display: grid;
    min-width: 0;
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .pick-meta {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .warn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .buttons {
    display: flex;
    gap: var(--space-1);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
  }

  .grow {
    flex: 1 1 16rem;
  }

  .more .sub {
    display: grid;
    gap: var(--space-3);
    padding-top: var(--space-4);
  }

  @media (min-width: 768px) {
    .times {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }

  @media (width < 768px) {
    .publish-row {
      justify-content: flex-start;
    }

    .pick {
      grid-template-columns: 3.5rem minmax(0, 1fr);
    }

    .buttons {
      grid-column: 1 / -1;
      justify-content: flex-end;
    }
  }
</style>
