<!--
  Workspace: read a problem, write code, run it, submit it and understand
  the verdict (design.md 5.3, cache/v1/m3-plan.md section 7). Built to be
  mounted anywhere a problem is solved: M3 mounts it for organisers at
  /admin/problems/{id}/try, M4 under a contest.

  Layout: from 1024 px three columns, a rail (the clock slot, the back
  link under it, and the problems), the statement with its samples, and
  the code column with the editor in the dark well over the result. A
  separator between the two (the WAI-ARIA window splitter: drag it, or
  arrows, Page Up and Down, Home, End, Enter to hide) sets the result's
  share, 20 to 80 percent, and "Hide result" folds it to one line that
  still names the verdict; both are kept on this device (P5 F7.5). The
  workspace is sized from the shell's measured header (--shell-head), so
  the page does not scroll; a laptop at most 45rem tall (the `short` tier,
  such as 1366 by 768 at 125 percent) gets a tighter title row, a smaller
  first result share. From 1024 to 1279 px, at any height, the rail is
  narrower and the code column wider, so the toolbar keeps one row from
  1100 px (F7.3, council C1-042; 1024 to 1099 px may wrap it). From 768 to 1023 px, Problem and Code tabs, the
  result under the editor; under 768 px, Problem, Code and Result tabs.
  Under 1024 px one strip sticks to the top of the window (F7.2, audit
  S7): the contest clock with the previous and next problem, then the
  tabs and Submit, which lives there instead of in the toolbar. While it
  sticks, the page's scroll padding leaves room for it (X3). When a tab
  switches by itself after Submit or Run, focus moves to the result's
  heading (F7.4).

  Behaviour:
  - The draft is saved to the server every 10 s and when the editor or the
    window loses focus, and mirrored to localStorage on every pause, under
    the signed-in user's own key; on load the newer of the two wins
    (lib/drafts.ts). An empty draft starts from
    the language's starter with fast input (lib/starters.ts).
  - Submit sends an idempotency key made per press and reused by "Submit
    again", so a retry after a lost answer never counts twice. Offline or
    unreachable requests say "Not sent" with "Submit again" or "Run again".
    The contest's gap between submits (`minGapS`) and a server 429 with a
    wait count down inside the button, "Submit in 24 s", aria-disabled
    with a fixed width, so nothing moves (audit 2026-10-20 S4); a refusal
    without a known wait is a muted status line beside Submit, never a red
    alert (S4, audit 01-3); so is "3 of 15 submissions used" in a contest.
    After a Run that did not compile, Submit asks once "Your last run did
    not compile. Submit anyway?" while the code is unchanged. While a submit is on its way
    the button reads "Submitting" and is aria-disabled, so focus stays.
  - A waiting submission reads "Submitted, 3rd in queue"; one that scored
    less than the problem already has adds "Your score on this problem
    stays 60 of 100" (lib/workspace.ts). A failing sample offers "Run on
    this input" (D114), which fills the custom input box, opens it and runs
    it. Run and Submit carry the play and send icons.
  - Results stream over GET /api/events (lib/stream.ts): reconnects with
    jitter, falls back to polling every 15 s after three failures, and
    refetches own submissions after every reconnect; when another tab takes
    over live updates, it stops and offers "Resume here". server_now on every
    response keeps the clock offset (lib/clock.ts).
  - Integrity counts (lib/integrity.ts): typed and pasted characters from
    the editor, window blur and focus, sent in batches; never in the way of
    a submit.
  - The code column's toolbar is one row (audit 2026-10-20 S2): the
    language, "Editor settings" (font size, "Tab moves focus" and "Reset to
    starter", components/EditorSettings) and Run and Submit at the end; a
    narrow column drops the icons to keep the row. Under the editor one
    line holds "Custom input" (a quiet button that opens its box at the top
    of the result pane), the submission count and the draft state.
  - "Reset to starter" asks first ("Reset to starter?", the danger button
    repeating the words) and saves the draft at once, so the next timed
    save or a reload cannot bring the old code back (F7.12, D114).
  - A practice submission or any run may carry its first failing test in
    full (D115); VerdictPanel and RunResults show it with "Run on this
    input". A contest submission never carries one.
  - During a live contest (a `clock` is given) sign out asks first
    (App.svelte, lib/session.svelte.ts guardSignOut, F7.6).
  - Ctrl+Enter submits and Ctrl+' runs on samples (Cmd on a Mac), listed
    under "Keyboard shortcuts" in the footer; the app's announcer
    (lib/announce.svelte.ts) speaks each submission and run result as a
    sentence, compile errors included. Sign out waits for the draft save
    (beforeSignOut).

  Props: `base` (the API base, such as "/api/workspace/12"), `backHref` and
  `backLabel` (the link above the title), `note` (one line in the clock
  slot when there is no clock, optional) and `clock` (a snippet for a
  contest clock, given true when it should be one compact line, optional).
  For contests (M4): `banner` (a snippet drawn above the title, so from
  1024 px it never moves the editor), `rail` (a snippet given the
  layout, drawn in place of the single-problem rail item, and under the
  title below 1024 px), `label` (the problem letter, shown before the
  title) and `context` (the contest title, for the document title),
  `feedback` ("exam" shows only "Submitted" for each submission),
  `showScores` (exam mode after the end: scores are shown), `locked` (a
  sentence; Run and Submit are off and it says why, unless the workspace's
  own `refusal` from the server says it better), `problemScore` ({ score,
  max } of the problem so far, for "your score stays"), `used` ({ count,
  max } submissions, for "3 of 15 submissions used"), `minGapS` (the
  contest's seconds between submits, counted down in the button), `statementKey` (a
  number the contest page raises when an organiser moved this problem to
  another revision: the statement, title, limits and samples reload, the
  editor is left alone), and `extraEvents` with
  `onevent` (further event names to listen for on the same stream; every
  event, submissions included, is passed to `onevent`).
-->
<script lang="ts">
  import PageHeader from './PageHeader.svelte';
  import { media } from '../lib/breakpoints';
  import PageState from './PageState.svelte';
  import { onMount, tick, type Snippet } from 'svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import {
    DEFAULT_FONT_SIZE,
    FONT_SIZES,
    RESULT_SHARE,
    clampShare,
    draftStorage,
    mayMirror,
    mirrorKey,
    pickDraft,
    readEditorPrefs,
    readMirror,
    readResultPanel,
    writeEditorPrefs,
    writeMirror,
    writeResultPanel,
  } from '../lib/drafts';
  import { polite } from '../lib/announce.svelte';
  import { keepHeadingFocus } from '../lib/focus';
  import { publishHeight } from '../lib/publishHeight';
  import { setTitle } from '../lib/head';
  import { FLUSH_MS, IntegrityRecorder } from '../lib/integrity';
  import { beforeSignOut, guardSignOut, session } from '../lib/session.svelte';
  import { isStarterOrEmpty, starter } from '../lib/starters';
  import { EventStream, type StreamMode } from '../lib/stream';
  import { mergeRun, mergeSubmission, mergeSubmissions, reduceEvent, type JudgingState, type StreamEvent } from '../lib/submissionState';
  import {
    LANGUAGES,
    announcement,
    bestScore,
    firstFailure,
    formatWhen,
    headline,
    isWaiting,
    languageLabel,
    limitsText,
    newKey,
    runAnnouncement,
    runCompileFailed,
    runHeadline,
    usedText,
    workspacePaths,
    type Language,
    type Run,
    type Submission,
    type Workspace,
  } from '../lib/workspace';
  import CodeEditor from './CodeEditor.svelte';
  import Icon from './Icon.svelte';
  import RunResults from './RunResults.svelte';
  import Statement from './Statement.svelte';
  import StreamNotice from './StreamNotice.svelte';
  import SubmissionList from './SubmissionList.svelte';
  import VerdictPanel from './VerdictPanel.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import EditorSettings from './EditorSettings.svelte';

  interface Props {
    base: string;
    backHref: string;
    backLabel: string;
    note?: string;
    clock?: Snippet<[boolean]>;
    banner?: Snippet;
    rail?: Snippet<[string]>;
    label?: string;
    context?: string;
    feedback?: 'subtask' | 'exam';
    showScores?: boolean;
    locked?: string;
    problemScore?: { score: number | null; max: number } | null;
    used?: { count: number; max: number } | null;
    minGapS?: number;
    statementKey?: number;
    extraEvents?: readonly string[];
    onevent?: (type: string, data: unknown) => void;
  }

  let {
    base,
    backHref,
    backLabel,
    note = '',
    clock,
    banner,
    rail,
    label = '',
    context = '',
    feedback = 'subtask',
    showScores = false,
    locked = '',
    problemScore = null,
    used = null,
    minGapS = 0,
    statementKey = 0,
    extraEvents = [],
    onevent,
  }: Props = $props();

  const exam = $derived(feedback === 'exam');

  /** How often the draft is saved while it changes. */
  const SAVE_MS = 10_000;

  const paths = $derived(workspacePaths(base));
  const storage = draftStorage();
  // The mirror is per account (lib/drafts.ts); the shell mounts this only when signed in.
  const owner = session.user?.id ?? 0;
  const prefs = readEditorPrefs(storage);
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

  let loadState = $state<'loading' | 'ready' | 'missing' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let ws = $state.raw<Workspace | null>(null);

  // The code.
  let language = $state<Language>(prefs.language);
  let source = $state('');
  let initialDoc = $state('');
  let savedLanguage = '';
  let savedSource = '';
  let saveState = $state<'saved' | 'saving' | 'local' | 'failed'>('saved');
  let fontSize = $state(prefs.fontSize);
  let tabMovesFocus = $state(prefs.tabMovesFocus);
  let editor: CodeEditor | undefined = $state();

  // Judging.
  let judging = $state.raw<JudgingState>({ submissions: [], run: null, serverNow: null });
  let view = $state<'submission' | 'run'>('submission');
  let watchId = $state<number | null>(null);
  let announced = '';
  let announcedRun = '';
  let submitBusy = $state(false);
  let runBusy = $state(false);
  let pendingKey: string | null = null;
  let pendingRun: { language: Language; source: string; input?: string } | null = null;
  /** The code and language of the last run, so a compile error is only warned about while the code is unchanged (S4). */
  let lastRunBody = $state.raw<{ language: Language; source: string } | null>(null);
  let compileWarned = $state(false);
  let confirmCompile = $state(false);
  /** A refusal or failure: "pace" is a normal pacing rule (a muted status line), "error" a real failure. */
  let notice = $state<{ action: 'submit' | 'run'; text: string; retry: boolean; kind: 'pace' | 'error' } | null>(null);
  let waitUntil = $state(0);
  let nowTick = $state(Date.now());
  let streamMode = $state<StreamMode>('connecting');

  // Custom input.
  let customOpen = $state(false);
  let customInput = $state('');
  let customField: HTMLTextAreaElement | undefined = $state();

  // Layout.
  let layout = $state<'wide' | 'tablet' | 'phone'>('wide');
  let tab = $state<'problem' | 'code' | 'result'>('problem');

  // The result panel under the editor from 1024 px (F7.5, D87): its share of
  // the code column and whether it is hidden, kept on this device.
  // A short laptop starts with a smaller result, so the editor keeps its height (F7.3).
  const resultPrefs = readResultPanel(storage, typeof window !== 'undefined' && media('short').matches ? 30 : RESULT_SHARE.default);
  let resultShare = $state(resultPrefs.share);
  let resultCollapsed = $state(resultPrefs.collapsed);
  let codeCol: HTMLDivElement | undefined = $state();
  let dragging = false;

  // F7.12: "Reset to starter" asks first.
  let confirmReset = $state(false);

  // The contest may allow fewer languages than the judge knows.
  const languages = $derived.by(() => {
    const ids = (ws?.languages ?? []).map((l) => l.id);
    return ids.length > 0 ? LANGUAGES.filter((l) => ids.includes(l.id)) : [...LANGUAGES];
  });

  // The server says why Run and Submit are off when they are (its sentence is
  // the precise one, such as practice opening later); `locked` is the fallback.
  const lockText = $derived(ws?.refusal ? ws.refusal : locked);

  const latest = $derived(judging.submissions[0] ?? null);
  const current = $derived(
    watchId !== null ? (judging.submissions.find((s) => s.id === watchId) ?? latest) : latest,
  );
  const best = $derived(bestScore(judging.submissions));
  const waitLeft = $derived(Math.max(0, Math.ceil((waitUntil - nowTick) / 1000)));
  // The contest counts submissions as they are made; judge failures do not count (docs/API.md).
  const usedNow = $derived(
    used ? { count: Math.max(used.count, judging.submissions.filter((x) => x.state !== 'error').length), max: used.max } : null,
  );
  // S4: a known wait counts down inside the Submit button; only a refusal
  // without a known wait takes the status line.
  const paceText = $derived(notice?.kind === 'pace' && waitLeft === 0 && waitUntil === 0 ? notice.text : '');
  const submitLabel = $derived(submitBusy ? 'Submitting' : waitLeft > 0 ? `Submit in ${waitLeft} s` : 'Submit');
  // The last run did not compile and the code has not changed since.
  const runDidNotCompile = $derived(
    judging.run !== null &&
      lastRunBody !== null &&
      runCompileFailed(judging.run) &&
      lastRunBody.source === source &&
      lastRunBody.language === language,
  );
  // A pacing refusal is said once, politely; the line itself only counts down.
  $effect(() => {
    if (notice?.kind === 'pace') polite(notice.text);
  });
  // A wait that ran out takes its refusal with it.
  $effect(() => {
    if (notice?.kind === 'pace' && waitUntil > 0 && waitLeft === 0) notice = null;
  });
  const editorLabel = $derived(`Code for ${ws?.title ?? 'this problem'}, ${languageLabel(language)}`);
  const sampleFailure = $derived(
    current && current.state === 'done' ? (firstFailure(current)?.sample ?? false) : false,
  );

  const tabs = $derived(
    layout === 'phone'
      ? ([
          { id: 'problem', label: 'Problem' },
          { id: 'code', label: 'Code' },
          { id: 'result', label: 'Result' },
        ] as const)
      : ([
          { id: 'problem', label: 'Problem' },
          { id: 'code', label: 'Code' },
        ] as const),
  );
  // The one line the hidden result panel keeps in its bar.
  const resultSummary = $derived.by(() => {
    if (view === 'run' && judging.run) return runHeadline(judging.run).text;
    if (!current) return '';
    return exam || current.hidden ? 'Submitted' : headline(current).text;
  });
  const codeRows = $derived(
    layout !== 'wide'
      ? undefined
      : resultCollapsed
        ? 'minmax(0, 1fr) auto 0'
        : `minmax(0, ${100 - resultShare}fr) auto minmax(0, ${resultShare}fr)`,
  );

  const showStatement = $derived(layout === 'wide' || tab === 'problem');
  const showEditor = $derived(layout === 'wide' || tab === 'code');
  const showResult = $derived(
    layout === 'wide' ? !resultCollapsed : layout === 'tablet' ? tab === 'code' : tab === 'result',
  );

  let integrity: IntegrityRecorder | null = null;
  let stream: EventStream | null = null;
  let alive = true;

  /** Handles errors every request can hit; returns true when it did. */
  function handleSessionError(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  /** Runs a request and feeds its server_now to the clock. */
  async function timed<T extends { server_now?: number }>(fn: () => Promise<T>): Promise<T> {
    const sent = Date.now();
    const value = await fn();
    if (value && typeof value.server_now === 'number') serverClock.observe(value.server_now, sent, Date.now());
    return value;
  }

  async function load() {
    loadState = 'loading';
    let data: Workspace;
    try {
      data = await timed(() => getJSON<Workspace>(paths.load));
    } catch (e) {
      if (handleSessionError(e)) return;
      if (e instanceof ApiError && e.status === 403) {
        loadState = 'forbidden';
        return;
      }
      if (e instanceof ApiError && (e.status === 404 || e.status === 400)) {
        loadState = 'missing';
        setTitle('Problem not found');
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
      return;
    }
    if (!alive) return;
    ws = data;
    if (label !== '') setTitle(`${label}. ${data.title}`, context);
    else setTitle(`Try ${data.title}`);

    const allowed = (data.languages ?? []).map((l) => l.id);
    if (allowed.length > 0 && !allowed.includes(language)) language = allowed[0];
    const picked = pickDraft(data.draft, readMirror(storage, mirrorKey(owner, base), owner));
    if (picked.draft && (allowed.length === 0 || allowed.includes(picked.draft.language))) {
      language = picked.draft.language;
      source = picked.draft.source;
    } else if (picked.draft) {
      source = picked.draft.source;
    } else {
      source = starter(language);
    }
    initialDoc = source;
    if (picked.origin === 'server' && data.draft) {
      savedLanguage = data.draft.language;
      savedSource = data.draft.source;
      saveState = 'saved';
    } else {
      // The local copy is newer, or there is nothing on the server yet.
      savedLanguage = '';
      savedSource = '';
      saveState = picked.origin === 'local' ? 'local' : 'saved';
      if (picked.origin === 'local') saveDraft();
    }
    judging = { ...judging, submissions: mergeSubmissions([], data.submissions ?? []), serverNow: data.server_now };
    const restore = keepHeadingFocus();
    loadState = 'ready';
    void restore();
    startStream();
  }

  function startStream() {
    stream?.stop();
    stream = new EventStream({
      url: '/api/events',
      events: ['submission', 'run', 'clock', ...extraEvents],
      onEvent: (type, data) => {
        if (type === 'submission' || type === 'run' || type === 'clock') onStreamEvent({ type, data } as StreamEvent);
        onevent?.(type, data);
      },
      onReconnect: refetchOwn,
      poll: pollPending,
      onMode: (m) => (streamMode = m),
    });
    stream.start();
  }

  function onStreamEvent(ev: StreamEvent) {
    if (!ws) return;
    if (ev.type === 'clock' && typeof ev.data?.server_now === 'number') {
      const now = Date.now();
      serverClock.observe(ev.data.server_now, now, now);
    }
    judging = reduceEvent(judging, ev, ws.problem_id);
  }

  async function refetchOwn() {
    try {
      const data = await timed(() => getJSON<Workspace>(paths.load));
      if (!alive) return;
      judging = { ...judging, submissions: mergeSubmissions(judging.submissions, data.submissions ?? []) };
    } catch (e) {
      handleSessionError(e);
    }
    await pollPending();
  }

  async function pollPending() {
    const waiting = judging.submissions.filter(isWaiting);
    for (const s of waiting) {
      try {
        const fresh = await timed(() => getJSON<Submission>(`/api/submissions/${s.id}`));
        if (alive) judging = { ...judging, submissions: mergeSubmission(judging.submissions, fresh) };
      } catch (e) {
        if (handleSessionError(e)) return;
      }
    }
    const run = judging.run;
    if (run && isWaiting(run)) {
      try {
        const fresh = await timed(() => getJSON<Run>(`/api/runs/${run.id}`));
        if (alive) judging = { ...judging, run: mergeRun(judging.run, fresh) };
      } catch (e) {
        handleSessionError(e);
      }
    }
  }

  // Announces the watched submission each time its state or verdict changes.
  $effect(() => {
    const s = watchId !== null ? judging.submissions.find((x) => x.id === watchId) : undefined;
    if (!s) return;
    // In exam mode only the submit itself is announced.
    const text = exam || s.hidden ? 'Submitted.' : announcement(s);
    // Keyed by submission, so a second "Submitted." is spoken too.
    if (`${s.id}:${text}` !== announced) {
      announced = `${s.id}:${text}`;
      polite(text);
    }
  });

  // Announces the latest run as it starts and finishes (X1), compile errors included.
  $effect(() => {
    const r = judging.run;
    if (!r) return;
    const text = runAnnouncement(r);
    if (text !== '' && `${r.id}:${text}` !== announcedRun) {
      announcedRun = `${r.id}:${text}`;
      polite(text);
    }
  });

  // A new pinned revision (C3): reload what the student reads, never the editor.
  let seenStatementKey: number | null = null;
  $effect(() => {
    const key = statementKey;
    const first = seenStatementKey === null;
    if (key === seenStatementKey) return;
    seenStatementKey = key;
    if (!first) void reloadStatement();
  });

  async function reloadStatement() {
    try {
      const data = await timed(() => getJSON<Workspace>(paths.load));
      if (!alive || !ws) return;
      ws = { ...ws, title: data.title, statement_md: data.statement_md, samples: data.samples, time_limit_ms: data.time_limit_ms, memory_limit_mb: data.memory_limit_mb };
    } catch (e) {
      handleSessionError(e);
    }
  }

  // Draft saving.
  let mirrorTimer: ReturnType<typeof setTimeout> | undefined;

  function onSourceChange(next: string) {
    source = next;
    queueMirror();
  }

  function queueMirror() {
    if (saveState === 'saved' && (source !== savedSource || language !== savedLanguage)) saveState = 'local';
    clearTimeout(mirrorTimer);
    mirrorTimer = setTimeout(() => {
      mirror({ language, source, updated_at: serverClock.serverNow() });
    }, 400);
  }

  async function saveDraft(): Promise<void> {
    if (loadState !== 'ready' && saveState !== 'local') return;
    if (source === savedSource && language === savedLanguage) return;
    const body = { language, source };
    mirror({ ...body, updated_at: serverClock.serverNow() });
    saveState = 'saving';
    try {
      await sendJSON<void>('PUT', paths.draft, body);
      savedLanguage = body.language;
      savedSource = body.source;
      saveState = source === savedSource && language === savedLanguage ? 'saved' : 'local';
    } catch (e) {
      if (handleSessionError(e)) return;
      // Saved too soon after the last save: the copy here is kept and the next save sends it.
      saveState = e instanceof ApiError && e.status === 429 ? 'local' : 'failed';
    }
  }

  /** The last chance to save when the page goes away; not awaited. */
  /**
   * Mirrors the draft on this device, only while its owner is still the
   * signed-in user: at sign-out the shell unmounts this after the mirrors
   * were removed, and nothing may be written back for the next person.
   */
  function mirror(draft: { language: Language; source: string; updated_at: number }) {
    if (!mayMirror(owner)) return;
    writeMirror(storage, mirrorKey(owner, base), draft, owner);
  }

  function saveOnLeave() {
    if (source === savedSource && language === savedLanguage) return;
    if (!mayMirror(owner)) return;
    mirror({ language, source, updated_at: serverClock.serverNow() });
    try {
      void fetch(paths.draft, {
        method: 'PUT',
        keepalive: true,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ language, source }),
      }).catch(() => undefined);
    } catch {
      // The local mirror still has it.
    }
  }

  function savePrefs() {
    writeEditorPrefs(storage, { fontSize, tabMovesFocus, language });
  }

  function onLanguageChange(event: Event) {
    const next = (event.currentTarget as HTMLSelectElement).value as Language;
    const wasStarter = isStarterOrEmpty(source);
    language = next;
    savePrefs();
    if (wasStarter) {
      const code = starter(next);
      source = code;
      editor?.setDoc(code);
    }
    queueMirror();
  }

  function onFontChange(event: Event) {
    fontSize = Number((event.currentTarget as HTMLSelectElement).value) || DEFAULT_FONT_SIZE;
    savePrefs();
  }

  function onTabToggle(event: Event) {
    tabMovesFocus = (event.currentTarget as HTMLInputElement).checked;
    savePrefs();
  }

  // Submitting and running.
  function offlineText(): string {
    return typeof navigator !== 'undefined' && navigator.onLine === false
      ? 'Not sent: you are offline. Your code is kept on this device. Retry when you are back online.'
      : 'Not sent: the server could not be reached. Your code is kept on this device. Retry in a moment.';
  }

  /**
   * Shows the result pane when another tab hides it. When the tab switched
   * by itself, focus follows to the result's heading (or to the failure
   * notice), so a keyboard or screen reader user lands on what changed
   * (F7.4). From 1024 px nothing switches and focus stays on the button.
   */
  async function revealResult() {
    let switched = false;
    if (layout === 'phone' && tab !== 'result') {
      tab = 'result';
      switched = true;
    } else if (layout === 'tablet' && tab === 'problem') {
      tab = 'code';
      switched = true;
    }
    if (!switched) return;
    await tick();
    document.querySelector<HTMLElement>('#ws-result [data-result-focus]')?.focus();
  }

  async function submit(retryKey?: string, confirmed = false) {
    if (submitBusy || loadState !== 'ready' || lockText !== '') return;
    if (waitLeft > 0 && retryKey === undefined) {
      polite(`You can submit again in ${waitLeft} s.`);
      return;
    }
    // A run that did not compile asks once before a submit uses up an attempt (S4).
    if (retryKey === undefined && !confirmed && runDidNotCompile && !compileWarned) {
      confirmCompile = true;
      return;
    }
    const key = retryKey ?? newKey();
    pendingKey = key;
    notice = null;
    submitBusy = true;
    // Integrity events go first, on their own; they never hold up the submit.
    integrity?.flush();
    const counts = integrity?.counts() ?? { typed_chars: 0, pasted_chars: 0, focus_losses: 0 };
    const body = { language, source, ...counts, client_key: key };
    try {
      const s = await timed(() => sendJSON<Submission>('POST', paths.submit, body));
      if (!alive) return;
      pendingKey = null;
      integrity?.resetCounts();
      judging = { ...judging, submissions: mergeSubmission(judging.submissions, s) };
      watchId = s.id;
      // The contest's gap between submits counts down in the button.
      if (minGapS > 0) {
        waitUntil = Date.now() + minGapS * 1000;
        nowTick = Date.now();
      }
      view = 'submission';
      void saveDraft();
      await revealResult();
    } catch (e) {
      if (handleSessionError(e)) return;
      if (e instanceof ApiError && e.code === 'network') {
        notice = { action: 'submit', text: offlineText(), retry: true, kind: 'error' };
      } else {
        pendingKey = null;
        const pace = e instanceof ApiError && e.status === 429;
        if (pace && e.retryAfter !== null) {
          waitUntil = Date.now() + e.retryAfter * 1000;
          nowTick = Date.now();
        }
        notice = { action: 'submit', text: errorText(e), retry: false, kind: pace ? 'pace' : 'error' };
      }
      // A pacing line belongs beside Submit, and the announcer says it; only a failure opens the result.
      if (notice?.kind === 'error') await revealResult();
    } finally {
      submitBusy = false;
    }
  }

  async function startRun(body: { language: Language; source: string; input?: string }) {
    if (runBusy || loadState !== 'ready' || lockText !== '') return;
    pendingRun = body;
    lastRunBody = { language: body.language, source: body.source };
    compileWarned = false;
    notice = null;
    runBusy = true;
    try {
      const r = await timed(() => sendJSON<Run>('POST', paths.run, body));
      if (!alive) return;
      pendingRun = null;
      judging = { ...judging, run: r };
      view = 'run';
      await revealResult();
    } catch (e) {
      if (handleSessionError(e)) return;
      if (e instanceof ApiError && e.code === 'network') {
        notice = { action: 'run', text: offlineText(), retry: true, kind: 'error' };
      } else {
        pendingRun = null;
        notice = { action: 'run', text: errorText(e), retry: false, kind: e instanceof ApiError && e.status === 429 ? 'pace' : 'error' };
      }
      if (notice?.kind === 'error') await revealResult();
    } finally {
      runBusy = false;
    }
  }

  function runSamples() {
    void startRun({ language, source });
  }

  function runCustom() {
    void startRun({ language, source, input: customInput });
  }

  /** "Run on this input" on a failing sample: the input goes in the custom box, which opens and runs it. */
  async function runOnInput(input: string) {
    customInput = input;
    customOpen = true;
    if (layout === 'phone') tab = 'result';
    else if (layout === 'tablet') tab = 'code';
    await tick();
    runCustom();
  }

  /** "Custom input" opens its box at the top of the result pane (S2) and puts the cursor in it. */
  async function toggleCustom() {
    customOpen = !customOpen;
    if (!customOpen) return;
    if (layout === 'phone') tab = 'result';
    await tick();
    customField?.focus();
  }

  /** F7.12: the starter replaces the code and is saved at once, so the next timed save cannot bring the old code back. */
  async function resetToStarter() {
    confirmReset = false;
    const code = starter(language);
    source = code;
    editor?.setDoc(code);
    await saveDraft();
    polite('Reset to the starter.');
    editor?.focus();
  }

  // The separator between the editor and the result (F7.5): the WAI-ARIA
  // window splitter. Arrows move it by 5 percent, Page Up and Page Down by
  // 15, Home and End go to the ends, Enter hides or shows the result; a
  // step down from the smallest size hides it too.
  function saveResultPanel() {
    writeResultPanel(storage, { share: resultShare, collapsed: resultCollapsed });
  }

  function setResultShare(next: number) {
    resultShare = clampShare(next);
    resultCollapsed = false;
    saveResultPanel();
  }

  function toggleResult() {
    resultCollapsed = !resultCollapsed;
    saveResultPanel();
  }

  function onSplitKey(event: KeyboardEvent) {
    const steps: Record<string, number> = { ArrowUp: 5, ArrowDown: -5, PageUp: 15, PageDown: -15 };
    const step = steps[event.key] ?? 0;
    if (step !== 0) {
      event.preventDefault();
      if (resultCollapsed) {
        if (step > 0) setResultShare(RESULT_SHARE.min);
      } else if (step < 0 && resultShare <= RESULT_SHARE.min) {
        toggleResult();
      } else {
        setResultShare(resultShare + step);
      }
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault();
      setResultShare(event.key === 'Home' ? RESULT_SHARE.min : RESULT_SHARE.max);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      toggleResult();
    }
  }

  function onSplitDown(event: PointerEvent) {
    if (event.button !== 0 || !codeCol) return;
    dragging = true;
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function onSplitMove(event: PointerEvent) {
    if (!dragging || !codeCol) return;
    const box = codeCol.getBoundingClientRect();
    const share = ((box.bottom - event.clientY) / box.height) * 100;
    // Dragged well past the smallest size: hidden, as the step down from the smallest is.
    if (share < RESULT_SHARE.min / 2) {
      resultCollapsed = true;
    } else {
      resultCollapsed = false;
      resultShare = clampShare(share);
    }
  }

  function onSplitUp() {
    if (!dragging) return;
    dragging = false;
    saveResultPanel();
  }

  function retry() {
    if (!notice) return;
    if (notice.action === 'submit' && pendingKey) void submit(pendingKey);
    else if (notice.action === 'run' && pendingRun) void startRun(pendingRun);
  }

  async function jump(line: number, col: number | null) {
    if (layout !== 'wide') {
      tab = 'code';
      await tick();
    }
    editor?.jumpTo(line, col);
  }

  // Keyboard: Ctrl or Cmd with Enter submits, with ' runs, with S saves.
  function onKeydown(event: KeyboardEvent) {
    if (event.defaultPrevented || loadState !== 'ready') return;
    const modKey = isMac ? event.metaKey : event.ctrlKey;
    if (!modKey || event.altKey) return;
    if (event.key === 'Enter') {
      event.preventDefault();
      void submit();
    } else if (event.key === "'") {
      event.preventDefault();
      if (customOpen && document.activeElement === customField) runCustom();
      else runSamples();
    } else if (event.key === 's' || event.key === 'S') {
      event.preventDefault();
      void saveDraft();
    }
  }

  function onWindowBlur() {
    integrity?.blur();
    void saveDraft();
  }

  function onWindowFocus() {
    integrity?.focus();
  }

  function onVisibility() {
    if (document.visibilityState === 'hidden') {
      integrity?.flush();
      void saveDraft();
    }
  }

  function onPageHide() {
    integrity?.flush(true);
    saveOnLeave();
  }

  // Tabs follow the ARIA pattern: arrows move and choose.
  function onTabKey(event: KeyboardEvent) {
    const ids = tabs.map((t) => t.id);
    const i = ids.indexOf(tab as (typeof ids)[number]);
    let next = -1;
    if (event.key === 'ArrowRight') next = (i + 1) % ids.length;
    else if (event.key === 'ArrowLeft') next = (i - 1 + ids.length) % ids.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = ids.length - 1;
    if (next < 0) return;
    event.preventDefault();
    tab = ids[next];
    tick().then(() => document.getElementById(`ws-tab-${ids[next]}`)?.focus());
  }

  // F7.6: during a live contest, sign out asks first (App.svelte); the draft save still runs before it.
  $effect(() => {
    if (!clock) return;
    return guardSignOut('Your draft is saved first. Your time keeps running while you are signed out.');
  });

  onMount(() => {
    const wide = media('laptop');
    const tablet = media('tablet');
    const place = () => {
      layout = wide.matches ? 'wide' : tablet.matches ? 'tablet' : 'phone';
      if (layout === 'tablet' && tab === 'result') tab = 'code';
    };
    place();
    wide.addEventListener('change', place);
    tablet.addEventListener('change', place);

    integrity = new IntegrityRecorder(paths.events, () => serverClock.serverNow());
    const flushTimer = setInterval(() => integrity?.flush(), FLUSH_MS);
    const saveTimer = setInterval(() => void saveDraft(), SAVE_MS);
    const secondTimer = setInterval(() => {
      if (waitUntil > Date.now() - 1000) nowTick = Date.now();
    }, 1000);

    load();
    const forgetSave = beforeSignOut(saveDraft);

    return () => {
      forgetSave();
      alive = false;
      wide.removeEventListener('change', place);
      tablet.removeEventListener('change', place);
      clearInterval(flushTimer);
      clearInterval(saveTimer);
      clearInterval(secondTimer);
      clearTimeout(mirrorTimer);
      stream?.stop();
      integrity?.flush(true);
      integrity?.stop();
      saveOnLeave();
    };
  });
</script>

<svelte:window onkeydown={onKeydown} onblur={onWindowBlur} onfocus={onWindowFocus} onpagehide={onPageHide} />
<svelte:document onvisibilitychange={onVisibility} />

{#if loadState !== 'ready' || !ws}
  <div class="state">
    <PageHeader compact title={loadState === 'missing' ? 'Problem not found' : 'Problem'} back={{ href: backHref, label: backLabel }} />
    {#if loadState === 'loading'}
      <PageState kind="loading" message="Loading the problem" />
    {:else if loadState === 'forbidden'}
      <PageState kind="forbidden" message="You cannot open this problem." ask="Ask an organiser if you think you should." />
    {:else if loadState === 'missing'}
      <PageState kind="missing" message="This problem does not exist." parent={{ href: backHref, label: 'Go back.' }} />
    {:else if loadState === 'error'}
      <PageState kind="error" message={loadError} onretry={load} />
    {/if}
  </div>
{:else}
  <div class="ws" data-layout={layout}>
    <p class="visually-hidden" aria-live="polite">{saveState === 'failed' ? 'Draft not saved to the server. A copy is kept on this device.' : ''}</p>
    {#if layout === 'wide'}
      <aside class="rail" aria-label="Problem">
        <div class="rail-head">
          <div class="clock-slot">
            {#if clock}{@render clock(false)}{:else if note}<p class="note">{note}</p>{/if}
          </div>
          <!-- S6: the back link sits in the rail head, under the clock, so the title row is the title only. -->
          <a class="back" href={backHref}>{backLabel}</a>
        </div>
        {#if rail}
          {@render rail(layout)}
        {:else}
          <div class="rail-problem">
            <span class="rail-title">{ws.title}</span>
            <span class="mono rail-score">{best ? `${best.score} of ${best.max}` : 'Not submitted'}</span>
            <span class="bar" aria-hidden="true">
              <span
                class="bar-fill"
                class:part={best !== null && best.score > 0 && best.score < best.max}
                style:width="{best && best.max > 0 ? Math.round((best.score / best.max) * 100) : 0}%"
              ></span>
            </span>
          </div>
        {/if}
      </aside>
    {/if}

    <div class="title-area">
      {#if banner}<div class="banner-slot">{@render banner()}</div>{/if}
      <div class="title-main">
        <PageHeader compact back={layout === 'wide' ? null : { href: backHref, label: backLabel }}>
          {#snippet heading()}{#if label}<span class="mono letter">{label}</span><span class="visually-hidden">.&nbsp;</span>{/if}{ws?.title ?? ''}{/snippet}
        </PageHeader>
        {#if layout === 'wide'}<p class="limits">{limitsText(ws.time_limit_ms, ws.memory_limit_mb)}</p>{/if}
      </div>
      {#if layout !== 'wide' && !clock && note}
        <div class="clock-slot clock-inline"><p class="note">{note}</p></div>
      {/if}
      {#if layout !== 'wide' && rail}
        <div class="rail-inline">{@render rail(layout)}</div>
      {/if}
    </div>

    {#if layout !== 'wide'}
      <!-- F7.2 (audit S7): under 1024 px one strip sticks to the top of the window: the clock and
           the previous and next problem, then the tabs and Submit, so Submit is never a tab away.
           Its height sets the page's scroll padding while it sticks (X3, base.css). -->
      <div class="strip" data-sticky-bar use:publishHeight={'--sticky-bar-h'}>
        {#if clock || rail}
          <div class="strip-top">
            {#if clock}<div class="strip-clock">{@render clock(true)}</div>{/if}
            {#if rail}{@render rail('strip')}{/if}
          </div>
        {/if}
        <div class="strip-tabs">
          <div class="tabs" role="tablist" aria-label="Workspace">
            {#each tabs as t (t.id)}
              <button
                type="button"
                role="tab"
                id="ws-tab-{t.id}"
                class="tab"
                aria-selected={tab === t.id}
                aria-controls={t.id === 'problem' ? 'ws-problem' : t.id === 'code' && layout === 'tablet' ? 'ws-code ws-result' : `ws-${t.id}`}
                tabindex={tab === t.id ? 0 : -1}
                onclick={() => (tab = t.id)}
                onkeydown={onTabKey}
              >
                {t.label}{#if t.id === 'result' && current && isWaiting(current)}<span class="visually-hidden">, judging</span><span
                    class="dot"
                    aria-hidden="true"
                  ></span>{/if}
              </button>
            {/each}
          </div>
          {@render submitButton()}
        </div>
      </div>
    {/if}

    <section
      id="ws-problem"
      class="pane statement-pane"
      hidden={!showStatement}
      role={layout === 'wide' ? undefined : 'tabpanel'}
      aria-labelledby={layout === 'wide' ? undefined : 'ws-tab-problem'}
      aria-label={layout === 'wide' ? 'Statement' : undefined}
    >
      <!-- Under 1024 px the limits open the Problem tab, so the statement starts higher (audit S7). -->
      {#if layout !== 'wide'}<p class="limits limits-tab">{limitsText(ws.time_limit_ms, ws.memory_limit_mb)}</p>{/if}
      <Statement markdown={ws.statement_md} samples={ws.samples} />
    </section>

    <div class="code-col" bind:this={codeCol} style:grid-template-rows={codeRows}>
      <section
        id="ws-code"
        class="pane editor-pane"
        hidden={!showEditor}
        role={layout === 'wide' ? undefined : 'tabpanel'}
        aria-labelledby={layout === 'wide' ? undefined : 'ws-tab-code'}
        aria-label={layout === 'wide' ? 'Code' : undefined}
      >
        <!-- S2: one row. Language, the editor settings, then Run and, from 1024 px, Submit at the end. -->
        <div class="toolbar">
          <label class="visually-hidden" for="ws-language">Language</label>
          <select id="ws-language" class="select lang" value={language} onchange={onLanguageChange}>
            {#each languages as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
          </select>
          <EditorSettings
            {fontSize}
            sizes={FONT_SIZES}
            {tabMovesFocus}
            onfont={onFontChange}
            ontab={onTabToggle}
            onreset={() => (confirmReset = true)}
          />
          <div class="run-actions">
            <button
              type="button"
              class="btn btn-secondary btn-compact"
              disabled={lockText !== ''}
              aria-disabled={runBusy ? 'true' : undefined}
              aria-describedby={lockText ? 'ws-locked' : undefined}
              onclick={runSamples}><Icon name="play" />Run on samples</button
            >
            {#if layout === 'wide'}{@render submitButton()}{/if}
          </div>
        </div>
        {#if lockText}<p class="locked" id="ws-locked">{lockText}</p>{/if}

        <div class="editor-box">
          <CodeEditor
            bind:this={editor}
            initial={initialDoc}
            {language}
            label={editorLabel}
            {fontSize}
            {tabMovesFocus}
            onchange={onSourceChange}
            ontyped={(n) => integrity?.typedChars(n)}
            onpaste={(n) => integrity?.paste(n)}
            onblur={() => void saveDraft()}
            onsubmit={() => void submit()}
            onrun={runSamples}
          />
        </div>

        <div class="under">
          <button
            type="button"
            class="btn btn-link btn-compact custom-toggle"
            aria-expanded={customOpen}
            aria-controls="ws-custom-panel"
            onclick={toggleCustom}>Custom input</button
          >
          <!-- Pacing and the submission count: muted, never an alert (S4). The announcer speaks the pacing line. -->
          {#if paceText || (usedNow && !lockText)}
            <p class="submit-status" id="ws-submit-status">
              {#if paceText}<span>{paceText}</span>{/if}
              {#if usedNow && !lockText}<span class="used">{usedText(usedNow.count, usedNow.max)}</span>{/if}
            </p>
          {/if}
          <p class="save" class:failed={saveState === 'failed'}>
            {#if saveState === 'saved'}Draft saved{:else if saveState === 'saving'}Saving draft{:else if saveState === 'failed'}Draft not saved
              to the server. A copy is kept on this device.{:else}Changes kept on this device{/if}
          </p>
        </div>
      </section>

      {#if layout === 'wide'}
        <!-- F7.5: drag or use the keyboard on the separator; the button hides and shows the result. -->
        <div class="split" class:collapsed={resultCollapsed}>
          <!-- A focusable separator is the WAI-ARIA window splitter, an interactive widget; Svelte lists the role as static. -->
          <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
          <div
            class="grip"
            role="separator"
            tabindex="0"
            aria-orientation="horizontal"
            aria-label="Result panel size"
            aria-controls="ws-result"
            aria-valuemin={0}
            aria-valuemax={RESULT_SHARE.max}
            aria-valuenow={resultCollapsed ? 0 : resultShare}
            aria-valuetext={resultCollapsed ? 'Result hidden' : `Result ${resultShare} percent of the column`}
            onkeydown={onSplitKey}
            onpointerdown={onSplitDown}
            onpointermove={onSplitMove}
            onpointerup={onSplitUp}
            onpointercancel={onSplitUp}
          >
            <span class="grip-line" aria-hidden="true"></span>
            {#if resultCollapsed && resultSummary}<span class="split-summary">{resultSummary}</span>{/if}
          </div>
          <button type="button" class="btn btn-quiet btn-compact" aria-expanded={!resultCollapsed} aria-controls="ws-result" onclick={toggleResult}>
            {resultCollapsed ? 'Show result' : 'Hide result'}
          </button>
        </div>
      {/if}

      <section
        id="ws-result"
        class="pane result-pane"
        hidden={!showResult}
        role={layout === 'phone' ? 'tabpanel' : undefined}
        aria-labelledby={layout === 'phone' ? 'ws-tab-result' : undefined}
        aria-label={layout === 'phone' ? undefined : 'Result'}
      >
        {#if notice && notice.kind === 'error'}
          <div class="banner banner-error notice" role="alert" tabindex="-1" data-result-focus>
            <p>{notice.text}</p>
            {#if notice.retry}
              <button type="button" class="btn btn-secondary btn-compact" onclick={retry}>{notice.action === 'submit' ? 'Submit again' : 'Run again'}</button>
            {/if}
          </div>
        {/if}
        {#if customOpen}
          <div class="custom-panel" id="ws-custom-panel">
            <div class="custom-head">
              <label for="ws-custom">Custom input</label>
              <button type="button" class="btn btn-quiet btn-compact" onclick={() => (customOpen = false)}>Close</button>
            </div>
            <textarea
              id="ws-custom"
              class="input textarea-code"
              rows="4"
              spellcheck="false"
              bind:this={customField}
              bind:value={customInput}
            ></textarea>
            <button type="button" class="btn btn-secondary btn-compact" disabled={lockText !== ''} aria-disabled={runBusy ? 'true' : undefined} onclick={runCustom}>
              <Icon name="play" />Run on this input
            </button>
          </div>
        {/if}
        <StreamNotice mode={streamMode} onresume={() => stream?.resume()} />

        {#if view === 'run' && judging.run}
          <div class="block">
            <div class="block-head">
              <h2 class="block-title" tabindex="-1" data-result-focus>{judging.run.results.some((r) => r.label === 'Custom input') ? 'Run on your input' : 'Run on samples'}</h2>
              {#if current}
                <button type="button" class="btn btn-quiet btn-compact" onclick={() => (view = 'submission')}>Show the latest submission</button>
              {/if}
            </div>
            <RunResults run={judging.run} {language} onjump={jump} samples={ws.samples} onruninput={lockText ? undefined : runOnInput} />
          </div>
        {:else if current && (exam || current.hidden)}
          <div class="block">
            <h2 class="block-title" tabindex="-1" data-result-focus>Latest submission</h2>
            <p class="headline-exam">Submitted</p>
            <p class="muted small">
              {formatWhen(current.created_at)}, {languageLabel(current.language)}.
              {#if showScores && current.state === 'done'}Score <span class="mono">{current.score} of {current.max_score}</span>.{:else}Scores
                appear after the contest.{/if}
            </p>
          </div>
        {:else if current}
          <div class="block">
            <!-- S5: "Show the latest run" is a quiet button on the heading's row. -->
            <div class="block-head">
              <h2 class="block-title" tabindex="-1" data-result-focus>Latest submission</h2>
              {#if judging.run}
                <button type="button" class="btn btn-quiet btn-compact" onclick={() => (view = 'run')}>Show the latest run</button>
              {/if}
            </div>
            <VerdictPanel
              submission={current}
              onjump={jump}
              idPrefix="current"
              {problemScore}
              sticky={layout === 'wide'}
              onruninput={lockText ? undefined : runOnInput}
            />
            {#if sampleFailure}
              <button type="button" class="btn btn-secondary btn-compact" disabled={runBusy || lockText !== ''} onclick={runSamples}>Run on samples</button>
            {/if}
          </div>
        {:else}
          <p class="muted empty" tabindex="-1" data-result-focus>Run on samples to check your code, then submit it. Results appear here.</p>
        {/if}

        <div class="block">
          <h2 class="block-title">Your submissions</h2>
          <SubmissionList submissions={judging.submissions} onjump={jump} {exam} {showScores} />
        </div>
      </section>
    </div>
  </div>
{/if}

{#snippet submitButton()}
  <button
    type="button"
    class="btn btn-primary btn-compact submit"
    class:btn-wait={submitBusy || waitLeft > 0}
    disabled={lockText !== ''}
    aria-disabled={submitBusy || waitLeft > 0 ? 'true' : undefined}
    aria-describedby={lockText ? 'ws-locked' : paceText || usedNow ? 'ws-submit-status' : undefined}
    onclick={() => submit()}><Icon name="send" />{submitLabel}</button
  >
{/snippet}

<ConfirmDialog
  open={confirmCompile}
  title="Your last run did not compile. Submit anyway?"
  body="A submission that does not compile still uses one of your attempts."
  confirmLabel="Submit anyway"
  onconfirm={() => {
    confirmCompile = false;
    compileWarned = true;
    void submit(undefined, true);
  }}
  oncancel={() => {
    confirmCompile = false;
    compileWarned = true;
  }}
/>

<ConfirmDialog
  open={confirmReset}
  title="Reset to starter?"
  body={`Your code for this problem is replaced with the ${languageLabel(language)} starter, and the draft is saved.`}
  confirmLabel="Reset to starter"
  danger
  onconfirm={() => void resetToStarter()}
  oncancel={() => (confirmReset = false)}
/>

<style>
  .state {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-form);
  }

  .ws {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-width: 0;
  }

  .ws [hidden] {
    display: none !important;
  }

  .title-area {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    min-width: 0;
  }

  .title-main {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }

  .limits {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .limits-tab {
    margin-bottom: var(--space-3);
  }

  .letter {
    margin-right: var(--space-3);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .rail-inline {
    flex-basis: 100%;
    min-width: 0;
  }

  .locked {
    padding-bottom: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .headline-exam {
    font-size: var(--fs-2);
    font-weight: 600;
  }

  .clock-slot:empty {
    display: none;
  }

  .note {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .banner-slot {
    flex-basis: 100%;
    min-width: 0;
  }

  .banner-slot:empty {
    display: none;
  }

  /* F7.2: under 1024 px the strip sticks to the top of the window, edge to edge. */
  .strip {
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
    display: grid;
    margin-top: var(--space-3);
    margin-inline: calc(-1 * var(--gutter));
    padding-inline: var(--gutter);
    border-bottom: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .strip-top {
    display: flex;
    align-items: center;
    gap: 0 var(--space-3);
    min-height: var(--control-h-touch);
    border-bottom: var(--rule) solid var(--line);
  }

  .strip-clock {
    flex: 1;
    min-width: 0;
    padding-block: var(--space-1);
  }

  .strip-tabs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0 var(--space-2);
  }

  .strip .submit {
    margin-block: var(--space-1);
  }

  .clock-inline {
    max-width: 22rem;
  }

  .tabs {
    display: flex;
    gap: var(--space-1);
    margin-bottom: calc(-1 * var(--rule));
  }

  .tab {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h-touch);
    padding-inline: var(--space-3);
    border: 0;
    border-bottom: calc(2 * var(--rule)) solid transparent;
    border-radius: var(--radius) var(--radius) 0 0;
    background: none;
    color: var(--ink-muted);
    font-weight: 500;
  }

  /* State is a shape (F9): the selected tab is underlined by a border, which
     forced-colours mode keeps, unlike an inset shadow. */
  .tab[aria-selected='true'] {
    border-bottom-color: var(--act);
    color: var(--ink);
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: var(--wait);
  }

  .pane {
    min-width: 0;
  }

  .statement-pane {
    padding-top: var(--space-5);
  }

  .code-col {
    display: grid;
    min-width: 0;
  }

  .editor-pane {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto auto;
    align-content: start;
    padding-top: var(--space-4);
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding-bottom: var(--space-2);
  }

  .toolbar .select {
    width: auto;
  }

  .toolbar .lang {
    width: 8rem;
  }

  .run-actions {
    display: flex;
    gap: var(--space-2);
    margin-left: auto;
  }

  /* "Submit in 24 s" fits without the button growing as it counts (S4). */
  .submit {
    min-width: 9rem;
  }

  /* On a phone the strip holds the three tabs and Submit on one line: the
     word carries Submit, and the button may grow while it counts down,
     since it ends the row and moves nothing. */
  @media (width < 768px) {
    .strip .submit {
      min-width: 0;
    }

    .strip .submit :global(svg) {
      display: none;
    }

    .strip .tab {
      padding-inline: var(--space-2);
    }
  }

  /* A narrow code column keeps the toolbar on one row: the icons go from
     Run, Submit and Editor settings, and the select and buttons narrow. */
  @container code (width < 36rem) {
    .run-actions :global(svg),
    .toolbar :global(.settings svg) {
      display: none;
    }

    .run-actions .submit {
      min-width: 7.5rem;
    }

    .toolbar .lang {
      width: 6.5rem;
    }

    .toolbar :global(.btn) {
      padding-inline: var(--space-2);
    }
  }

  .editor-box {
    height: max(18rem, 55dvh);
    min-height: 0;
  }

  /* The custom input box at the top of the result pane (S2). */
  .custom-panel {
    display: grid;
    justify-items: start;
    gap: var(--space-2);
    padding-bottom: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .custom-panel .input {
    width: 100%;
  }

  /* 16 px on touch screens, so iOS does not zoom into the box (F7.4). */
  @media (pointer: coarse) {
    .custom-panel .input {
      font-size: var(--fs-1);
    }
  }

  .custom-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .custom-toggle {
    margin-inline-start: calc(-1 * var(--space-3));
  }

  .under {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-4);
    padding-top: var(--space-2);
    font-size: var(--fs-0);
  }

  .under .submit-status {
    margin-left: auto;
  }

  .submit-status + .save {
    margin-left: 0;
  }

  .submit-status {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 0 var(--space-3);
    color: var(--ink-muted);
    font-size: var(--fs-0);
    text-align: right;
  }

  .save {
    margin-left: auto;
    color: var(--ink-muted);
  }

  .save.failed {
    color: var(--bad);
  }

  .result-pane {
    display: grid;
    align-content: start;
    gap: var(--space-4);
    padding-block: var(--space-5);
  }

  /* The heading of the result with its switch button on the same row (S5). */
  .block-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-1) var(--space-3);
  }

  .block-title:focus {
    outline: none;
  }

  .block-title:focus-visible {
    outline: calc(2 * var(--rule)) solid var(--act);
    outline-offset: var(--space-1);
  }

  .notice {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
  }

  .empty {
    font-size: var(--fs-0);
  }

  .block {
    display: grid;
    gap: var(--space-3);
    justify-items: stretch;
    min-width: 0;
  }

  .block > .btn {
    justify-self: start;
  }

  .block-title {
    font-size: var(--fs-1);
  }

  .rail-head {
    display: grid;
    justify-items: start;
    gap: var(--space-2);
  }

  .rail-head .back {
    margin-inline-start: calc(-1 * var(--space-1));
    padding-inline: var(--space-1);
  }

  .rail {
    display: grid;
    align-content: start;
    gap: var(--space-5);
    padding: var(--space-4);
    border-right: var(--rule) solid var(--line);
    background: var(--surface-2);
    overflow-y: auto;
  }

  .rail-problem {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius);
    background: var(--act-tint);
  }

  .rail-title {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .rail-score {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .bar {
    display: block;
    height: calc(2 * var(--rule));
    background: var(--line);
  }

  .bar-fill {
    display: block;
    height: 100%;
    background: var(--ok);
  }

  /* A partial score fills in the partial colour, as standings cells do (audit 01-15). */
  .bar-fill.part {
    background: var(--part);
  }

  /* Forced colours drop backgrounds: the rail bar keeps its track and fill. */
  @media (forced-colors: active) {
    .bar {
      outline: var(--rule) solid CanvasText;
    }

    .bar-fill {
      background: Highlight;
      forced-color-adjust: none;
    }

    /* A transparent border would be forced to CanvasText: unselected tabs paint theirs as the canvas. */
    .tab {
      border-bottom-color: Canvas;
    }

    .tab[aria-selected='true'] {
      border-bottom-color: Highlight;
    }
  }

  /* Tablet: the result follows the editor in the Code tab. */
  @media (768px <= width < 1024px) {
    .result-pane {
      border-top: var(--rule) solid var(--line);
      margin-top: var(--space-4);
    }
  }

  /* Three columns: rail, statement, code over verdict, filling the window. */
  @media (min-width: 1024px) {
    .ws {
      grid-template-columns: 11rem minmax(0, 1fr) minmax(22.5rem, 1.15fr);
      grid-template-rows: auto minmax(0, 1fr);
      grid-template-areas:
        'rail title code'
        'rail statement code';
      /* Sized from the shell (S2, F7.3): the header's real height, testing
         banner included, comes from App.svelte as --shell-head. No 32rem
         floor any more, so a short laptop never scrolls the page; 18rem only
         keeps a zoomed-in window usable. */
      height: calc(100dvh - var(--shell-head, var(--space-7)) - 2 * var(--space-3));
      min-height: 18rem;
      border: var(--rule) solid var(--line);
      background: var(--surface);
    }

    .rail {
      grid-area: rail;
    }

    .title-area {
      grid-area: title;
      padding: var(--space-4) var(--space-5) var(--space-3);
      border-bottom: var(--rule) solid var(--line);
    }

    .ws .title-area :global(h1) {
      font-size: var(--fs-3);
    }

    .statement-pane {
      grid-area: statement;
      padding: var(--space-4) var(--space-5) var(--space-6);
      overflow-y: auto;
    }

    /* The rows come from the separator (F7.5) as an inline style; this is the shape before it. */
    .code-col {
      grid-area: code;
      grid-template-rows: minmax(0, 3fr) auto minmax(0, 2fr);
      border-left: var(--rule) solid var(--line);
      min-height: 0;
    }

    .editor-pane {
      min-height: 0;
      padding: var(--space-2) var(--space-3) var(--space-1);
      container: code / inline-size;
    }

    /* F7.5: the bar between the editor and the result. The grip is the separator; the button hides the result. */
    .split {
      display: flex;
      align-items: center;
      gap: var(--space-2);
      padding-inline-end: var(--space-2);
      border-top: var(--rule) solid var(--line);
    }

    /* The grip line sits in the middle; a hidden result's one line sits before it. */
    .grip {
      display: grid;
      flex: 1;
      grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
      align-items: center;
      gap: var(--space-3);
      min-width: 0;
      min-height: var(--control-h);
      padding-inline: var(--space-4);
      cursor: row-resize;
      touch-action: none;
      user-select: none;
    }

    .grip:focus-visible {
      outline: calc(2 * var(--rule)) solid var(--act);
      outline-offset: calc(-2 * var(--rule));
    }

    .grip-line {
      grid-column: 2;
      width: var(--space-7);
      height: calc(2 * var(--rule));
      border-radius: var(--rule);
      background: var(--line-strong);
    }

    .grip:hover .grip-line,
    .grip:focus-visible .grip-line {
      background: var(--ink-muted);
    }

    .split-summary {
      grid-column: 1;
      grid-row: 1;
      overflow: hidden;
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .editor-box {
      height: auto;
    }

    .result-pane {
      --sticky-top: var(--space-3);
      --sticky-bleed: var(--space-4);
      padding: var(--sticky-top) var(--space-4) var(--space-4);
      overflow-y: auto;
    }
  }

  /* S6: a wider rail from 1280 px, so most titles fit on their two lines. */
  @media (min-width: 1280px) {
    .ws {
      grid-template-columns: 13rem minmax(0, 1fr) minmax(22.5rem, 1.15fr);
    }
  }

  /* Under 1280 px, at any height (1366 by 768 at 125 percent is 1093 px
     wide; audit S2 and council C1-042 at 1024 by 900): a narrower rail and
     a wider code column, so the toolbar keeps one row. */
  @media (1024px <= width < 1280px) {
    .ws {
      grid-template-columns: 10rem minmax(0, 1fr) minmax(22.5rem, 1.4fr);
    }

    .rail {
      padding-inline: var(--space-3);
    }

    .editor-pane {
      padding-inline: var(--space-2);
    }
  }

  /* The short tier (F7.3): a laptop at most 45rem tall, such as 1366 by 768 at 125 percent.
     The title shares one line with the limits and every pane gives back padding to the editor. */
  @media (min-width: 1024px) and (max-height: 45rem) {
    .title-area {
      padding-block: var(--space-2);
    }

    .title-main {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: 0 var(--space-3);
    }

    .ws .title-area :global(h1) {
      font-size: var(--fs-2);
    }

    .statement-pane {
      padding-top: var(--space-3);
    }

    .rail {
      gap: var(--space-3);
      padding-block: var(--space-3);
    }

    .editor-pane {
      padding-top: var(--space-1);
    }

    .toolbar {
      padding-bottom: var(--space-1);
    }

    .under {
      padding-top: var(--space-1);
    }

    .result-pane {
      --sticky-top: var(--space-2);
    }
  }
</style>
