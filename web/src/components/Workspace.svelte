<!--
  Workspace: read a problem, write code, run it, submit it and understand
  the verdict (design.md 5.3, cache/v1/m3-plan.md section 7). Built to be
  mounted anywhere a problem is solved: M3 mounts it for organisers at
  /admin/problems/{id}/try, M4 under a contest.

  Layout: from 1024 px three columns, a rail (the clock slot and the
  problem with the best score so far), the statement with its samples, and
  the code column with the editor in the dark well over the verdict panel.
  From 768 to 1023 px, Problem and Code tabs, the verdict under the editor.
  Under 768 px, Problem, Code and Result tabs, and the clock slot sits in
  the title area.

  Behaviour:
  - The draft is saved to the server every 10 s and when the editor or the
    window loses focus, and mirrored to localStorage on every pause; on load
    the newer of the two wins (lib/drafts.ts). An empty draft starts from
    the language's starter with fast input (lib/starters.ts).
  - Submit sends an idempotency key made per press and reused by Retry, so
    a retry after a lost answer never counts twice. Offline or unreachable
    requests say "Not sent" with a Retry button. A 429 shows the server's
    sentence with the wait in seconds.
  - Results stream over GET /api/events (lib/stream.ts): reconnects with
    jitter, falls back to polling every 15 s after three failures, and
    refetches own submissions after every reconnect. server_now on every
    response keeps the clock offset (lib/clock.ts).
  - Integrity counts (lib/integrity.ts): typed and pasted characters from
    the editor, window blur and focus, sent in batches; never in the way of
    a submit.
  - Ctrl+Enter submits and Ctrl+' runs on samples (Cmd on a Mac); a live
    region announces each result as a sentence.

  Props: `base` (the API base, such as "/api/workspace/12"), `backHref` and
  `backLabel` (the link above the title), `note` (one line in the clock
  slot when there is no clock, optional) and `clock` (a snippet for a
  contest clock, optional).
