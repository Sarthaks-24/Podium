<!--
  ProblemEditor: one problem at /admin/problems/{id}, in three tabs
  (Statement, Tests, Checking) under a sticky bar with Save changes and
  Validate, which becomes "Add to contest" once the problem has passed.
  What needs attention sits under the bar on every tab. Validate makes
  missing outputs first and the server validates once they arrive, so
  there is no separate Generate step. The problem loads through
  lib/loader; the page keeps the working copy with an unsaved-changes
  guard, saves on Ctrl+S or Cmd+S, and polls the judge every 2 s while it
  works. A field error on another tab switches to it first, and a live
  contest using the problem asks for a reason with each save.
-->
<script lang="ts">
  import { organises } from '../lib/roles';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import ActionMenu from '../components/ActionMenu.svelte';
  import { onMount, tick } from 'svelte';
  import { keepHeadingFocus } from '../lib/focus';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ValidationReport from '../components/ValidationReport.svelte';
  import { inputGenPending, inputGenReport, isAIDraft, type InputGeneration } from '../lib/ai';
  import { ApiError, errorText, getJSON, isSignedOut, sendBody, sendJSON } from '../lib/api';
  import GeneratorPanel from '../components/problem/GeneratorPanel.svelte';
  import EditorBar from '../components/problem/EditorBar.svelte';
  import EditorNotes from '../components/problem/EditorNotes.svelte';
  import SaveErrors from '../components/problem/SaveErrors.svelte';
  import StatementSection from '../components/problem/StatementSection.svelte';
  import TestsSection from '../components/problem/TestsSection.svelte';
  import CheckingSection from '../components/problem/CheckingSection.svelte';
  import AddToContestDialog from '../components/problem/AddToContestDialog.svelte';
  import '../components/problem/editor.css';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import { routeTitle } from '../lib/routing';
  import {
    fieldKey,
    imagesWithoutAlt,
    isPending,
    judgedFieldsChanged,
    ordinal,
    problemPath,
    readPlan,
    toDraft,
    toEdit,
    validationResultLine,
    pipelineLine,
    wordList,
    type Draft,
    type Problem,
    type Validation,
  } from '../lib/problems';
  import { fieldErrors } from '../lib/problemFields';
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { type Generation } from '../lib/workspace';
  import { generationSummary, statusLine } from '../lib/problemEditor';

  let { id }: { id: string } = $props();

  type TabId = 'statement' | 'tests' | 'checking';

  /** How often the validation and generation are asked for while the judge has them. */
  const POLL_MS = 2000;

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

  let problem = $state<Problem | null>(null);
  let draft = $state<Draft | null>(null);
  let baseline = $state('');
  const dirty = $derived(draft !== null && JSON.stringify(draft) !== baseline);
  // A current server sends "generator" (null when there is none); an older one leaves it out.
  const hasGenerator = $derived(problem !== null && problem.generator !== undefined);
  const aiDraft = $derived(problem !== null && isAIDraft(problem.tags));
  const isOrganiser = $derived(organises(session.user?.role));

  const page = loader(
    () => getJSON<Problem>(problemPath(id)),
    (p) => adopt(p, true),
  );

  let tab = $state<TabId>('statement');

  let fields = $state<Record<string, string>>({});
  let saveError = $state('');
  /** Why the problem changed; required while a live contest uses it. */
  let reason = $state('');
  let saving = $state(false);

  let validation = $state<Validation | null>(null);
  let validateError = $state<{ message: string; problems: readonly string[] } | null>(null);
  let requesting = $state(false);

  let testsInput: HTMLInputElement | undefined = $state();
  let replacing = $state(false);
  let replaceError = $state<{ message: string; problems: readonly string[] } | null>(null);

  let generation = $state<Generation | null>(null);
  let genError = $state<{ message: string; problems: readonly string[] } | null>(null);
  /** Set when this page started an output generation, so its result is said here. */
  let genStarted = $state(false);
  /** Set while an output generation runs that a validation follows (Validate, or generated inputs). */
  let chained = $state(false);
  let regenOpen = $state(false);
  let replaceOpen = $state(false);

  let archiveOpen = $state(false);
  let archiving = $state(false);
  let addOpen = $state(false);
  let leaveTo = $state<string | null>(null);

  // The generator, its plan, and generating inputs from it.
  let inputGen = $state<InputGeneration | null>(null);
  let inputGenError = $state<{ message: string; problems: readonly string[] } | null>(null);
  let inputGenBusy = $state(false);

  const genPending = $derived(generation !== null && (generation.state === 'queued' || generation.state === 'running'));
  const genSummary = $derived(genStarted ? generationSummary(generation) : null);
  const inputPending = $derived(inputGenPending(inputGen));
  const inputReport = $derived(inputGen && problem ? inputGenReport(inputGen, readPlan(problem.plan)) : null);

  const validating = $derived(isPending(validation));
  const status = $derived(problem ? statusLine(problem, validation, chained && genPending) : null);
  const missingOutputs = $derived((problem?.tests ?? []).some((t) => t.output_missing));
  /** What stops validation; the line on missing outputs (always the last) is left out, since Validate makes them. */
  const blockers = $derived.by(() => {
    const all = problem?.problems ?? [];
    return missingOutputs ? all.slice(0, -1) : all;
  });
  /** Judged fields changed and not saved (a save would need a new validation). */
  const judged = $derived(draft && baseline ? judgedFieldsChanged(JSON.parse(baseline) as Draft, draft, aiDraft) : []);
  const ready = $derived(
    !!problem && problem.validated && !problem.archived && !dirty && validation?.current !== false && !validating && !genPending,
  );
  const validatePrimary = $derived(!dirty && !!problem && blockers.length === 0 && !ready);
  const validateLabel = $derived.by(() => {
    if (chained && genPending) return 'Making outputs';
    if (validating && validation) {
      return validation.state === 'queued' && validation.queue_position !== null && validation.queue_position > 0
        ? `Validating, ${ordinal(validation.queue_position)} in queue`
        : 'Validating';
    }
    return dirty ? 'Save and validate' : 'Validate';
  });
  const resultLine = $derived(validation && validation.current !== false ? validationResultLine(validation) : null);
  // The chain line is for a chain only: Validate's outputs, or a generator's inputs and what follows.
  const pipeline = $derived(chained || inputPending ? pipelineLine(hasGenerator ? inputGen : null, generation, validation) : '');
  /** Set when a validation finishes while the editor is open: the result then takes one line under the bar. */
  let justFinished = $state(false);

  const fieldList = $derived(fieldErrors(fields, draft, hasGenerator));

  /** Advice that does not stop validation, with a line for statement images that have no description. */
  const warnings = $derived.by(() => {
    if (!problem || !draft) return [];
    const n = imagesWithoutAlt(draft.statement_md);
    if (n === 0) return problem.warnings;
    return [
      ...problem.warnings,
      `${n === 1 ? 'An image' : `${n} images`} in the statement ${n === 1 ? 'has' : 'have'} no description. Write one between the square brackets, as in ![A graph of the input](graph.png), so screen readers can read ${n === 1 ? 'it' : 'them'}.`,
    ];
  });

  const tabs = $derived([
    { id: 'statement' as const, label: 'Statement' },
    { id: 'tests' as const, label: 'Tests', count: problem?.tests.length ?? 0 },
    { id: 'checking' as const, label: 'Checking' },
  ]);

  /** Handles errors every side request can hit; returns true when it did. */
  function handleAuth(e: unknown): boolean {
    if (isSignedOut(e)) return true;
    if (e instanceof ApiError && e.status === 403) {
      page.forbid();
      return true;
    }
    return false;
  }

  /**
   * Takes a fresh copy of the problem from the server. The working copy is
   * replaced only when it has no unsaved changes, or when `replace` is set
   * (after a save, which sent the working copy).
   */
  function adopt(p: Problem, replace: boolean) {
    const keep = !replace && dirty;
    problem = p;
    const saved = toDraft(p);
    baseline = JSON.stringify(saved);
    if (!keep) draft = saved;
    if (validation) validation = { ...validation, current: validation.content_hash === p.content_hash };
    setTitle(p.title, routeTitle('problems'));
  }

  /** Polls one job of the problem every 2 s while `pending` holds; `take` gets each answer. */
  function poller<T>(path: string, pending: () => boolean, take: (value: T) => void) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function tick() {
      try {
        const value = await getJSON<T>(problemPath(id, path));
        if (!page.live) return;
        take(value);
      } catch (e) {
        if (handleAuth(e)) return;
        // A failed poll is tried again on the next tick.
      }
      schedule();
    }
    function schedule() {
      clearTimeout(timer);
      if (page.live && pending()) timer = setTimeout(tick, POLL_MS);
    }
    return { tick, schedule, stop: () => clearTimeout(timer) };
  }

  const validationPoll = poller<Validation>('/validation', () => isPending(validation), (v) => {
    const was = validation;
    validation = v;
    if (isPending(was) && !isPending(v)) {
      justFinished = true;
      void refreshProblem();
    }
  });
  const genPoll = poller<Generation>('/generation', () => genPending, (g) => {
    const wasPending = genPending;
    generation = g;
    if (wasPending && !(g.state === 'queued' || g.state === 'running')) {
      chained = false;
      void refreshProblem();
      // When every output arrived, the server queued the validation next.
      if (g.summary?.validating && !isPending(validation)) void validationPoll.tick();
    }
  });
  const inputPoll = poller<InputGeneration>('/input-generation', () => inputPending, (g) => {
    const wasPending = inputPending;
    inputGen = g;
    if (wasPending && !inputGenPending(g)) {
      void refreshProblem();
      // Output generation follows good inputs; show it and poll it.
      if (g.summary?.generating_outputs) {
        genStarted = true;
        chained = true;
        void genPoll.tick();
      }
    }
  });

  async function refreshProblem() {
    try {
      const p = await getJSON<Problem>(problemPath(id));
      if (page.live) adopt(p, false);
    } catch (e) {
      handleAuth(e);
    }
  }

  /** Fetches one job's latest state; null when there is none yet. */
  async function latest<T>(path: string): Promise<T | null> {
    try {
      return await getJSON<T>(problemPath(id, path));
    } catch (e) {
      handleAuth(e);
      return null;
    }
  }

  async function load() {
    const restore = keepHeadingFocus();
    await page.load();
    void restore();
    if (page.state === 'missing') setTitle('Problem not found');
    if (page.state !== 'ready' || !problem) return;
    const v = await latest<Validation>('/validation');
    if (!page.live) return;
    validation = v && problem ? { ...v, current: v.content_hash === problem.content_hash } : null;
    validationPoll.schedule();
    generation = await latest<Generation>('/generation');
    genPoll.schedule();
    if (!hasGenerator || !page.live) return;
    inputGen = await latest<InputGeneration>('/input-generation');
    inputPoll.schedule();
  }

  onMount(() => {
    setTitle(routeTitle('problem'));
    if (!isOrganiser) page.forbid();
    else void load();
    const removeGuard = setNavigationGuard((to) => {
      if (!dirty) return true;
      leaveTo = to;
      return false;
    });
    return () => {
      page.stop();
      validationPoll.stop();
      genPoll.stop();
      inputPoll.stop();
      removeGuard();
    };
  });

  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
  }

  function onKeydown(event: KeyboardEvent) {
    const mod = isMac ? event.metaKey : event.ctrlKey;
    if (mod && !event.altKey && (event.key === 's' || event.key === 'S') && draft && page.state === 'ready') {
      event.preventDefault();
      if (!saving) void save();
    }
  }

  /** Shows the tab that holds an element, unfolds what hides it, and focuses it. */
  async function reveal(elementId: string) {
    const el = document.getElementById(elementId);
    if (!el) return;
    const panel = el.closest<HTMLElement>('[role="tabpanel"][data-tab]');
    if (panel) tab = panel.dataset.tab as TabId;
    const folded = el.closest('details');
    if (folded) folded.open = true;
    await tick();
    el.scrollIntoView({ block: 'center' });
    el.focus({ preventScroll: true });
  }

  /** After a refused save: the first field marked invalid, on whichever tab, or else the list of errors. */
  async function focusInvalid() {
    await tick();
    const el = document.querySelector<HTMLElement>('.editor [aria-invalid="true"]');
    if (el?.id) await reveal(el.id);
    else document.getElementById('field-errors')?.focus();
  }

  /** Saves the working copy. Resolves to true when the server took it. */
  async function save(): Promise<boolean> {
    if (!draft) return false;
    fields = {};
    saveError = '';
    const result = toEdit(draft);
    if (!result.ok) {
      fields = result.fields;
      saveError = 'Some fields need attention. Each is linked here.';
      await focusInvalid();
      return false;
    }
    saving = true;
    try {
      const live = (problem?.live_contests ?? []).length > 0;
      const body = live ? { ...result.edit, reason: reason.trim() } : result.edit;
      const p = await sendJSON<Problem>('PUT', problemPath(id), body);
      adopt(p, true);
      reason = '';
      showToast('Saved');
      return true;
    } catch (e) {
      if (handleAuth(e)) return false;
      const raw = e instanceof ApiError ? e.fields : {};
      const next: Record<string, string> = {};
      for (const [key, message] of Object.entries(raw)) next[fieldKey(key)] = message;
      fields = next;
      saveError = errorText(e);
      if (Object.keys(next).length > 0) await focusInvalid();
      return false;
    } finally {
      saving = false;
    }
  }

  function onSubmit(event: SubmitEvent) {
    event.preventDefault();
    void save();
  }

  /** Validates; missing outputs are made from the reference first, then the server validates. */
  async function validate() {
    validateError = null;
    genError = null;
    justFinished = false;
    if (dirty && !(await save())) return;
    if (!problem) return;
    requesting = true;
    try {
      if (problem.tests.some((t) => t.output_missing)) {
        generation = await sendJSON<Generation>('POST', problemPath(id, '/generate'), { then_validate: true });
        genStarted = true;
        chained = true;
        genPoll.schedule();
      } else {
        validation = await sendJSON<Validation>('POST', problemPath(id, '/validate'));
        validationPoll.schedule();
      }
    } catch (e) {
      if (handleAuth(e)) return;
      validateError = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    } finally {
      requesting = false;
    }
  }

  async function regenerate() {
    regenOpen = false;
    genError = null;
    if (dirty && !(await save())) return;
    try {
      generation = await sendJSON<Generation>('POST', problemPath(id, '/generate'), { all: true });
      genStarted = true;
      genPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      genError = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    }
  }

  async function generateInputs() {
    inputGenError = null;
    if (dirty && !(await save())) return;
    inputGenBusy = true;
    try {
      inputGen = await sendJSON<InputGeneration>('POST', problemPath(id, '/generate-inputs'));
      inputPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      inputGenError = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    } finally {
      inputGenBusy = false;
    }
  }

  function pickTests() {
    testsInput?.click();
  }

  async function replaceTests() {
    const file = testsInput?.files?.[0];
    if (!testsInput || !file) return;
    testsInput.value = '';
    replaceError = null;
    replacing = true;
    const first = problem?.tests.length === 0;
    try {
      const p = await sendBody<Problem>('PUT', problemPath(id, '/tests'), file, 'application/zip');
      adopt(p, false);
      const count = `${p.tests.length} ${p.tests.length === 1 ? 'test' : 'tests'} stored.`;
      showToast(first ? `Uploaded tests. ${count}` : `Replaced tests. ${count}`);
    } catch (e) {
      if (handleAuth(e)) return;
      replaceError = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    } finally {
      replacing = false;
    }
  }

  function downloadPack() {
    const a = document.createElement('a');
    a.href = problemPath(id, '/export');
    a.download = '';
    document.body.append(a);
    a.click();
    a.remove();
  }

  async function archive() {
    archiving = true;
    try {
      await sendJSON<void>('DELETE', problemPath(id));
      archiveOpen = false;
      baseline = JSON.stringify(draft);
      showToast('Archived');
      navigate('/admin/problems');
    } catch (e) {
      archiveOpen = false;
      if (!handleAuth(e)) showToast(errorText(e), 'error');
    } finally {
      archiving = false;
    }
  }

  function leave() {
    const to = leaveTo;
    leaveTo = null;
    if (to === null) return;
    baseline = JSON.stringify(draft);
    navigate(to);
  }

  /** Moves to the validation report on the Checking tab and puts focus on its heading. */
  async function showReport(event: MouseEvent) {
    event.preventDefault();
    await reveal('validation-heading');
  }

  const menu = $derived([
    { label: 'Download pack', onselect: downloadPack },
    ...((problem?.tests.length ?? 0) > 0 ? [{ label: 'Regenerate all outputs', onselect: () => (regenOpen = true) }] : []),
    ...(problem && !problem.archived ? [{ label: 'Archive', onselect: () => (archiveOpen = true), danger: true }] : []),
  ]);

  const pageTitle = $derived(
    page.state === 'missing' ? 'Problem not found' : problem ? (problem.title.trim() !== '' ? problem.title : problem.slug) : 'Problem',
  );
