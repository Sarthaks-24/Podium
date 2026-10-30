<!--
  Workspace: read a problem, write code, run it, submit it and understand
  the verdict, for organisers at /admin/problems/{id}/try and for students
  in a contest. The code and where it is kept live in lib/draft.svelte.ts,
  submitting, running and the result tabs in lib/judging.svelte.ts, and
  the grid, toolbar and results in components/workspace/; this file wires
  them together: it loads the problem through lib/loader, holds the
  timers, the keys and the layout tier, and asks before a reset, a
  language switch over typed code, or a sign-out during a live contest;
  while a reset or a language switch waits for its answer, Run and Submit
  do nothing, so they never use the code about to be replaced. A refused
  paste is said once and goes at the next key, Run or Submit, and closing
  the workspace empties the announcer, so nothing said about this problem
  is left under the next one. The draft is saved every 10 s and whenever focus or the page goes, and
  integrity counts go out in batches that never hold up a submit, so a
  crash or a closed lid loses nothing and judging is never slowed.
-->
<script lang="ts">
  import { onMount, tick, type Snippet } from 'svelte';
  import { hush, polite } from '../lib/announce.svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { loader } from '../lib/loader.svelte';
  import { media } from '../lib/breakpoints';
  import { serverClock } from '../lib/clock';
  import { compileMarks } from '../lib/compileErrors';
  import { Draft } from '../lib/draft.svelte';
  import { RESULT_SHARE, STATEMENT_KEY, draftStorage, readResultPanel, writeResultPanel } from '../lib/drafts';
  import { keepHeadingFocus } from '../lib/focus';
  import { setTitle } from '../lib/head';
  import { PASTE_BLOCKED, type ExamOptions } from '../lib/contests';
  import { FLUSH_MS, IntegrityRecorder } from '../lib/integrity';
  import { Judging } from '../lib/judging.svelte';
  import { beforeSignOut, guardSignOut, session } from '../lib/session.svelte';
  import { isTypingTarget } from '../lib/shortcuts';
  import { dismissToast, showToast } from '../lib/toast.svelte';
  import { LANGUAGES, bestScore, headline, isWaiting, languageLabel, limitsText, runCompileFailed, runHeadline, type Language, type Workspace } from '../lib/workspace';
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
    exam?: ExamOptions | null;
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
    exam = null,
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

  let ws = $state.raw<Workspace | null>(null);
  let editor: CodeEditor | undefined = $state();
  let integrity: IntegrityRecorder | null = null;
  let confirmReset = $state(false);
  /** A past submission's code waiting on the "Use this code" confirm. */
  let restoring = $state.raw<{ language: Language; source: string } | null>(null);
  /** The "Pasting is blocked" toast on screen, until the next key, Run or Submit. */
  let pasteNote = 0;

  function clearPasteNote() {
    if (pasteNote === 0) return;
    dismissToast(pasteNote);
    pasteNote = 0;
  }

  const judging = new Judging({
    base: api,
    problemId: () => ws?.problem_id,
    ready: () => page.state === 'ready',
    locked: () => lockText,
    held: () => draft.pendingLanguage !== null || confirmReset || restoring !== null,
    minGapS: () => minGapS,
    code: () => ({ language: draft.language, source: draft.source }),
    integrity: () => integrity,
    submitted: () => void draft.save(),
    reveal: revealResult,
  });

  // Layout: from 1024 px three columns, from 768 px two tabs, under it three.
  let layout = $state<'wide' | 'tablet' | 'phone'>('wide');
  let tab = $state<'problem' | 'code' | 'result'>('problem');
  // Under 1024 px code is read, not written.
  const NARROW = 'Use a computer to edit, run or submit.';
  // The server says why Run and Submit are off when they are; `locked` is the fallback.
  const lockText = $derived(ws?.refusal ? ws.refusal : layout !== 'wide' ? NARROW : locked);
  // A short laptop starts with a smaller result, so the editor keeps its height.
  const panel = readResultPanel(draftStorage(), typeof window !== 'undefined' && media('short').matches ? 30 : RESULT_SHARE.default);
  let resultShare = $state(panel.share);
  let resultCollapsed = $state(panel.collapsed);
  let codeCol: HTMLDivElement | undefined = $state();
  // In a contest from 1024 px: the statement's share beside the code, or hidden.
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
  /** The selected language and the judge's time factor for it, for the limits line. */
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

  // A problem the address names wrongly (400) reads as missing, like one that is not there.
  const page = loader(
    async () => {
      const sent = Date.now();
      try {
        const data = await getJSON<Workspace>(api);
        serverClock.observe(data.server_now, sent, Date.now());
        return data;
      } catch (e) {
        throw e instanceof ApiError && e.status === 400 ? new ApiError(404, e.code, e.message) : e;
      }
    },
    (data) => {
      const restore = keepHeadingFocus();
      ws = data;
      if (label !== '') setTitle(`${label}. ${data.title}`, context);
      else setTitle(`Try ${data.title}`);
      draft.restore(data);
      judging.loaded(data);
      void restore();
      // After Start the pressed button is gone: focus goes to the problem's heading, never the body.
      void tick().then(() => {
        const a = document.activeElement;
        if (!a || a === document.body) document.querySelector<HTMLElement>('main h1')?.focus();
      });
      judging.startStream(extraEvents, onevent, onresync);
    },
  );

  async function load() {
    await page.load();
    if (page.live && page.state === 'missing') setTitle('Problem not found');
  }

  // A changed statement: reload what the student reads, never the editor.
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
      if (!page.live || !ws) return;
      ws = { ...ws, title: data.title, statement_md: data.statement_md, samples: data.samples, time_limit_ms: data.time_limit_ms, memory_limit_mb: data.memory_limit_mb };
    } catch {
      // The old statement stays; the next change or a reload brings the new one.
    }
  }

  /**
   * Shows the result pane when another tab hides it. When the tab switched
   * by itself, focus follows to the shown panel's first line, so a keyboard
   * or screen reader user lands on what changed. From 1024 px
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

  async function restore() {
    if (!restoring) return;
    const { language, source } = restoring;
    restoring = null;
    await draft.useCode(language, source);
    polite("That submission's code is in the editor.");
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

  // Editor keys pressed outside the editor say where they work.
  const EDITOR_KEYS = new Set(['/', 'd', 'l', 'h', 'g', 'u']);
  let hinted = 0;

  function onKeydown(event: KeyboardEvent) {
    if (event.defaultPrevented || page.state !== 'ready') return;
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

  // Full screen on: the contest page asked for it at Start; each exit is counted.
  function onFullscreenChange() {
    if (exam?.full_screen && !document.fullscreenElement) integrity?.fullscreenExit();
  }

  function onPageHide() {
    integrity?.flush(true);
    draft.saveOnLeave();
  }

  // During a live contest, sign out asks first (App.svelte); the draft save still runs before it.
  $effect(() => {
    if (!clock) return;
    return guardSignOut('Your draft is saved first. Your time keeps running while you are signed out.');
  });

  $effect(() => {
    draft.editor = editor;
  });

  // Compiler marks (v4 E4): a run or submission seen waiting here that ends
  // in a compile error marks its lines; the next one to wait clears them.
  // One that was already over when the page opened marks nothing, since the
  // code may have changed since. shortcut: one still waiting when the page
  // opened is marked when it ends, though the code may have changed; track
  // only what this page sent if that ever misleads.
  // Not reactive state: the effects below write it, so tracking it would loop.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  const waited = new Set<string>();
  function follow(key: string, r: (Parameters<typeof isWaiting>[0] & { compile_output: string }) | null, failed: boolean) {
    if (!r) return;
    if (isWaiting(r)) {
      if (!waited.has(key)) editor?.setMarks([]);
      waited.add(key);
    } else if (waited.delete(key)) {
      editor?.setMarks(failed ? compileMarks(r.compile_output, draft.language) : []);
    }
  }
  $effect(() => {
    const run = judging.state.run;
    follow(`run ${run?.id}`, run, !!run && runCompileFailed(run));
  });
  $effect(() => {
    const s = judging.current;
    follow(`submission ${s?.id}`, s, s?.verdict === 'CE');
  });

  // The next Run or Submit takes the paste refusal away, so it never reads as part of the result.
  $effect(() => {
    if (judging.runBusy || judging.submitBusy) clearPasteNote();
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
    document.addEventListener('fullscreenchange', onFullscreenChange);
    const flushTimer = setInterval(() => integrity?.flush(), FLUSH_MS);
    const saveTimer = setInterval(() => void draft.save(), SAVE_MS);
    const secondTimer = setInterval(() => judging.tick(), 1000);

    void load();
    const forgetSave = beforeSignOut(() => draft.save());

    return () => {
      forgetSave();
      page.stop();
      document.removeEventListener('fullscreenchange', onFullscreenChange);
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
      // The next problem starts with nothing said about this one.
      hush();
    };
  });
</script>

<svelte:window onkeydown={onKeydown} onblur={onWindowBlur} onfocus={() => integrity?.focus()} onpagehide={onPageHide} />
<svelte:document onvisibilitychange={onVisibility} />

{#if page.state !== 'ready' || !ws}
  <div class="state">
    <PageHeader compact title={page.state === 'missing' ? 'Problem not found' : 'Problem'} back={{ href: backHref, label: backLabel }} />
    {#if page.state === 'loading'}
      <PageState kind="loading" message="Loading the problem" shape="statement" />
    {:else if page.state === 'forbidden' && (page.code === 'not_started' || page.code === 'contest_over')}
      <PageState kind="missing" message={page.error} parent={{ href: contestHref || backHref, label: 'Go to the contest page.' }} />
    {:else if page.state === 'forbidden'}
      <PageState kind="forbidden" message={page.error || 'You cannot open this problem.'} ask="Ask an organiser if you think you should." />
    {:else if page.state === 'missing'}
      <PageState kind="missing" message="This problem does not exist." parent={{ href: backHref, label: 'Go back.' }} />
    {:else if page.state === 'error'}
      <PageState kind="error" message={page.error} onretry={load} />
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
      <Statement markdown={problem.statement_md} samples={problem.samples} blockCopy={exam?.block_copy ?? false} />
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
        fullscreen={!exam?.full_screen}
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
          blockPaste={exam?.block_paste ?? false}
          onpasteblocked={() => (pasteNote = showToast(PASTE_BLOCKED))}
          oncursor={(line, col) => (draft.cursor = { line, col })}
          onchange={(s) => draft.change(s)}
          ontyped={(n) => {
            integrity?.typedChars(n);
            clearPasteNote();
          }}
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
        mine={() => draft.source}
        onuse={(s, source) => (restoring = { language: s.language, source })}
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
  open={restoring !== null}
  title="Replace your code with this submission's?"
  body={restoring && restoring.language !== draft.language
    ? `The editor switches to ${languageLabel(restoring.language)} with this submission's code, and saves it. Your ${languageLabel(draft.language)} code comes back when you switch back.`
    : `Your ${languageLabel(draft.language)} code for this problem is replaced with this submission's, and saved.`}
  confirmLabel="Use this code"
  danger
  onconfirm={() => void restore()}
  oncancel={() => (restoring = null)}
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
