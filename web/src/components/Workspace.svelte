<!--
  Workspace: read a problem, write code, run it, submit it and understand
  the verdict (design.md 5.3, cache/v3/design/editor.md). Mounted for
  organisers at /admin/problems/{id}/try and for students under a contest.
  Split once in B2: the code and where it is kept live in
  lib/draft.svelte.ts, submitting, running and the result tabs in
  lib/judging.svelte.ts, the grid in workspace/Layout, the toolbar in
  workspace/Toolbar and the results in workspace/ResultPane. This file
  wires them: it loads the problem, holds the timers, the keys and the
  layout tier, and the two confirms.

  - From 1024 px the rail, the statement and the code column with the
    editor over the tabbed result; the splitter sets the result's share,
    20 to 80 percent, or hides it to one line, kept on this device (F7.5).
    Under 1024 px the panes are tabs under the sticky strip, which carries
    Submit; when a tab switches by itself after Submit or Run, focus moves
    to the result's first line (F7.4).
  - The draft is saved every 10 s, when the editor or the window loses
    focus, before sign-out and when the page hides; integrity counts
    (lib/integrity.ts) are sent in batches and never hold up a submit.
  - Ctrl+Enter submits, Ctrl+' runs (on Your input while that box has
    focus), Ctrl+S saves (Cmd on a Mac); an editor key pressed outside the
    editor shows a toast saying where it works (P6). Help lists them all.
  - Reset and a language switch over typed code ask first (F7.12, P5).
  - During a live contest (a `clock` is given) sign out asks first (F7.6).

  Props: `base` (the API base, such as "/api/workspace/12"), `backHref` and
  `backLabel`, `contestHref` (where a refused "not started" or "time is
  up" load points), `note` (one line in the clock slot when there is no
  clock), `clock` (a snippet, given true for one compact line). For
  contests: `banner` (drawn above the title), `rail` (the problems, given
  the layout), `label` (the problem letter), `context` (the contest title,
  for the document title), `locked` (why Run and Submit are off, unless the server's
  own `refusal` says it better), `problemScore` (for "your score stays"),
  `used` (for "3 of 15 submissions used"), `minGapS` (the seconds between
  submits), `statementKey` (raised when the statement changed: it reloads,
  the editor is left alone), and `extraEvents` with `onevent` and
  `onresync` (more events on the same stream, and a hook on reconnect).