-->
<script lang="ts">
  import { onMount, tick, type Snippet } from 'svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { serverClock } from '../lib/clock';
  import {
    DEFAULT_FONT_SIZE,
    FONT_SIZES,
    draftStorage,
    mirrorKey,
    pickDraft,
    readEditorPrefs,
    readMirror,
    writeEditorPrefs,
    writeMirror,
  } from '../lib/drafts';
  import { setTitle } from '../lib/head';
  import { FLUSH_MS, IntegrityRecorder } from '../lib/integrity';
  import { session } from '../lib/session.svelte';
  import { isStarterOrEmpty, starter } from '../lib/starters';
  import { EventStream } from '../lib/stream';
  import { mergeRun, mergeSubmission, mergeSubmissions, reduceEvent, type JudgingState, type StreamEvent } from '../lib/submissionState';
  import {
    LANGUAGES,
    announcement,
    bestScore,
    firstFailure,
    isWaiting,
    languageLabel,
    limitsText,
    newKey,
    workspacePaths,
    type Language,
    type Run,
    type Submission,
    type Workspace,
  } from '../lib/workspace';
  import CodeEditor from './CodeEditor.svelte';
  import RunResults from './RunResults.svelte';
  import Statement from './Statement.svelte';
  import SubmissionList from './SubmissionList.svelte';
  import VerdictPanel from './VerdictPanel.svelte';

  interface Props {
    base: string;
    backHref: string;
    backLabel: string;
    note?: string;
    clock?: Snippet;
  }

  let { base, backHref, backLabel, note = '', clock }: Props = $props();

  /** How often the draft is saved while it changes. */
  const SAVE_MS = 10_000;

  const paths = $derived(workspacePaths(base));
  const storage = draftStorage();
  const prefs = readEditorPrefs(storage);
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const mod = isMac ? 'Cmd' : 'Ctrl';

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
  let announced = $state('');
  let submitBusy = $state(false);
  let runBusy = $state(false);
  let pendingKey: string | null = null;
  let pendingRun: { language: Language; source: string; input?: string } | null = null;
  let notice = $state<{ action: 'submit' | 'run'; text: string; retry: boolean } | null>(null);
  let waitUntil = $state(0);
  let nowTick = $state(Date.now());
  let streamMode = $state<'live' | 'polling' | 'connecting'>('connecting');

  // Custom input.
  let customOpen = $state(false);
  let customInput = $state('');
  let customField: HTMLTextAreaElement | undefined = $state();

  // Layout.
  let layout = $state<'wide' | 'tablet' | 'phone'>('wide');
  let tab = $state<'problem' | 'code' | 'result'>('problem');

  const latest = $derived(judging.submissions[0] ?? null);
  const current = $derived(
    watchId !== null ? (judging.submissions.find((s) => s.id === watchId) ?? latest) : latest,
  );
  const best = $derived(bestScore(judging.submissions));
  const waitLeft = $derived(Math.max(0, Math.ceil((waitUntil - nowTick) / 1000)));
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
  const showStatement = $derived(layout === 'wide' || tab === 'problem');
  const showEditor = $derived(layout === 'wide' || tab === 'code');
  const showResult = $derived(layout === 'wide' || (layout === 'tablet' ? tab === 'code' : tab === 'result'));

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

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
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
      loadError = messageOf(e);
      loadState = 'error';
      return;
    }
    if (!alive) return;
    ws = data;
    setTitle(`Try ${data.title}`);

    const picked = pickDraft(data.draft, readMirror(storage, mirrorKey(base)));
    if (picked.draft) {
      language = picked.draft.language;
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
    loadState = 'ready';
    startStream();
  }

  function startStream() {
    stream?.stop();
    stream = new EventStream({
      url: '/api/events',
      events: ['submission', 'run', 'clock'],
      onEvent: (type, data) => onStreamEvent({ type, data } as StreamEvent),
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
    const text = announcement(s);
    if (text !== announced) announced = text;
  });

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
      writeMirror(storage, mirrorKey(base), { language, source, updated_at: serverClock.serverNow() });
    }, 400);
  }

  async function saveDraft(): Promise<void> {
    if (loadState !== 'ready' && saveState !== 'local') return;
    if (source === savedSource && language === savedLanguage) return;
    const body = { language, source };
    writeMirror(storage, mirrorKey(base), { ...body, updated_at: serverClock.serverNow() });
    saveState = 'saving';
    try {
      await sendJSON<void>('PUT', paths.draft, body);
      savedLanguage = body.language;
      savedSource = body.source;
      saveState = source === savedSource && language === savedLanguage ? 'saved' : 'local';
    } catch (e) {
      if (handleSessionError(e)) return;
      saveState = 'failed';
    }
  }

  /** The last chance to save when the page goes away; not awaited. */
  function saveOnLeave() {
    if (source === savedSource && language === savedLanguage) return;
    writeMirror(storage, mirrorKey(base), { language, source, updated_at: serverClock.serverNow() });
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

  async function revealResult() {
    if (layout === 'phone') {
      tab = 'result';
      await tick();
    }
  }

  async function submit(retryKey?: string) {
    if (submitBusy || loadState !== 'ready') return;
    if (waitLeft > 0 && retryKey === undefined) {
      notice = { action: 'submit', text: `Wait ${waitLeft} s before submitting again.`, retry: false };
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
      view = 'submission';
      void saveDraft();
      await revealResult();
    } catch (e) {
      if (handleSessionError(e)) return;
      if (e instanceof ApiError && e.code === 'network') {
        notice = { action: 'submit', text: offlineText(), retry: true };
      } else {
        pendingKey = null;
        if (e instanceof ApiError && e.status === 429 && e.retryAfter !== null) {
          waitUntil = Date.now() + e.retryAfter * 1000;
          nowTick = Date.now();
        }
        notice = { action: 'submit', text: messageOf(e), retry: false };
      }
      await revealResult();
    } finally {
      submitBusy = false;
    }
  }

  async function startRun(body: { language: Language; source: string; input?: string }) {
    if (runBusy || loadState !== 'ready') return;
    pendingRun = body;
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
        notice = { action: 'run', text: offlineText(), retry: true };
      } else {
        pendingRun = null;
        notice = { action: 'run', text: messageOf(e), retry: false };
      }
      await revealResult();
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
    integrity?.flush();
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

  onMount(() => {
    const wide = window.matchMedia('(min-width: 1024px)');
    const tablet = window.matchMedia('(min-width: 768px)');
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

    return () => {
      alive = false;
      wide.removeEventListener('change', place);
      tablet.removeEventListener('change', place);
      clearInterval(flushTimer);
      clearInterval(saveTimer);
      clearInterval(secondTimer);
      clearTimeout(mirrorTimer);
      stream?.stop();
      integrity?.flush();
      saveOnLeave();
    };
  });
</script>

<svelte:window onkeydown={onKeydown} onblur={onWindowBlur} onfocus={onWindowFocus} onpagehide={onPageHide} />
<svelte:document onvisibilitychange={onVisibility} />

{#if loadState !== 'ready' || !ws}
  <div class="state">
    <a class="back" href={backHref}>{backLabel}</a>
    <h1 tabindex="-1">{loadState === 'missing' ? 'Problem not found' : 'Problem'}</h1>
    {#if loadState === 'loading'}
      <p class="muted" role="status">Loading the problem</p>
    {:else if loadState === 'forbidden'}
      <p>You cannot open this problem. Ask an organiser if you think you should.</p>
    {:else if loadState === 'missing'}
      <p>There is no problem with that number. <a href={backHref}>Go back.</a></p>
    {:else if loadState === 'error'}
      <div class="banner banner-error" role="alert"><p>{loadError}</p></div>
      <p><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
    {/if}
  </div>
{:else}
  <div class="ws" data-layout={layout}>
    {#if layout === 'wide'}
      <aside class="rail" aria-label="Problem">
        <div class="clock-slot">
          {#if clock}{@render clock()}{:else if note}<p class="note">{note}</p>{/if}
        </div>
        <div class="rail-problem">
          <span class="rail-title">{ws.title}</span>
          <span class="mono rail-score">{best ? `${best.score} of ${best.max}` : 'Not submitted'}</span>
          <span class="bar" aria-hidden="true">
            <span class="bar-fill" style:width="{best && best.max > 0 ? Math.round((best.score / best.max) * 100) : 0}%"></span>
          </span>
        </div>
      </aside>
    {/if}

    <div class="title-area">
      <div class="title-main">
        <a class="back" href={backHref}>{backLabel}</a>
        <h1 tabindex="-1">{ws.title}</h1>
        <p class="limits">{limitsText(ws.time_limit_ms, ws.memory_limit_mb)}</p>
      </div>
      {#if layout !== 'wide'}
        <div class="clock-slot clock-inline">
          {#if clock}{@render clock()}{:else if note}<p class="note">{note}</p>{/if}
        </div>
      {/if}
    </div>

    {#if layout !== 'wide'}
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
    {/if}

    <section
      id="ws-problem"
      class="pane statement-pane"
      hidden={!showStatement}
      role={layout === 'wide' ? undefined : 'tabpanel'}
      aria-labelledby={layout === 'wide' ? undefined : 'ws-tab-problem'}
      aria-label={layout === 'wide' ? 'Statement' : undefined}
    >
      <Statement markdown={ws.statement_md} samples={ws.samples} />
    </section>

    <div class="code-col">
      <section
        id="ws-code"
        class="pane editor-pane"
        hidden={!showEditor}
        role={layout === 'wide' ? undefined : 'tabpanel'}
        aria-labelledby={layout === 'wide' ? undefined : 'ws-tab-code'}
        aria-label={layout === 'wide' ? 'Code' : undefined}
      >
        <div class="toolbar">
          <label class="visually-hidden" for="ws-language">Language</label>
          <select id="ws-language" class="select lang" value={language} onchange={onLanguageChange}>
            {#each LANGUAGES as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
          </select>
          <label class="visually-hidden" for="ws-font">Font size</label>
          <select id="ws-font" class="select font" value={fontSize} onchange={onFontChange}>
            {#each FONT_SIZES as size (size)}<option value={size}>{size} px</option>{/each}
          </select>
          <span class="spacer"></span>
          <button type="button" class="btn btn-secondary" disabled={runBusy} onclick={runSamples}>Run on samples</button>
          <button type="button" class="btn btn-primary" disabled={submitBusy} onclick={() => submit()}>Submit</button>
        </div>

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
          <label class="tab-toggle">
            <input type="checkbox" checked={tabMovesFocus} onchange={onTabToggle} />
            Tab moves focus
          </label>
          <details class="custom" bind:open={customOpen}>
            <summary>Custom input</summary>
            <div class="custom-body">
              <label class="visually-hidden" for="ws-custom">Custom input</label>
              <textarea
                id="ws-custom"
                class="input textarea-code"
                rows="4"
                spellcheck="false"
                bind:this={customField}
                bind:value={customInput}
              ></textarea>
              <button type="button" class="btn btn-secondary btn-compact" disabled={runBusy} onclick={runCustom}>Run on this input</button>
            </div>
          </details>
          <p class="save" class:failed={saveState === 'failed'} aria-live="polite">
            {#if saveState === 'saved'}Draft saved{:else if saveState === 'saving'}Saving draft{:else if saveState === 'failed'}Draft not saved
              to the server. A copy is kept on this device.{:else}Changes kept on this device{/if}
          </p>
        </div>
        <p class="hint" id="ws-keys">
          <kbd>{mod}+Enter</kbd> submits, <kbd>{mod}+'</kbd> runs on samples, <kbd>Esc</kbd> then <kbd>Tab</kbd> leaves the editor.
        </p>
      </section>

      <section
        id="ws-result"
        class="pane result-pane"
        hidden={!showResult}
        role={layout === 'phone' ? 'tabpanel' : undefined}
        aria-labelledby={layout === 'phone' ? 'ws-tab-result' : undefined}
        aria-label={layout === 'phone' ? undefined : 'Result'}
      >
        <p class="visually-hidden" aria-live="polite">{announced}</p>

        {#if notice}
          <div class="banner banner-error notice" role="alert">
            <p>{notice.text}</p>
            {#if notice.retry}
              <button type="button" class="btn btn-secondary btn-compact" onclick={retry}>Retry</button>
            {/if}
          </div>
        {/if}
        {#if waitLeft > 0 && !notice}
          <p class="muted small">You can submit again in {waitLeft} s.</p>
        {/if}
        {#if streamMode === 'polling'}
          <p class="muted small">Live updates are paused. Checking for results every 15 s.</p>
        {/if}

        {#if view === 'run' && judging.run}
          <div class="block">
            <h2 class="block-title">{judging.run.results.some((r) => r.label === 'Custom input') ? 'Run on your input' : 'Run on samples'}</h2>
            <RunResults run={judging.run} {language} onjump={jump} />
            {#if current}
              <button type="button" class="btn btn-quiet btn-compact switch" onclick={() => (view = 'submission')}>Show the latest submission</button>
            {/if}
          </div>
        {:else if current}
          <div class="block">
            <h2 class="block-title">Latest submission</h2>
            <VerdictPanel submission={current} onjump={jump} idPrefix="current" />
            {#if sampleFailure}
              <button type="button" class="btn btn-secondary btn-compact" disabled={runBusy} onclick={runSamples}>Run on samples</button>
            {/if}
            {#if judging.run}
              <button type="button" class="btn btn-quiet btn-compact switch" onclick={() => (view = 'run')}>Show the last run</button>
            {/if}
          </div>
        {:else}
          <p class="muted empty">Run on samples to check your code, then submit it. Results appear here.</p>
        {/if}

        <div class="block">
          <h2 class="block-title">Your submissions</h2>
          <SubmissionList submissions={judging.submissions} onjump={jump} />
        </div>
      </section>
    </div>
  </div>
{/if}

<style>
  .state {
    display: grid;
    gap: var(--space-3);
    max-width: 48rem;
  }

  .back {
    width: fit-content;
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
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

  h1 {
    font-size: var(--fs-4);
    overflow-wrap: anywhere;
  }

  .limits {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .clock-slot:empty {
    display: none;
  }

  .note {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .clock-inline {
    max-width: 22rem;
  }

  .tabs {
    display: flex;
    gap: var(--space-1);
    margin-top: var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .tab {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h-touch);
    padding-inline: var(--space-4);
    border: 0;
    border-radius: var(--radius) var(--radius) 0 0;
    background: none;
    color: var(--ink-muted);
    font-weight: 500;
  }

  .tab[aria-selected='true'] {
    color: var(--ink);
    box-shadow: inset 0 calc(-2 * var(--rule)) 0 var(--act);
  }

  .tab[aria-selected='true']:focus-visible {
    box-shadow:
      inset 0 calc(-2 * var(--rule)) 0 var(--act),
      var(--focus);
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

  .spacer {
    flex: 1 1 auto;
  }

  .editor-box {
    height: max(18rem, 55dvh);
    min-height: 0;
  }

  .under {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-4);
    padding-top: var(--space-2);
    font-size: var(--fs-0);
  }

  .tab-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .tab-toggle input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .hint {
    padding-top: var(--space-1);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  kbd {
    padding: 0 var(--space-1);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    font-size: 0.92em;
    white-space: nowrap;
  }

  .save {
    margin-left: auto;
    color: var(--ink-muted);
  }

  .save.failed {
    color: var(--bad);
  }

  /* Opened, the custom input takes a row of its own under the others. */
  .custom[open] {
    flex-basis: 100%;
    order: 1;
  }

  .custom summary {
    width: fit-content;
    min-height: var(--control-h);
    border-radius: var(--radius);
    color: var(--act);
    font-size: var(--fs-0);
    font-weight: 500;
    line-height: var(--control-h);
    cursor: pointer;
  }

  .custom-body {
    display: grid;
    justify-items: start;
    gap: var(--space-2);
    padding-bottom: var(--space-2);
  }

  .result-pane {
    display: grid;
    align-content: start;
    gap: var(--space-4);
    padding-block: var(--space-5);
  }

  .notice {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
  }

  .small {
    font-size: var(--fs-0);
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

  /* Tablet: the result follows the editor in the Code tab. */
  @media (min-width: 768px) and (max-width: 1023px) {
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
      height: calc(100dvh - var(--space-7) - 2 * var(--space-3) - var(--rule));
      min-height: 32rem;
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

    h1 {
      font-size: var(--fs-3);
    }

    .statement-pane {
      grid-area: statement;
      padding: var(--space-4) var(--space-5) var(--space-6);
      overflow-y: auto;
    }

    .code-col {
      grid-area: code;
      grid-template-rows: minmax(0, 3fr) minmax(0, 2fr);
      border-left: var(--rule) solid var(--line);
      min-height: 0;
    }

    .editor-pane {
      min-height: 0;
      padding: var(--space-2) var(--space-3) var(--space-1);
    }

    .editor-box {
      height: auto;
    }

    .result-pane {
      padding: var(--space-3) var(--space-4) var(--space-4);
      overflow-y: auto;
      border-top: var(--rule) solid var(--line);
    }
  }

  @media (min-width: 1280px) {
    .ws {
      grid-template-columns: 12.5rem minmax(0, 1fr) minmax(22.5rem, 1.15fr);
    }
  }
</style>
