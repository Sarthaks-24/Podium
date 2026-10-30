<!--
  ContestEditor: Setup, an organiser's settings for one contest at
  /admin/contests/{id}. Every field saves as it changes with a PUT of only
  the changed fields, and one line in the title row says "Saved", "Saving"
  or "Not saved", so there is no Save button to forget; leaving with a
  change pending, or changing the start, length or hidden minutes while
  the contest is live, asks first, because students see it at once. The
  six inputs come first (title, problems, start, length, hidden minutes,
  languages) and everything else waits under Advanced; from 1280 px a
  sticky column lists what the contest still needs before it can be
  published. Students see a short explanation instead.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onMount, tick } from 'svelte';
  import ActionMenu from '../components/ActionMenu.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import DuplicateDialog from '../components/DuplicateDialog.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import { ApiError, errorText, getJSON, isSignedOut, sendJSON } from '../lib/api';
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
  import { loader } from '../lib/loader.svelte';
  import { routeTitle } from '../lib/routing';
  import { LANGUAGES, type ProblemSummary } from '../lib/problems';
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  /** A setting as the form holds it; `length` is minutes, sent as ends_at. */
  type Key =
    | 'title'
    | 'starts_at'
    | 'length'
    | 'freeze_minutes'
    | 'languages'
    | 'min_gap_s'
    | 'max_per_problem'
    | 'practice_after_end'
    | 'hold_reveal'
    | ExamKey
    | 'description_md'
    | 'honour_text';

  interface Form {
    title: string;
    starts_at: string;
    // Number inputs bind numbers, or null when empty.
    length: number | string | null;
    freeze_minutes: number | string | null;
    languages: string[];
    min_gap_s: number | string | null;
    max_per_problem: number | string | null;
    practice_after_end: boolean;
    hold_reveal: boolean;
    full_screen: boolean;
    count_focus: boolean;
    block_paste: boolean;
    block_copy: boolean;
    description_md: string;
    honour_text: string;
  }

  type ExamKey = 'full_screen' | 'count_focus' | 'block_paste' | 'block_copy';
  /** The exam options, each a switch that is off by default, with what it does. */
  const EXAM: readonly { key: ExamKey; label: string; help: string }[] = [
    { key: 'full_screen', label: 'Full screen on start', help: 'Asks for full screen when a student presses Start, and counts each time they leave it.' },
    { key: 'count_focus', label: 'Count tab switches and focus loss', help: 'Shows you how often each student left the page. Podium records it either way.' },
    { key: 'block_paste', label: 'Block paste into the editor', help: 'A paste inserts nothing, and the student is told why.' },
    { key: 'block_copy', label: 'Block copy out of the statement', help: 'Copying the statement or the examples does nothing, and the student is told why.' },
  ];

  /** Typing saves after this pause. */
  const PAUSE_MS = 800;
  /** The settings under Advanced, so an error there opens it. */
  const ADVANCED: readonly Key[] = [
    'min_gap_s',
    'max_per_problem',
    'practice_after_end',
    'hold_reveal',
    'full_screen',
    'count_focus',
    'block_paste',
    'block_copy',
    'description_md',
    'honour_text',
  ];
  /** Changes a live contest's clock or board, so they ask first. */
  const TIMING: readonly Key[] = ['starts_at', 'length', 'freeze_minutes'];

  let contest = $state.raw<AdminContest | null>(null);
  let form = $state<Form | null>(null);
  let fields = $state<Partial<Record<Key, string>>>({});
  let advancedOpen = $state(false);

  // Autosave: the keys changed and not yet saved, an edit count per key so a
  // save that raced a newer edit keeps the key pending, and the save running.
  let pending = $state<Key[]>([]);
  const edits: Partial<Record<Key, number>> = {};
  let timer: ReturnType<typeof setTimeout> | undefined;
  let inflight: Promise<void> | null = null;
  let saving = $state(false);
  let saveState = $state<'' | 'failed' | 'invalid'>('');
  let saveError = $state('');
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

  let duplicating = $state(false);

  // One confirmation dialog for every action that asks first.
  let confirm = $state<null | {
    title: string;
    body: string;
    items?: string[];
    label: string;
    danger: boolean;
    run: () => Promise<void>;
    cancel?: () => void;
  }>(null);
  let confirmBusy = $state(false);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const page = loader(
    () => getJSON<AdminContest>(base),
    (c) => {
      adopt(c);
      form = toForm(c);
    },
  );
  // The server's state ignores `published`, so an unpublished contest past
  // its start reads "live" there; students cannot see it.
  const running = $derived(contest?.state === 'live' || contest?.state === 'frozen');
  const live = $derived(!!contest?.published && running);
  const startedOrOver = $derived(running || contest?.state === 'ended' || contest?.state === 'practice');
  const pastStartUnpublished = $derived(!!contest && !contest.published && startedOrOver);
  /** A change waits for its save, or failed: leaving asks first. */
  const dirty = $derived(pending.length > 0);
  const available = $derived(bank.filter((p) => p.validated && !p.archived && !picked.some((x) => x.problem_id === p.id)));

  /** The checklist, from the settings as typed and the problems as saved. */
  const checks = $derived.by(() => {
    if (!contest || !form) return [];
    return publishChecks(
      {
        problems: contest.problems ?? [],
        languages: form.languages,
        starts_at: fromLocalInput(form.starts_at) ?? contest.starts_at,
        freeze_minutes: whole(form.freeze_minutes),
        hold_reveal: form.hold_reveal,
      },
      startedOrOver && !!contest.published,
      contest.server_now ?? Date.now(),
    );
  });
  const blocked = $derived(checks.some((c) => c.tone === 'bad'));
  const hiddenOff = $derived(form !== null && !(whole(form.freeze_minutes) > 0));

  /** The title row's line: whether students can see it, then what and when. */
  const statusLine = $derived.by(() => {
    if (!contest) return '';
    const what = countOf((contest.problems ?? []).length, 'problem');
    if (!contest.published) return `Not published. ${what}, starts ${formatDateTime(contest.starts_at)}.`;
    if (live) return `Published and live. Ends ${formatDateTime(contest.close_at ?? contest.ends_at)}.`;
    if (startedOrOver) return `Published. Ended ${formatDateTime(contest.close_at ?? contest.ends_at)}.`;
    return `Published. Students see it in their list. ${what}, starts ${formatDateTime(contest.starts_at)}.`;
  });

  /** The one save line: settings and problems save through it alike. */
  const saveText = $derived(
    saveState === 'failed'
      ? 'Not saved.'
      : saveState === 'invalid'
        ? 'Not saved. Fix the marked field.'
        : saving || problemsBusy || pending.length > 0
          ? 'Saving'
          : 'Saved',
  );

  /** The end the length gives, for the line under it. */
  const endsAt = $derived.by(() => {
    if (!form) return null;
    const start = fromLocalInput(form.starts_at);
    const len = whole(form.length);
    return start !== null && len > 0 ? start + len * 60_000 : null;
  });

  /** A whole number from an input, or NaN. */
  function whole(v: unknown): number {
    const text = String(v ?? '').trim();
    const n = Number(text);
    return text !== '' && Number.isInteger(n) ? n : NaN;
  }

  function toForm(c: AdminContest): Form {
    return {
      title: c.title,
      starts_at: toLocalInput(c.starts_at),
      length: Math.round((c.ends_at - c.starts_at) / 60_000),
      freeze_minutes: c.freeze_minutes ?? 0,
      languages: [...(c.languages ?? [])],
      min_gap_s: c.min_gap_s,
      max_per_problem: c.max_per_problem,
      practice_after_end: c.practice_after_end,
      hold_reveal: c.hold_reveal ?? false,
      full_screen: c.full_screen ?? false,
      count_focus: c.count_focus ?? false,
      block_paste: c.block_paste ?? false,
      block_copy: c.block_copy ?? false,
      description_md: c.description_md ?? '',
      honour_text: c.honour_text ?? '',
    };
  }

  /** Takes the server's copy of the contest. The form is the organiser's and stays. */
  function adopt(c: AdminContest) {
    contest = c;
    // An AI problem waiting for its reason is not saved yet, so the list stays.
    if (aiLabels.length === 0) picked = (c.problems ?? []).map((p) => ({ ...p }));
    setTitle(routeTitle('admin-contest'), c.title);
  }

  async function load() {
    const restore = keepHeadingFocus();
    await page.load();
    void restore();
    if (page.state !== 'ready') return;
    try {
      const data = await getJSON<{ problems: ProblemSummary[] }>('/api/admin/problems');
      if (page.live) bank = data.problems ?? [];
    } catch {
      bank = [];
    }
  }

  // Autosave.

  /** Marks settings changed; they save after a pause, or at once with `now`. */
  function change(keys: Key[], now = false) {
    const next = { ...fields };
    for (const k of keys) {
      edits[k] = (edits[k] ?? 0) + 1;
      delete next[k];
    }
    fields = next;
    pending = [...pending, ...keys.filter((k) => !pending.includes(k))];
    if (saveState === 'failed') saveState = '';
    clearTimeout(timer);
    if (now) void flush();
    else timer = setTimeout(() => void flush(), PAUSE_MS);
  }

  /** Saves what waits when a field loses focus, without the pause. */
  function saveNow() {
    if (timer !== undefined && pending.length > 0) void flush();
  }

  /** The checks the server makes, so a bad field never holds up the good ones. */
  function check(k: Key, f: Form): string {
    const len = whole(f.length);
    const n = (v: unknown, lo: number, hi: number) => {
      const x = whole(v);
      return x >= lo && x <= hi;
    };
    switch (k) {
      case 'title': {
        const t = f.title.trim();
        if (t === '') return 'Enter a title.';
        return [...t].length > 120 ? 'Enter a title of at most 120 characters.' : '';
      }
      case 'starts_at':
        return fromLocalInput(f.starts_at) === null ? 'Enter the start date and time.' : '';
      case 'length':
        if (!(len >= 1)) return 'Enter the length in whole minutes.';
        return len > 14 * 24 * 60 ? 'A contest can last at most 14 days.' : '';
      case 'freeze_minutes': {
        const h = whole(f.freeze_minutes);
        if (!(h >= 0)) return 'Enter whole minutes, 0 to hide nothing.';
        const total = len >= 1 ? len : contest ? Math.round((contest.ends_at - contest.starts_at) / 60_000) : Infinity;
        return h >= total ? 'Hide fewer minutes than the contest lasts.' : '';
      }
      case 'languages':
        return f.languages.length === 0 ? 'Allow at least one language.' : '';
      case 'min_gap_s':
        return n(f.min_gap_s, 0, 3600) ? '' : 'Set a wait from 0 to 3600 seconds.';
      case 'max_per_problem':
        return n(f.max_per_problem, 1, 1000) ? '' : 'Allow from 1 to 1000 submissions per problem.';
      default:
        return '';
    }
  }

  /** The PUT body for `keys`; the start and the length always travel together. */
  function bodyFor(keys: Key[], f: Form): Partial<ContestSettings> {
    const body: Partial<ContestSettings> = {};
    for (const k of keys) {
      if (k === 'starts_at' || k === 'length') {
        const start = fromLocalInput(f.starts_at) as number;
        body.starts_at = start;
        body.ends_at = start + whole(f.length) * 60_000;
      } else if (k === 'title') body.title = f.title.trim();
      else if (k === 'freeze_minutes' || k === 'min_gap_s' || k === 'max_per_problem') body[k] = whole(f[k]);
      else if (k === 'languages') body.languages = [...f.languages];
      else body[k] = f[k] as never;
    }
    return body;
  }

  /** Sends what is pending: one save at a time, and a refused field stays marked. */
  function flush(): Promise<void> {
    clearTimeout(timer);
    timer = undefined;
    if (inflight) return inflight.then(() => flush());
    if (!contest || !form || pending.length === 0 || confirm !== null) return Promise.resolve();
    // The start and the length make the end, so they are checked and sent as one.
    let keys = [...pending];
    if (keys.includes('starts_at') !== keys.includes('length')) keys = [...keys, keys.includes('starts_at') ? 'length' : 'starts_at'];
    const local: Partial<Record<Key, string>> = {};
    for (const k of keys) {
      const why = check(k, form);
      if (why) local[k] = why;
    }
    const bad = Object.keys(local) as Key[];
    const tied = (k: Key) => (k === 'starts_at' || k === 'length') && (local.starts_at !== undefined || local.length !== undefined);
    const good = keys.filter((k) => !bad.includes(k) && !tied(k));
    if (bad.length > 0) {
      fields = { ...fields, ...local };
      saveState = 'invalid';
      if (bad.some((k) => ADVANCED.includes(k))) advancedOpen = true;
    }
    if (good.length === 0) return Promise.resolve();
    const body = bodyFor(good, form);
    const sent = Object.fromEntries(good.map((k) => [k, edits[k] ?? 0])) as Partial<Record<Key, number>>;
    const c = contest;
    const minute = (ms: number) => Math.floor(ms / 60_000);
    const liveChange =
      (body.starts_at !== undefined && (minute(body.starts_at) !== minute(c.starts_at) || minute(body.ends_at ?? 0) !== minute(c.ends_at))) ||
      (body.freeze_minutes !== undefined && body.freeze_minutes !== c.freeze_minutes);
    if (live && liveChange) {
      confirm = {
        title: 'Change a live contest?',
        body: 'Students see the change at once: a new start, length or hidden minutes can change their clocks and the board. To give more time, Extend in the Live view is safer; to stop now, use End now there.',
        label: 'Change the contest',
        danger: true,
        run: () => send(good, body, sent),
        cancel: () => revert(good.filter((k) => TIMING.includes(k))),
      };
      return Promise.resolve();
    }
    return send(good, body, sent);
  }

  /** Puts settings back as the server has them, for a live change that was not confirmed. */
  function revert(keys: Key[]) {
    if (!contest || !form) return;
    const saved = toForm(contest);
    for (const k of keys) (form as Record<Key, unknown>)[k] = saved[k];
    pending = pending.filter((k) => !keys.includes(k));
  }

  function send(keys: Key[], body: Partial<ContestSettings>, sent: Partial<Record<Key, number>>): Promise<void> {
    const run = async () => {
      saving = true;
      try {
        adopt(await sendJSON<AdminContest>('PUT', base, body));
        // A key edited again while this save ran stays pending for the next one.
        pending = pending.filter((k) => !keys.includes(k) || (edits[k] ?? 0) !== sent[k]);
        if (saveState === 'failed' || Object.keys(fields).length === 0) saveState = '';
        saveError = '';
      } catch (e) {
        if (isSignedOut(e)) return;
        const refused = e instanceof ApiError ? fieldErrors(e, keys) : {};
        if (Object.keys(refused).length > 0) {
          fields = { ...fields, ...refused };
          saveState = 'invalid';
          if (Object.keys(refused).some((k) => ADVANCED.includes(k as Key))) advancedOpen = true;
        } else {
          saveState = 'failed';
          saveError = errorText(e);
        }
      } finally {
        saving = false;
      }
    };
    inflight = run().finally(() => {
      inflight = null;
    });
    return inflight;
  }

  /** The server's field sentences on the form's fields; the end is the length's. */
  function fieldErrors(e: ApiError, keys: Key[]): Partial<Record<Key, string>> {
    const out: Partial<Record<Key, string>> = {};
    for (const [name, text] of Object.entries(e.fields)) {
      if (name === 'ends_at') out.length = text;
      else if (name === 'description_md') out[keys.includes('description_md') ? 'description_md' : 'honour_text'] = text;
      else if ((keys as string[]).includes(name)) out[name as Key] = text;
    }
    return out;
  }

  function retry() {
    saveState = '';
    void flush();
  }

  // Publishing.

  /** The picked problems whose hidden tests a contest already showed. */
  const shownLabels = $derived(picked.filter((p) => bank.find((b) => b.id === p.problem_id)?.shown_in_practice).map((p) => p.label));

  /** The publish confirm's line for those problems. */
  function shownWarning(): string[] {
    if (shownLabels.length === 0) return [];
    const which = shownLabels.length === 1 ? `Problem ${shownLabels[0]} was` : `Problems ${shownLabels.join(', ')} were`;
    return [`${which} open for practice after a contest, so students may know the hidden tests.`];
  }

  /** Publishing and unpublishing ask first and say what students will see; what waits saves first. */
  async function togglePublish() {
    if (!contest) return;
    publishError = '';
    if (pending.length > 0) await flush();
    if (!contest) return;
    if (pending.length > 0) {
      // A field the server cannot take yet: publishing waits for it.
      await focusFirstInvalid('.settings');
      return;
    }
    const title = contest.title;
    if (!contest.published) {
      confirm = {
        title: `Publish ${title}?`,
        body: 'Students from the allowed domains see it in their contest list at once.',
        items: [...checks.map((c) => c.text), ...shownWarning()],
        label: 'Publish contest',
        danger: false,
        run: () => setPublished(true, 'Published'),
      };
      return;
    }
    confirm = {
      title: `Unpublish ${title}?`,
      body: live
        ? 'The contest disappears from students at once, in the middle of it: they can no longer open the problems or submit. Submissions so far are kept.'
        : 'Students no longer see it in their list. Nothing is deleted, and you can publish it again.',
      label: 'Unpublish contest',
      danger: live,
      run: () => setPublished(false, 'Unpublished'),
    };
  }

  async function setPublished(on: boolean, done: string) {
    saving = true;
    try {
      adopt(await sendJSON<AdminContest>('PUT', base, { published: on }));
      showToast(done);
    } catch (e) {
      if (isSignedOut(e)) return;
      publishError = e instanceof ApiError ? (e.fields.published ?? e.message) : errorText(e);
      await tick();
      document.getElementById('publish-error')?.focus();
    } finally {
      saving = false;
    }
  }

  function toggleLanguage(lang: string, on: boolean) {
    if (!form) return;
    form.languages = on ? [...form.languages, lang].filter((v, i, a) => a.indexOf(v) === i) : form.languages.filter((l) => l !== lang);
    change(['languages'], true);
  }

  // Problems: every change is saved at once.
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
      const value = await sendJSON<AdminContest>(
        'PUT',
        `${base}/problems`,
        picked.map((p, i) =>
          reason !== '' && aiLabels.includes(labels[i])
            ? { problem_id: p.problem_id, label: labels[i], ai_reason: reason }
            : { problem_id: p.problem_id, label: labels[i] },
        ),
      );
      aiLabels = [];
      aiReason = '';
      adopt(value);
      showToast(done);
      return true;
    } catch (e) {
      if (isSignedOut(e)) return false;
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
      if (!isSignedOut(e)) showToast(errorText(e), 'error');
    } finally {
      confirmBusy = false;
    }
  }

  function cancelConfirm() {
    const cancel = confirm?.cancel;
    confirm = null;
    cancel?.();
  }

  function duplicated(made: AdminContest) {
    duplicating = false;
    showToast(`Duplicated ${made.title} as a draft`);
    navigate(`/admin/contests/${made.id}`);
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
        pending = [];
        navigate('/admin/contests');
      },
    };
  }

  function leave() {
    const to = leaveTo;
    leaveTo = null;
    if (to === null) return;
    clearTimeout(timer);
    pending = [];
    navigate(to);
  }

  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
  }

  /** A short reading of Advanced, so it need not be opened to be checked. */
  const advancedSummary = $derived.by(() => {
    if (!form) return '';
    const line = [
      `wait ${whole(form.min_gap_s) || 0} s between submissions`,
      countOf(whole(form.max_per_problem) || 0, 'submission') + ' per problem',
      form.practice_after_end ? 'practice after the end' : 'no practice after the end',
      ...(form.hold_reveal ? ['results hidden until you reveal them'] : []),
      ...(EXAM.some((x) => form?.[x.key]) ? [`exam options: ${EXAM.filter((x) => form?.[x.key]).map((x) => x.label.toLowerCase()).join(', ')}`] : []),
    ].join(', ');
    return line.charAt(0).toUpperCase() + line.slice(1);
  });

  /** The line under the hidden minutes: which results students will not see. */
  const hiddenHelp = $derived.by(() => {
    if (!form) return '';
    const h = whole(form.freeze_minutes);
    if (!(h > 0) || endsAt === null) return '0 hides nothing.';
    const from = new Date(endsAt - h * 60_000);
    const hhmm = `${String(from.getHours()).padStart(2, '0')}:${String(from.getMinutes()).padStart(2, '0')}`;
    return `Results from ${hhmm} are hidden on the standings until the end.`;
  });

  onMount(() => {
    setTitle(routeTitle('admin-contest'));
    if (!isOrganiser) page.forbid();
    else void load();
    const unguard = setNavigationGuard((to) => {
      if (!dirty) return true;
      leaveTo = to;
      return false;
    });
    return () => {
      unguard();
      page.stop();
      clearTimeout(timer);
    };
  });