-->
<script lang="ts">
  import { onMount, tick, type Snippet } from 'svelte';
  import { polite } from '../lib/announce.svelte';
  import { ApiError, errorText, getJSON, isSignedOut } from '../lib/api';
  import { media } from '../lib/breakpoints';
  import { serverClock } from '../lib/clock';
  import { Draft } from '../lib/draft.svelte';
  import { RESULT_SHARE, STATEMENT_KEY, draftStorage, readResultPanel, writeResultPanel } from '../lib/drafts';
  import { keepHeadingFocus } from '../lib/focus';
  import { setTitle } from '../lib/head';
  import { FLUSH_MS, IntegrityRecorder } from '../lib/integrity';
  import { Judging } from '../lib/judging.svelte';
  import { beforeSignOut, guardSignOut, session } from '../lib/session.svelte';
  import { isTypingTarget } from '../lib/shortcuts';
  import { showToast } from '../lib/toast.svelte';
  import { LANGUAGES, bestScore, headline, isWaiting, languageLabel, limitsText, runHeadline, type Workspace } from '../lib/workspace';
  import CodeEditor from './CodeEditor.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import PageHeader from './PageHeader.svelte';
  import PageState from './PageState.svelte';
  import Splitter from './Splitter.svelte';
  import Statement from './Statement.svelte';
  import Layout from './workspace/Layout.svelte';
  import Rail from './workspace/Rail.svelte';
  import ResultPane from './workspace/ResultPane.svelte';
  import StatusLine from './workspace/StatusLine.svelte';
  import Strip from './workspace/Strip.svelte';
  import SubmitButton from './workspace/SubmitButton.svelte';
  import Title from './workspace/Title.svelte';
  import Toolbar from './workspace/Toolbar.svelte';

  interface Props {
    base: string;
    backHref: string;
    backLabel: string;
    contestHref?: string;
    note?: string;
    clock?: Snippet<[boolean]>;
    banner?: Snippet;
    rail?: Snippet<[string]>;
    label?: string;
    context?: string;
    locked?: string;
    problemScore?: { score: number | null; max: number } | null;
    used?: { count: number; max: number } | null;
    minGapS?: number;
    statementKey?: number;
    extraEvents?: readonly string[];
    onevent?: (type: string, data: unknown) => void;
    onresync?: () => void;
  }

  let {
    base,
    backHref,
    backLabel,
    contestHref = '',
    note = '',
    clock,
    banner,
    rail,
    label = '',
    context = '',
    locked = '',
    problemScore = null,
    used = null,
    minGapS = 0,
    statementKey = 0,
    extraEvents = [],
    onevent,
    onresync,
  }: Props = $props();

  /** How often the draft is saved while it changes. */
  const SAVE_MS = 10_000;
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  // The base never changes: the contest page mounts a new workspace per problem.
  // svelte-ignore state_referenced_locally
  const api = base;
  // The mirror is per account (lib/drafts.ts); the shell mounts this only when signed in.
  const draft = new Draft(api, session.user?.id ?? 0, typeof window !== 'undefined' && !media('tablet').matches);

  let loadState = $state<'loading' | 'ready' | 'missing' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  // The server's 403 code: not_started and contest_over point back to the contest page (C2-017).
  let forbiddenCode = $state('');
  let ws = $state.raw<Workspace | null>(null);
  let editor: CodeEditor | undefined = $state();
  let integrity: IntegrityRecorder | null = null;
  let alive = true;
  let confirmReset = $state(false);

  const judging = new Judging({
    base: api,
    problemId: () => ws?.problem_id,
    ready: () => loadState === 'ready',
    locked: () => lockText,
    minGapS: () => minGapS,
    code: () => ({ language: draft.language, source: draft.source }),
    integrity: () => integrity,
    submitted: () => void draft.save(),
    reveal: revealResult,
  });

  // Layout: from 1024 px three columns, from 768 px two tabs, under it three.
  let layout = $state<'wide' | 'tablet' | 'phone'>('wide');
  let tab = $state<'problem' | 'code' | 'result'>('problem');
  // Under 1024 px code is read, not written (V3-14, owner answer 2).
  const NARROW = 'Use a computer to edit, run or submit.';
  // The server says why Run and Submit are off when they are; `locked` is the fallback.
  const lockText = $derived(ws?.refusal ? ws.refusal : layout !== 'wide' ? NARROW : locked);
  // A short laptop starts with a smaller result, so the editor keeps its height (F7.3).
  const panel = readResultPanel(draftStorage(), typeof window !== 'undefined' && media('short').matches ? 30 : RESULT_SHARE.default);
  let resultShare = $state(panel.share);
  let resultCollapsed = $state(panel.collapsed);
  let codeCol: HTMLDivElement | undefined = $state();
  // In a contest from 1024 px (B6): the statement's share beside the code, or hidden (E5).
  const statementPanel = readResultPanel(draftStorage(), 45, STATEMENT_KEY);
  let statementShare = $state(statementPanel.share);
  let statementHidden = $state(statementPanel.collapsed);
  let grid: HTMLDivElement | undefined = $state();
  const cols = $derived(statementHidden ? 'minmax(0, 1fr)' : `minmax(0, ${statementShare}fr) auto minmax(22.5rem, ${100 - statementShare}fr)`);

  // The contest may allow fewer languages than the judge knows.
  const languages = $derived.by(() => {
    const ids = (ws?.languages ?? []).map((l) => l.id);
    return ids.length > 0 ? LANGUAGES.filter((l) => ids.includes(l.id)) : [...LANGUAGES];
  });
  /** The selected language and the judge's time factor for it, for the limits line (D323). */
  const limits = $derived(
    ws
      ? limitsText(ws.time_limit_ms, ws.memory_limit_mb, {
          label: languages.find((l) => l.id === draft.language)?.label ?? draft.language,
          factor: ws.languages?.find((l) => l.id === draft.language)?.time_factor,
        })
      : '',
  );
  // The contest counts submissions as they are made; judge failures do not count (docs/API.md).
  const usedNow = $derived(
    used ? { count: Math.max(used.count, judging.state.submissions.filter((x) => x.state !== 'error').length), max: used.max } : null,
  );
  // The one line a hidden result keeps in the splitter's bar.
  const resultSummary = $derived.by(() => {
    if (judging.tab !== 'submissions' && judging.state.run) return runHeadline(judging.state.run).text;
    const c = judging.current;
    if (!c) return '';
    return c.hidden ? 'Submitted' : headline(c).text;
  });
  const rows = $derived(
    layout !== 'wide' ? undefined : resultCollapsed ? 'minmax(0, 1fr) auto 0' : `minmax(0, ${100 - resultShare}fr) auto minmax(0, ${resultShare}fr)`,
  );

  async function load() {
    loadState = 'loading';
    let data: Workspace;
    try {
      const sent = Date.now();
      data = await getJSON<Workspace>(api);
      serverClock.observe(data.server_now, sent, Date.now());
    } catch (e) {
      if (isSignedOut(e)) return;
      if (e instanceof ApiError && e.status === 403) {
        loadError = e.message || 'You cannot open this problem.';
        forbiddenCode = e.code;
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
    draft.restore(data);
    judging.loaded(data);
    const restore = keepHeadingFocus();
    loadState = 'ready';
    void restore();
    // After Start the pressed button is gone: focus goes to the problem's heading, never the body (P8).
    void tick().then(() => {
      const a = document.activeElement;
      if (!a || a === document.body) document.querySelector<HTMLElement>('main h1')?.focus();
    });
    judging.startStream(extraEvents, onevent, onresync);
  }

  // A changed statement (C3): reload what the student reads, never the editor.
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
      const data = await getJSON<Workspace>(api);
      if (!alive || !ws) return;
      ws = { ...ws, title: data.title, statement_md: data.statement_md, samples: data.samples, time_limit_ms: data.time_limit_ms, memory_limit_mb: data.memory_limit_mb };
    } catch {
      // The old statement stays; the next change or a reload brings the new one.
    }
  }

  /**
   * Shows the result pane when another tab hides it. When the tab switched
   * by itself, focus follows to the shown panel's first line, so a keyboard
   * or screen reader user lands on what changed (F7.4). From 1024 px
   * nothing switches and focus stays on the button.
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
    document.querySelector<HTMLElement>('#ws-result [data-result-focus]:not([hidden] *)')?.focus();
  }

  /** "Run on this input" on a failing example: the input goes in Your input, which shows and runs it. */
  async function runOnInput(input: string) {
    judging.customInput = input;
    judging.tab = 'input';
    if (layout === 'phone') tab = 'result';
    else if (layout === 'tablet') tab = 'code';
    await tick();
    judging.runCustom();
  }

  async function jump(line: number, col: number | null) {
    if (layout !== 'wide') {
      tab = 'code';
      await tick();
    }
    editor?.jumpTo(line, col);
  }

  async function reset() {
    confirmReset = false;
    await draft.reset();
    polite('Reset. The starter is back.');
    editor?.focus();
  }

  function onStatementSplit(next: { share: number; collapsed: boolean }, remember: boolean) {
    statementShare = next.share;
    statementHidden = next.collapsed;
    if (remember) writeResultPanel(draftStorage(), { share: statementShare, collapsed: statementHidden }, STATEMENT_KEY);
  }

  function onSplit(next: { share: number; collapsed: boolean }, remember: boolean) {
    resultShare = next.share;
    resultCollapsed = next.collapsed;
    if (remember) writeResultPanel(draftStorage(), { share: resultShare, collapsed: resultCollapsed });
  }

  // Editor keys pressed outside the editor say where they work (P6).
  const EDITOR_KEYS = new Set(['/', 'd', 'l', 'h', 'g', 'u']);
  let hinted = 0;

  function onKeydown(event: KeyboardEvent) {
    if (event.defaultPrevented || loadState !== 'ready') return;
    const modKey = isMac ? event.metaKey : event.ctrlKey;
    if (!modKey || event.altKey) return;
    const target = event.target as Element | null;
    // Inside the editor's find panel, Mod+Enter belongs to the panel, never to Submit.
    if (target?.closest?.('.cm-panel')) return;
    if (EDITOR_KEYS.has(event.key.toLowerCase()) && !target?.closest?.('.cm-editor') && !isTypingTarget(target)) {
      if (Date.now() - hinted > 4000) {
        hinted = Date.now();
        showToast('Click in the code editor first. Editor shortcuts work only there.');
      }
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      void judging.submit();
    } else if (event.key === "'") {
      event.preventDefault();
      if (document.activeElement?.id === 'ws-custom') judging.runCustom();
      else judging.runExamples();
    } else if (event.key === 's' || event.key === 'S') {
      event.preventDefault();
      void draft.save();
    }
  }

  function onWindowBlur() {
    integrity?.blur();
    void draft.save();
  }

  function onVisibility() {
    if (document.visibilityState === 'hidden') {
      integrity?.flush();
      void draft.save();
    }
  }

  function onPageHide() {
    integrity?.flush(true);
    draft.saveOnLeave();
  }

  // F7.6: during a live contest, sign out asks first (App.svelte); the draft save still runs before it.
  $effect(() => {
    if (!clock) return;
    return guardSignOut('Your draft is saved first. Your time keeps running while you are signed out.');
  });

  $effect(() => {
    draft.editor = editor;
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

    integrity = new IntegrityRecorder(`${api}/events`, () => serverClock.serverNow());
    const flushTimer = setInterval(() => integrity?.flush(), FLUSH_MS);
    const saveTimer = setInterval(() => void draft.save(), SAVE_MS);
    const secondTimer = setInterval(() => judging.tick(), 1000);

    void load();
    const forgetSave = beforeSignOut(() => draft.save());

    return () => {
      forgetSave();
      alive = false;
      wide.removeEventListener('change', place);
      tablet.removeEventListener('change', place);
      clearInterval(flushTimer);
      clearInterval(saveTimer);
      clearInterval(secondTimer);
      draft.dispose();
      judging.stop();
      integrity?.flush(true);
      integrity?.stop();
      draft.saveOnLeave();
    };
  });