</script>

<svelte:window onbeforeunload={onBeforeUnload} onkeydown={onKeydown} />

{#snippet generatorPart()}
  {#if draft && problem}
    <GeneratorPanel
      bind:draft
      bind:fields
      testIdx={problem.tests.map((t) => t.idx)}
      {dirty}
      {saving}
      busy={inputGenBusy}
      pending={inputPending}
      error={inputGenError}
      report={inputReport}
      ongenerate={generateInputs}
    />
  {/if}
{/snippet}

<div class="editor">
  <PageHeader title={pageTitle} crumbs={[{ label: 'Problems', href: '/admin/problems' }, { label: pageTitle }]}>
    {#snippet actions()}
      {#if problem && page.state === 'ready'}
        {#if status}
          <a class="status tone-{status.tone}" href="#validation-heading" onclick={showReport}>
            {#if status.tone === 'ok'}<span class="dot" aria-hidden="true"></span>{/if}{status.text}
          </a>
        {/if}
        <a class="btn btn-quiet" href="/admin/problems/{problem.id}/try">Try as a student</a>
        <ActionMenu id="problem-menu" items={menu} />
      {/if}
    {/snippet}
  </PageHeader>

  {#if page.state === 'forbidden'}
    <PageState kind="forbidden" message="Only organisers can manage problems." />
  {:else if page.state === 'missing'}
    <PageState kind="missing" message="There is no problem with that number." parent={{ href: '/admin/problems', label: 'Go to Problems.' }} />
  {:else if page.state === 'loading'}
    <PageState kind="loading" message="Loading problem" />
  {:else if page.state === 'error'}
    <PageState kind="error" message={page.error} onretry={load} />
  {:else if problem && draft}
    <EditorNotes {problem} {aiDraft} bind:reason {fields} />

    <EditorBar
      {tab}
      {tabs}
      ontab={(t) => (tab = t)}
      {aiDraft}
      {dirty}
      fieldCount={fieldList.length}
      {saving}
      busy={requesting || saving || validating || genPending || inputPending}
      {validateLabel}
      {validatePrimary}
      {ready}
      onvalidate={validate}
      onadd={() => (addOpen = true)}
    />

    <div class="notes">
      <SaveErrors {saveError} {fieldList} onreveal={reveal} />
      {#if validateError}
        <div class="banner banner-error problems-list" role="alert">
          <p>{validateError.message}</p>
          {#if validateError.problems.length > 0}
            <ul>{#each validateError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
          {/if}
        </div>
      {:else if blockers.length > 0}
        <div class="blockers" role="group" aria-labelledby="blockers-title">
          <h2 id="blockers-title" class="group-title tone-bad">Fix {blockers.length === 1 ? 'this' : 'these'} before you validate</h2>
          <ul>{#each blockers as line, i (i)}<li>{line}</li>{/each}</ul>
        </div>
      {/if}
      {#if judged.length > 0}
        <p class="line tone-part">
          {problem.validated
            ? `Saving needs a new validation: you changed the ${wordList(judged)}.`
            : `Your changes to the ${wordList(judged)} are not saved yet.`}
        </p>
      {/if}
      <div class="generation" aria-live="polite">
        {#if genError}
          <div class="banner banner-error problems-list">
            <p>{genError.message}</p>
            {#if genError.problems.length > 0}
              <ul>{#each genError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
            {/if}
          </div>
        {:else if pipeline}
          <p class="line pipeline">{pipeline}</p>
        {:else if genSummary}
          <p class="gen-line tone-{genSummary.tone}">{genSummary.text}</p>
          {#if genSummary.lines.length > 0}
            <ul class="gen-failures">{#each genSummary.lines as line, i (i)}<li>{line}</li>{/each}</ul>
          {/if}
        {/if}
      </div>
      {#if justFinished && !dirty && !validating && resultLine}
        <p class="line bar-result">
          <span class="tone-{resultLine.tone}">{resultLine.text}</span>
          <a href="#validation-heading" onclick={showReport}>See the report</a>
        </p>
      {/if}
    </div>

    <form id="problem-form" class="form" novalidate onsubmit={onSubmit}>
      <div id="panel-statement" class="tab-panel" role="tabpanel" aria-labelledby="tab-statement" data-tab="statement" hidden={tab !== 'statement'}>
        <StatementSection bind:draft {fields} tests={problem.tests} />
      </div>

      <div id="panel-tests" class="tab-panel" role="tabpanel" aria-labelledby="tab-tests" data-tab="tests" hidden={tab !== 'tests'}>
        <TestsSection
          {id}
          bind:draft
          bind:fields
          bind:fileInput={testsInput}
          tests={problem.tests}
          {replacing}
          {replaceError}
          onupload={() => (problem && problem.tests.length > 0 ? (replaceOpen = true) : pickTests())}
          onfile={replaceTests}
        />
      </div>

      <div id="panel-checking" class="tab-panel" role="tabpanel" aria-labelledby="tab-checking" data-tab="checking" hidden={tab !== 'checking'}>
        <div class="checking-layout">
          <CheckingSection bind:draft {fields} generator={hasGenerator ? generatorPart : undefined} />

          <section class="validation" aria-labelledby="validation-heading">
            <h2 id="validation-heading" tabindex="-1">Validation</h2>
            <ValidationReport {validation} onreveal={reveal} />
            {#if warnings.length > 0}
              {@const passed = problem.validated && judged.length === 0}
              <div class="advice" role="note" aria-labelledby="warnings-title">
                <h3 id="warnings-title" class="group-title">{passed ? 'Worth adding' : 'Before you validate'}</h3>
                <ul>{#each warnings as warning, i (i)}<li>{warning}</li>{/each}</ul>
              </div>
            {/if}
          </section>
        </div>
      </div>
    </form>
  {/if}
</div>

{#if problem}
  <AddToContestDialog
    open={addOpen}
    problemId={problem.id}
    title={problem.title}
    onclose={() => (addOpen = false)}
    ondone={(contest, label) => {
      addOpen = false;
      showToast(`Added to ${contest} as problem ${label}`);
    }}
  />
{/if}

<ConfirmDialog
  open={archiveOpen}
  title="Archive {problem?.title ?? 'this problem'}?"
  body="It leaves the list of problems. Contests that used it keep it."
  confirmLabel="Archive problem"
  danger
  busy={archiving}
  onconfirm={archive}
  oncancel={() => (archiveOpen = false)}
/>

<ConfirmDialog
  open={replaceOpen}
  title="Replace all {problem?.tests.length ?? 0} tests?"
  body="The tests in the .zip you pick take the place of every test now on this problem. Roles are kept by test number."
  confirmLabel="Replace tests"
  danger
  onconfirm={() => {
    replaceOpen = false;
    pickTests();
  }}
  oncancel={() => (replaceOpen = false)}
/>

<ConfirmDialog
  open={regenOpen}
  title="Regenerate all outputs?"
  body="Every expected output is replaced by what the reference solution prints. Validate again afterwards."
  confirmLabel="Regenerate all outputs"
  onconfirm={regenerate}
  oncancel={() => (regenOpen = false)}
/>

<ConfirmDialog
  open={leaveTo !== null}
  title="Leave without saving?"
  body="Your changes to this problem will be lost."
  confirmLabel="Leave page"
  danger
  onconfirm={leave}
  oncancel={() => (leaveTo = null)}
/>

<style>
  .editor {
    min-width: 0;
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-right: var(--space-2);
    border-radius: var(--radius);
    font-weight: 500;
    text-decoration: none;
  }

  .status:hover {
    text-decoration: underline;
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: currentColor;
  }

  .tone-ok {
    color: var(--ok);
  }

  .tone-bad {
    color: var(--bad);
  }

  .tone-wait,
  .tone-note {
    color: var(--ink-muted);
  }

  .tone-part {
    color: var(--part);
    font-weight: 500;
  }

  .notes {
    display: grid;
    gap: var(--space-2);
    max-width: var(--measure-wide);
    margin-top: var(--space-3);
  }

  .line {
    max-width: var(--measure-prose);
  }

  .pipeline {
    color: var(--ink-muted);
  }

  .bar-result {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--space-3);
    font-weight: 500;
  }

  .blockers,
  .advice {
    display: grid;
    gap: var(--space-1);
  }

  .group-title {
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .blockers ul,
  .advice ul {
    padding-left: var(--space-5);
  }

  .tab-panel {
    margin-top: var(--space-4);
    min-width: 0;
  }

  .validation {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
    max-width: var(--measure-wide);
  }

  .validation h2 {
    font-size: var(--fs-3);
  }

  /* From 1280 px the report sits beside the programs, so the tab stays short. */
  @media (min-width: 1280px) {
    .checking-layout {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      gap: var(--space-7);
      align-items: start;
    }

    .validation {
      margin-top: 0;
      padding-top: 0;
      border-top: 0;
    }
  }
</style>
