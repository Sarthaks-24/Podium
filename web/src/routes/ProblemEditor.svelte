<!--
  ProblemEditor: one problem at /admin/problems/{id} (design.md 5.6). The
  route loads the problem, keeps the working copy and its unsaved-changes
  guard, saves (Ctrl+S or Cmd+S), and runs validation, output generation
  and input generation, polling each every 2 s while the judge has it. The
  sections are components in components/problem/: EditorBar (the sticky
  status and actions), EditorNotes, StatementSection, TestsSection, GeneratorPanel
  (from M5, when the server sends a "generator" key), SolutionsSection
  with JudgingSection, EditorFooter, then the validation report.
  Their shared classes are in
  components/problem/editor.css. A live contest that pins the problem asks
  for a reason with each save; an AI draft carries a note until a person
  saves it. Props: `id`, the problem id from the address.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import { focusFirstInvalid } from '../lib/focus';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ValidationReport from '../components/ValidationReport.svelte';
  import { inputGenPending, inputGenReport, isAIDraft, type InputGeneration } from '../lib/ai';
  import { ApiError, errorText, getJSON, sendBody, sendJSON } from '../lib/api';
  import EditorFooter from '../components/problem/EditorFooter.svelte';
  import GeneratorPanel from '../components/problem/GeneratorPanel.svelte';
  import EditorBar from '../components/problem/EditorBar.svelte';
  import EditorNotes from '../components/problem/EditorNotes.svelte';
  import SolutionsSection from '../components/problem/SolutionsSection.svelte';
  import StatementSection from '../components/problem/StatementSection.svelte';
  import TestsSection from '../components/problem/TestsSection.svelte';
  import JudgingSection from '../components/problem/JudgingSection.svelte';
  import ReadinessPanel from '../components/problem/ReadinessPanel.svelte';
  import StatementPreview from '../components/StatementPreview.svelte';
  import { media } from '../lib/breakpoints';
  import '../components/problem/editor.css';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    editorStatus,
    fieldKey,
    imagesWithoutAlt,
    isPending,
    judgedFieldsChanged,
    previewSamples,
    ordinal,
    problemPath,
    readPlan,
    toDraft,
    toEdit,
    validationResultLine,
    pipelineLine,
    type Draft,
    type Problem,
    type Validation,
  } from '../lib/problems';
  import { fieldErrors } from '../lib/problemFields';
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { type Generation } from '../lib/workspace';
  import { editorJumps, generationSummary } from '../lib/problemEditor';

  let { id }: { id: string } = $props();

  /** How often the validation and generation are asked for while the judge has them. */
  const POLL_MS = 2000;

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

  let loadState = $state<'loading' | 'ready' | 'missing' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let problem = $state<Problem | null>(null);
  let draft = $state<Draft | null>(null);
  let baseline = $state('');
  const dirty = $derived(draft !== null && JSON.stringify(draft) !== baseline);
  // A server from M5 on sends "generator" (null when there is none).
  const hasGenerator = $derived(problem !== null && problem.generator !== undefined);
  const aiDraft = $derived(problem !== null && isAIDraft(problem.tags));

  let fields = $state<Record<string, string>>({});
  let saveError = $state('');
  /** Why the problem changed; required while a live contest pins it (C4, B2.2). */
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
  let genBusy = $state(false);
  let regenOpen = $state(false);
  let replaceOpen = $state(false);

  let archiveOpen = $state(false);
  let archiving = $state(false);
  let leaveTo = $state<string | null>(null);

  // M5: the generator, its plan, and generating inputs from it.
  let inputGen = $state<InputGeneration | null>(null);
  let inputGenError = $state<{ message: string; problems: readonly string[] } | null>(null);
  let inputGenBusy = $state(false);

  const status = $derived(problem ? editorStatus(problem, validation) : null);
  const validating = $derived(isPending(validation));
  const validateLabel = $derived.by(() => {
    if (validating && validation) {
      return validation.state === 'queued' && validation.queue_position !== null && validation.queue_position > 0
        ? `Validating, ${ordinal(validation.queue_position)} in queue`
        : 'Validating';
    }
    return dirty ? 'Save and validate' : 'Validate';
  });
  // The button that matters now is the primary one.
  const blocked = $derived((problem?.problems ?? []).length > 0);
  // While the server names what stops validation, Validate is not the next step.
  const validatePrimary = $derived(!dirty && !!problem && !blocked && !(problem.validated && validation?.current !== false));
  /** Judged fields changed and not saved (a save would need a new validation). */
  const judged = $derived(draft && baseline ? judgedFieldsChanged(JSON.parse(baseline) as Draft, draft, aiDraft) : []);
  const resultLine = $derived(validation && validation.current !== false ? validationResultLine(validation) : null);
  const pipeline = $derived(pipelineLine(hasGenerator ? inputGen : null, generation, validation));
  /** Set when a validation finishes while the editor is open: the bar then says how it went (audit O2). */
  let justFinished = $state(false);
  /** From 1280 px the preview sits in the context column beside the form. */
  let desk = $state(media('desk').matches);

  const genPending = $derived(generation !== null && (generation.state === 'queued' || generation.state === 'running'));
  const genSummary = $derived(generationSummary(generation));

  const inputPending = $derived(inputGenPending(inputGen));
  const inputReport = $derived(inputGen && problem ? inputGenReport(inputGen, readPlan(problem.plan)) : null);

  const fieldList = $derived(fieldErrors(fields, draft, hasGenerator));

  /** Handles errors every request can hit; returns true when it did. */
  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) return true;
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
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

  // Polling stops for good when the editor goes away.
  let alive = true;

  /** Polls one job of the problem every 2 s while `pending` holds; `take` gets each answer. */
  function poller<T>(path: string, pending: () => boolean, take: (value: T) => void) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function tick() {
      try {
        const value = await getJSON<T>(problemPath(id, path));
        if (!alive) return;
        take(value);
      } catch (e) {
        if (handleAuth(e)) return;
        // A failed poll is retried on the next tick.
      }
      schedule();
    }
    function schedule() {
      clearTimeout(timer);
      if (alive && pending()) timer = setTimeout(tick, POLL_MS);
    }
    return { tick, schedule, stop: () => clearTimeout(timer) };
  }

  const validationPoll = poller<Validation>('/validation', () => isPending(validation), (v) => {
    const was = validation;
    validation = v;
    if (isPending(was) && !isPending(v)) {
      justFinished = true;
      refreshProblem();
    }
  });
  const genPoll = poller<Generation>('/generation', () => genPending, (g) => {
    const wasPending = genPending;
    generation = g;
    if (wasPending && !(g.state === 'queued' || g.state === 'running')) {
      refreshProblem();
      // After generated inputs, the server queues validation next.
      if (hasGenerator && !isPending(validation)) void validationPoll.tick();
    }
  });
  const inputPoll = poller<InputGeneration>('/input-generation', () => inputPending, (g) => {
    const wasPending = inputPending;
    inputGen = g;
    if (wasPending && !inputGenPending(g)) {
      refreshProblem();
      // Output generation follows good inputs; show it and poll it.
      if (g.summary?.generating_outputs) void genPoll.tick();
    }
  });

  async function refreshProblem() {
    try {
      const p = await getJSON<Problem>(problemPath(id));
      if (alive) adopt(p, false);
    } catch (e) {
      handleAuth(e);
    }
  }

  async function load() {
    loadState = 'loading';
    try {
      const p = await getJSON<Problem>(problemPath(id));
      if (!alive) return;
      adopt(p, true);
      loadState = 'ready';
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && (e.status === 404 || e.status === 400)) {
        loadState = 'missing';
        setTitle('Problem not found');
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
      return;
    }
    try {
      validation = await getJSON<Validation>(problemPath(id, '/validation'));
      if (problem) validation = { ...validation, current: validation.content_hash === problem.content_hash };
      validationPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      validation = null;
    }
    try {
      generation = await getJSON<Generation>(problemPath(id, '/generation'));
      genPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      generation = null;
    }
    if (!hasGenerator) return;
    try {
      inputGen = await getJSON<InputGeneration>(problemPath(id, '/input-generation'));
      inputPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      inputGen = null;
    }
  }

  onMount(() => {
    setTitle(routeTitle('problem'));
    const role = session.user?.role;
    if (role === 'admin' || role === 'owner') {
      load();
    } else {
      loadState = 'forbidden';
    }
    const deskQuery = media('desk');
    const syncDesk = () => (desk = deskQuery.matches);
    deskQuery.addEventListener('change', syncDesk);
    const removeGuard = setNavigationGuard((to) => {
      if (!dirty) return true;
      leaveTo = to;
      return false;
    });
    return () => {
      alive = false;
      validationPoll.stop();
      genPoll.stop();
      inputPoll.stop();
      removeGuard();
      deskQuery.removeEventListener('change', syncDesk);
    };
  });

  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
  }

  function onKeydown(event: KeyboardEvent) {
    const mod = isMac ? event.metaKey : event.ctrlKey;
    if (mod && !event.altKey && (event.key === 's' || event.key === 'S') && draft && loadState === 'ready') {
      event.preventDefault();
      if (!saving) save();
    }
  }

  /** Saves the working copy. Resolves to true when the server took it. */
  async function save(): Promise<boolean> {
    if (!draft) return false;
    fields = {};
    saveError = '';
    const result = toEdit(draft);
    if (!result.ok) {
      fields = result.fields;
      saveError = 'Some fields need attention. They are marked above.';
      await focusFirstInvalid('.editor');
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
      if (Object.keys(next).length > 0) await focusFirstInvalid('.editor');
      return false;
    } finally {
      saving = false;
    }
  }

  function onSubmit(event: SubmitEvent) {
    event.preventDefault();
    save();
  }

  async function validate() {
    validateError = null;
    if (dirty && !(await save())) return;
    requesting = true;
    try {
      validation = await sendJSON<Validation>('POST', problemPath(id, '/validate'));
      validationPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      validateError = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    } finally {
      requesting = false;
    }
  }

  async function generate(all: boolean) {
    regenOpen = false;
    genError = null;
    if (dirty && !(await save())) return;
    genBusy = true;
    try {
      generation = await sendJSON<Generation>('POST', problemPath(id, '/generate'), { all });
      genPoll.schedule();
    } catch (e) {
      if (handleAuth(e)) return;
      genError = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    } finally {
      genBusy = false;
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

  /** Moves to the full validation report and puts focus on its heading. */
  function showReport(event: MouseEvent) {
    const el = document.getElementById('validation-heading');
    if (!el) return;
    event.preventDefault();
    el.scrollIntoView({ block: 'start' });
    el.focus({ preventScroll: true });
  }

  const jumps = $derived(editorJumps(problem, draft, hasGenerator));

  const pageTitle = $derived(
    loadState === 'missing' ? 'Problem not found' : problem ? (problem.title.trim() !== '' ? problem.title : problem.slug) : 'Problem',
  );
</script>

<svelte:window onbeforeunload={onBeforeUnload} onkeydown={onKeydown} />

<div class="editor">
  <PageHeader title={pageTitle} crumbs={[{ label: 'Problem bank', href: '/admin/problems' }, { label: pageTitle }]}>
    {#snippet actions()}
      {#if problem && loadState === 'ready'}
        <a class="btn btn-quiet" href="/admin/problems/{problem.id}/try">Try as a student</a>
      {/if}
    {/snippet}
  </PageHeader>

  {#if loadState === 'forbidden'}
    <PageState kind="forbidden" message="Only organisers can manage problems." />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no problem with that number." parent={{ href: '/admin/problems', label: 'Go to the problem bank.' }} />
  {:else if loadState === 'loading'}
    <PageState kind="loading" message="Loading problem" />
  {:else if loadState === 'error'}
    <PageState kind="error" message={loadError} onretry={load} />
  {:else if problem && draft}
    <EditorBar
      {status}
      {aiDraft}
      {dirty}
      fieldCount={fieldList.length}
      {jumps}
      {saving}
      busy={requesting || saving || validating}
      {validateLabel}
      {validatePrimary}
      result={justFinished && !dirty && !validating ? resultLine : null}
      onvalidate={validate}
      onreport={showReport}
    />

    <EditorNotes {problem} {aiDraft} bind:reason {fields} />

    <div class="page-split editor-split">
      <aside class="page-split-aside" aria-label="Readiness and preview">
        <ReadinessPanel
          {problem}
          {status}
          {judged}
          {dirty}
          {pipeline}
          result={resultLine}
          altMissing={imagesWithoutAlt(draft.statement_md)}
          onreport={showReport}
        />
        {#if desk}
          <section class="panel aside-preview" aria-labelledby="preview-heading">
            <h2 id="preview-heading">Preview</h2>
            <StatementPreview
              title={draft.title}
              markdown={draft.statement_md}
              timeLimitMs={draft.time_limit_ms}
              memoryMb={draft.memory_limit_mb}
              samples={previewSamples(problem.tests, draft.samples).samples}
              samplesCut={previewSamples(problem.tests, draft.samples).cut}
            />
          </section>
        {/if}
      </aside>

      <div class="page-split-main editor-main">
        <form id="problem-form" class="form" novalidate onsubmit={onSubmit}>
          <StatementSection bind:draft {fields} showPreview={!desk} tests={problem.tests} />

          <SolutionsSection bind:draft {fields} />

          <TestsSection
            {id}
            bind:draft
            bind:fields
            bind:fileInput={testsInput}
            tests={problem.tests}
            {replacing}
            {replaceError}
            {genBusy}
            {genPending}
            {genError}
            {genSummary}
            onupload={() => (problem && problem.tests.length > 0 ? (replaceOpen = true) : pickTests())}
            ongenerate={() => generate(false)}
            onregenerate={() => (regenOpen = true)}
            onfile={replaceTests}
          />

          {#if hasGenerator}
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

          <JudgingSection bind:draft {fields} />

          <EditorFooter {id} archived={!!problem.archived} {saveError} {fieldList} onarchive={() => (archiveOpen = true)} />
        </form>

        <section class="block validation" aria-labelledby="validation-heading">
          <h2 id="validation-heading" tabindex="-1">Validation</h2>
          {#if validateError}
            <div class="banner banner-error problems-list" role="alert">
              <p>{validateError.message}</p>
              {#if validateError.problems.length > 0}
                <ul>{#each validateError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
              {/if}
            </div>
          {/if}
          <ValidationReport {validation} />
        </section>

      </div>
    </div>
  {/if}
</div>

<ConfirmDialog
  open={archiveOpen}
  title="Archive {problem?.title ?? 'this problem'}?"
  body="It leaves the problem bank. Contests that used it keep it."
  confirmLabel="Archive problem"
  danger
  busy={archiving}
  onconfirm={archive}
  oncancel={() => (archiveOpen = false)}
/>

<ConfirmDialog
  open={replaceOpen}
  title="Replace all {problem?.tests.length ?? 0} tests?"
  body="The tests in the .zip you pick take the place of every test now on this problem. Subtask ranges stay as they are."
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
  busy={genBusy}
  onconfirm={() => generate(true)}
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

  /* The context column sits under the sticky bar, not behind it. */
  .editor-split {
    margin-top: var(--space-5);
  }

  @media (min-width: 1280px) {
    .editor-split > .page-split-aside {
      top: calc(var(--sticky-bar-h, var(--space-7)) + var(--space-4));
      max-height: calc(100dvh - var(--sticky-bar-h, var(--space-7)) - 2 * var(--space-4));
      /* A preview taller than the window scrolls in the column; without this
         its end (the sample copy buttons) could never be reached. */
      overflow-y: auto;
      overscroll-behavior: contain;
    }
  }

  /* One column that never grows past the page: wide tables scroll inside it. */
  .editor-main {
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }

  .aside-preview h2 {
    font-size: var(--fs-sub);
  }

  .validation.block {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: 0;
    border-top: 0;
    max-width: var(--measure-wide);
  }
</style>