</script>

<svelte:window onbeforeunload={onBeforeUnload} />

{#snippet err(key: Key)}
  {#if fields[key]}<p id="f-{key}-error" class="field-error">{fields[key]}</p>{/if}
{/snippet}

{#if page.state === 'forbidden'}
  <PageHeader title={routeTitle('admin-contest')} />
  <PageState kind="forbidden" message="Only organisers can manage contests." />
{:else if page.state !== 'ready' || !contest || !form}
  <PageHeader
    title={page.state === 'missing' ? 'Contest not found' : routeTitle('admin-contest')}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: page.state === 'missing' ? 'Contest not found' : routeTitle('admin-contest') }]}
  />
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if page.state === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={page.error} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} slug={contest.slug} title={contest.title} state={contest.state} published={contest.published} current="settings">
    {#snippet actions()}
      <div class="publish-row">
        <p class="publish-line"><MonoDigits text={statusLine} /></p>
        <span class="save-line" class:bad={saveState !== ''}>
          <span role="status">{saveText}</span>
          {#if saveState === 'failed'}<button type="button" class="btn btn-quiet btn-compact" onclick={retry}>Try again</button>{/if}
        </span>
        <button
          type="button"
          class="btn {contest?.published ? 'btn-secondary' : 'btn-primary'}"
          disabled={saving || (!contest?.published && blocked)}
          aria-describedby={!contest?.published && blocked ? 'publish-checks' : undefined}
          onclick={togglePublish}
        >
          {contest?.published ? 'Unpublish contest' : 'Publish contest'}
        </button>
        <ActionMenu
          id="contest-menu"
          items={[
            { label: 'Duplicate', onselect: () => (duplicating = true) },
            { label: 'Archive', onselect: archive, danger: true },
          ]}
        />
      </div>
    {/snippet}
  </ContestAdminNav>

  {#if live}
    <div class="banner banner-info live-note" role="note">
      <p>
        This contest is live. Changes here reach students at once. To give more time or to end it now, use the
        <a href="/admin/contests/{encodeURIComponent(id)}/console">Live view</a>.
      </p>
    </div>
  {:else if pastStartUnpublished}
    <div class="banner banner-info live-note" role="note">
      <p>The start time has passed. Students cannot see this contest until you publish it, so move the start or publish now.</p>
    </div>
  {/if}
  {#if saveState === 'failed' && saveError}
    <div class="banner banner-error live-note" role="alert"><p>{saveError}</p></div>
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
      </section>
    </aside>

    <div class="page-split-main editor-main">
      <form class="settings" novalidate onsubmit={(e) => e.preventDefault()} aria-label="Contest setup">
        <div class="field">
          <label for="f-title">Title</label>
          <input
            id="f-title"
            aria-required="true"
            class="input"
            type="text"
            autocomplete="off"
            bind:value={form.title}
            oninput={() => change(['title'])}
            onchange={saveNow}
            aria-invalid={fields.title ? 'true' : undefined}
            aria-describedby={fields.title ? 'f-title-error' : undefined}
          />
          {@render err('title')}
        </div>

        <section class="section" aria-labelledby="problems-title">
          <h2 id="problems-title">Problems</h2>
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

        <div class="times">
          <div class="field">
            <label for="f-starts_at">Starts</label>
            <input
              id="f-starts_at"
              aria-required="true"
              class="input"
              type="datetime-local"
              bind:value={form.starts_at}
              oninput={() => change(['starts_at'])}
              onchange={saveNow}
              aria-invalid={fields.starts_at ? 'true' : undefined}
              aria-describedby={fields.starts_at ? 'f-starts_at-error' : undefined}
            />
            {@render err('starts_at')}
          </div>
          <div class="field">
            <label for="f-length">Length in minutes</label>
            <input
              id="f-length"
              aria-required="true"
              class="input mono"
              type="number"
              min="1"
              inputmode="numeric"
              bind:value={form.length}
              oninput={() => change(['length'])}
              onchange={saveNow}
              aria-invalid={fields.length ? 'true' : undefined}
              aria-describedby="f-length-help{fields.length ? ' f-length-error' : ''}"
            />
            <p id="f-length-help" class="field-help">{#if endsAt !== null}Ends <MonoDigits text={formatDateTime(endsAt)} />.{:else}Ends after this many minutes.{/if}</p>
            {@render err('length')}
          </div>
          <div class="field">
            <label for="f-freeze_minutes">Hidden minutes at the end</label>
            <input
              id="f-freeze_minutes"
              class="input mono"
              type="number"
              min="0"
              inputmode="numeric"
              bind:value={form.freeze_minutes}
              oninput={() => change(['freeze_minutes'])}
              onchange={saveNow}
              aria-invalid={fields.freeze_minutes ? 'true' : undefined}
              aria-describedby="f-freeze-help{fields.freeze_minutes ? ' f-freeze_minutes-error' : ''}"
            />
            <p id="f-freeze-help" class="field-help"><MonoDigits text={hiddenHelp} /></p>
            {@render err('freeze_minutes')}
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

        <details id="advanced" class="advanced" bind:open={advancedOpen}>
          <summary>
            <h2 id="advanced-title" class="summary-title">Advanced <Icon name="chevron-down" /></h2>
            <span class="muted small summary-line">{advancedSummary}</span>
          </summary>
          <div class="advanced-body">
            <p class="muted small">Address <span class="mono">/contests/{contest.slug}</span>, made from the title when the contest was created. It does not change.</p>
            <div class="grid">
              <div class="field">
                <label for="f-min_gap_s">Wait between submissions, seconds</label>
                <input
                  id="f-min_gap_s"
                  class="input mono"
                  type="number"
                  min="0"
                  inputmode="numeric"
                  bind:value={form.min_gap_s}
                  oninput={() => change(['min_gap_s'])}
                  onchange={saveNow}
                  aria-invalid={fields.min_gap_s ? 'true' : undefined}
                  aria-describedby={fields.min_gap_s ? 'f-min_gap_s-error' : undefined}
                />
                {@render err('min_gap_s')}
              </div>
              <div class="field">
                <label for="f-max_per_problem">Submissions per problem</label>
                <input
                  id="f-max_per_problem"
                  class="input mono"
                  type="number"
                  min="1"
                  inputmode="numeric"
                  bind:value={form.max_per_problem}
                  oninput={() => change(['max_per_problem'])}
                  onchange={saveNow}
                  aria-invalid={fields.max_per_problem ? 'true' : undefined}
                  aria-describedby={fields.max_per_problem ? 'f-max_per_problem-error' : undefined}
                />
                {@render err('max_per_problem')}
              </div>
            </div>

            <fieldset class="group">
              <legend>After the end</legend>
              <label class="choice">
                <input
                  type="checkbox"
                  checked={form.practice_after_end}
                  onchange={(e) => {
                    if (form) form.practice_after_end = e.currentTarget.checked;
                    change(['practice_after_end'], true);
                  }}
                />
                Open the problems for practice after the end. Practice shows students the first hidden test they fail.
              </label>
              <label class="choice" class:off={hiddenOff && !form.hold_reveal}>
                <input
                  type="checkbox"
                  checked={form.hold_reveal}
                  disabled={hiddenOff && !form.hold_reveal}
                  aria-describedby="f-hold-help"
                  onchange={(e) => {
                    if (form) form.hold_reveal = e.currentTarget.checked;
                    change(['hold_reveal'], true);
                  }}
                />
                Keep the hidden results hidden after the end until you reveal them
              </label>
              <p id="f-hold-help" class="field-help hold-help" class:warn-text={hiddenOff && form.hold_reveal}>
                Needs hidden minutes at the end; with none there is nothing to reveal.
              </p>
            </fieldset>

            <fieldset class="group">
              <legend>Exam options</legend>
              <p class="field-help">All off for an ordinary contest. Students read which ones are on before they start.</p>
              {#each EXAM as x (x.key)}
                <label class="choice">
                  <input
                    type="checkbox"
                    checked={form[x.key]}
                    aria-describedby="f-{x.key}-help"
                    onchange={(e) => {
                      if (form) form[x.key] = e.currentTarget.checked;
                      change([x.key], true);
                    }}
                  />
                  {x.label}
                </label>
                <p id="f-{x.key}-help" class="field-help exam-help">{x.help}</p>
              {/each}
            </fieldset>

            <div class="field">
              <label for="f-description_md">Description (Markdown)</label>
              <textarea
                id="f-description_md"
                class="input"
                rows="3"
                bind:value={form.description_md}
                oninput={() => change(['description_md'])}
                onchange={saveNow}
                aria-invalid={fields.description_md ? 'true' : undefined}
                aria-describedby={fields.description_md ? 'f-description_md-error' : undefined}
              ></textarea>
              {@render err('description_md')}
            </div>
            <div class="field">
              <label for="f-honour_text">Honour code (Markdown)</label>
              <textarea
                id="f-honour_text"
                class="input"
                rows="4"
                placeholder={contest.default_honour_text ?? ''}
                bind:value={form.honour_text}
                oninput={() => change(['honour_text'])}
                onchange={saveNow}
                aria-invalid={fields.honour_text ? 'true' : undefined}
                aria-describedby="f-honour-help{fields.honour_text ? ' f-honour_text-error' : ''}"
              ></textarea>
              <p id="f-honour-help" class="field-help">Leave empty for the default, which lists what is recorded and for how long.</p>
              {@render err('honour_text')}
            </div>
          </div>
        </details>
      </form>
    </div>
  </div>

  <DuplicateDialog
    open={duplicating}
    {base}
    title={contest.title}
    startsAt={contest.starts_at}
    endsAt={contest.ends_at}
    onclose={() => (duplicating = false)}
    ondone={duplicated}
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
    oncancel={cancelConfirm}
  />

  <ConfirmDialog
    open={leaveTo !== null}
    title="Leave without saving?"
    body="Your last change to the setup is not saved. Problems are saved already."
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

  .save-line {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .save-line.bad {
    color: var(--bad);
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

  .settings {
    display: grid;
    gap: var(--space-5);
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

  .section {
    display: grid;
    gap: var(--space-3);
  }

  .section h2,
  .summary-title {
    font-size: var(--fs-sub);
    font-weight: 600;
    line-height: var(--lh-tight);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr));
    gap: var(--space-4);
  }

  /* Start, length and hidden minutes on one row from 768 px. */
  .times {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-4);
  }

  .advanced {
    padding-top: var(--space-4);
    border-top: var(--rule) solid var(--line);
  }

  .advanced summary {
    display: grid;
    gap: var(--space-1);
    width: fit-content;
    max-width: 100%;
    border-radius: var(--radius);
    cursor: pointer;
    list-style: none;
  }

  .advanced summary::-webkit-details-marker {
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

  .advanced[open] .summary-title :global(svg) {
    transform: rotate(180deg);
  }

  .advanced-body {
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

  .hold-help,
  .exam-help {
    margin-top: calc(-1 * var(--space-2));
    padding-left: var(--space-6);
  }

  .warn-text {
    color: var(--part);
    font-weight: 500;
  }

  .banner {
    margin-top: var(--space-5);
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

  @media (min-width: 768px) {
    .times {
      grid-template-columns: repeat(3, minmax(0, 1fr));
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