</script>

<svelte:window onkeydown={onKeydown} onblur={onWindowBlur} onfocus={() => integrity?.focus()} onpagehide={onPageHide} />
<svelte:document onvisibilitychange={onVisibility} />

{#if loadState !== 'ready' || !ws}
  <div class="state">
    <PageHeader compact title={loadState === 'missing' ? 'Problem not found' : 'Problem'} back={{ href: backHref, label: backLabel }} />
    {#if loadState === 'loading'}
      <PageState kind="loading" message="Loading the problem" />
    {:else if loadState === 'forbidden' && (forbiddenCode === 'not_started' || forbiddenCode === 'contest_over')}
      <PageState kind="missing" message={loadError} parent={{ href: contestHref || backHref, label: 'Go to the contest page.' }} />
    {:else if loadState === 'forbidden'}
      <PageState kind="forbidden" message={loadError} ask="Ask an organiser if you think you should." />
    {:else if loadState === 'missing'}
      <PageState kind="missing" message="This problem does not exist." parent={{ href: backHref, label: 'Go back.' }} />
    {:else if loadState === 'error'}
      <PageState kind="error" message={loadError} onretry={load} />
    {/if}
  </div>
{:else}
  {@const problem = ws}
  <Layout
    {layout}
    {rows}
    {cols}
    {statementHidden}
    bar={rail ? contestBar : undefined}
    bind:grid
    showStatement={layout === 'wide' || tab === 'problem'}
    showEditor={layout === 'wide' || tab === 'code'}
    showResult={layout === 'wide' ? !resultCollapsed : layout === 'tablet' ? tab === 'code' : tab === 'result'}
    bind:column={codeCol}
  >
    {#snippet side()}
      <Rail {clock} {note} {backHref} {backLabel} title={problem.title} best={bestScore(judging.state.submissions)} />
    {/snippet}
    {#snippet title()}
      <Title
        {layout}
        {banner}
        {rail}
        {label}
        title={problem.title}
        {limits}
        back={{ href: backHref, label: backLabel }}
        {note}
        hasClock={!!clock}
        {statementHidden}
        ontogglestatement={rail ? () => onStatementSplit({ share: statementShare, collapsed: !statementHidden }, true) : undefined}
      />
    {/snippet}
    {#snippet strip()}
      <Strip
        layout={layout === 'phone' ? 'phone' : 'tablet'}
        {clock}
        {rail}
        {tab}
        ontab={(t) => (tab = t)}
        judging={!!judging.current && isWaiting(judging.current)}
      />
    {/snippet}
    {#snippet statement()}
      {#if layout !== 'wide'}<p class="limits">{limits}</p>{/if}
      <Statement markdown={problem.statement_md} samples={problem.samples} />
    {/snippet}
    {#snippet code()}
      {#if layout !== 'wide'}
        <p class="narrow">{NARROW}</p>
      {:else}
      <Toolbar
        {languages}
        language={draft.pendingLanguage ?? draft.language}
        onlanguage={(l) => draft.choose(l)}
        onreset={() => (confirmReset = true)}
        settings={{ fontSize: draft.fontSize, tabSize: draft.tabSize, wrap: draft.wrap, tabMovesFocus: draft.tabMovesFocus }}
        onsettings={(s) => draft.settings(s)}
        locked={lockText}
        runBusy={judging.runBusy}
        onrun={() => judging.runExamples()}
        submit={submitButton}
      />
      {/if}
      <div class="editor-box">
        <CodeEditor
          bind:this={editor}
          initial={draft.initialDoc}
          language={draft.language}
          label={`Code for ${problem.title}, ${languageLabel(draft.language)}`}
          fontSize={draft.fontSize}
          tabMovesFocus={draft.tabMovesFocus}
          tabSize={draft.tabSize}
          wrap={draft.wrap}
          readOnly={layout !== 'wide'}
          oncursor={(line, col) => (draft.cursor = { line, col })}
          onchange={(s) => draft.change(s)}
          ontyped={(n) => integrity?.typedChars(n)}
          onpaste={(n) => integrity?.paste(n)}
          onblur={() => void draft.save()}
          onsubmit={() => void judging.submit()}
          onrun={() => judging.runExamples()}
        />
      </div>
      <StatusLine pace={judging.paceText} used={usedNow && !lockText ? usedNow : null} saveState={draft.saveState} cursor={draft.cursor} />
    {/snippet}
    {#snippet colSplit()}
      <Splitter
        share={statementShare}
        collapsed={statementHidden}
        min={RESULT_SHARE.min}
        max={RESULT_SHARE.max}
        summary=""
        controls="ws-problem"
        column={grid}
        orientation="columns"
        name="Statement"
        button={false}
        onchange={onStatementSplit}
      />
    {/snippet}
    {#snippet splitter()}
      <Splitter
        share={resultShare}
        collapsed={resultCollapsed}
        min={RESULT_SHARE.min}
        max={RESULT_SHARE.max}
        summary={resultSummary}
        controls="ws-result"
        column={codeCol}
        onchange={onSplit}
      />
    {/snippet}
    {#snippet result()}
      <ResultPane
        {judging}
        samples={problem.samples}
        language={draft.language}
        {problemScore}
        locked={lockText}
        sticky={layout === 'wide'}
        narrow={layout !== 'wide'}
        onjump={jump}
        onruninput={runOnInput}
      />
    {/snippet}
  </Layout>
{/if}

{#snippet contestBar()}{@render rail?.('bar')}{/snippet}

{#snippet submitButton()}
  <SubmitButton
    label={judging.submitLabel}
    waiting={judging.submitBusy || judging.waitLeft > 0}
    locked={lockText !== ''}
    describedby={lockText ? 'ws-locked' : judging.paceText || usedNow ? 'ws-submit-status' : undefined}
    onclick={() => void judging.submit()}
  />
{/snippet}

<ConfirmDialog
  open={judging.confirmCompile}
  title="Your last run did not compile. Submit anyway?"
  body="A submission that does not compile still uses one of your attempts."
  confirmLabel="Submit anyway"
  onconfirm={() => judging.answerCompile(true)}
  oncancel={() => judging.answerCompile(false)}
/>

<ConfirmDialog
  open={confirmReset}
  title="Replace your code with the starter?"
  body={`Your ${languageLabel(draft.language)} code for this problem is replaced with the starter, and saved.`}
  confirmLabel="Reset"
  danger
  onconfirm={() => void reset()}
  oncancel={() => (confirmReset = false)}
/>

<ConfirmDialog
  open={draft.pendingLanguage !== null}
  title={`Replace your code with the ${languageLabel(draft.pendingLanguage ?? draft.language)} starter?`}
  body={`Your ${languageLabel(draft.language)} code is kept and comes back when you switch back. Cancel keeps the code you have now.`}
  confirmLabel="Use the starter"
  onconfirm={() => draft.confirmLanguage(true)}
  oncancel={() => draft.confirmLanguage(false)}
/>

<style>
  .state {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-form);
  }

  .narrow {
    padding-bottom: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .limits {
    margin-bottom: var(--space-3);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .editor-box {
    height: max(18rem, 55dvh);
    min-height: 0;
  }

  @media (min-width: 1024px) {
    .editor-box {
      height: auto;
    }
  }
</style>
