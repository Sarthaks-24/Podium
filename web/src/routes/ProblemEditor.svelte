<!--
  ProblemEditor: one problem at /admin/problems/{id} (design.md 5.6). The
  route loads the problem, keeps the working copy and its unsaved-changes
  guard, saves (Ctrl+S or Cmd+S), and runs validation, output generation
  and input generation, polling each every 2 s while the judge has it. The
  sections are components in components/problem/: EditorBar (the sticky
  status and actions), StatementSection, TestsSection, GeneratorPanel
  (from M5, when the server sends a "generator" key), SolutionsSection
  with JudgingSection, EditorFooter, then the validation report,
  RevisionHistory and ModelSolve. Their shared classes are in
  components/problem/editor.css. A live contest that pins the problem asks
  for a reason with each save; an AI draft carries a note until a person
  saves it. Props: `id`, the problem id from the address.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ValidationReport from '../components/ValidationReport.svelte';
  import { inputGenPending, inputGenReport, isAIDraft, type InputGeneration } from '../lib/ai';
  import { ApiError, getJSON, sendBody, sendJSON } from '../lib/api';
  import EditorFooter from '../components/problem/EditorFooter.svelte';
  import GeneratorPanel from '../components/problem/GeneratorPanel.svelte';
  import EditorBar from '../components/problem/EditorBar.svelte';
  import ModelSolve from '../components/problem/ModelSolve.svelte';
  import SolutionsSection from '../components/problem/SolutionsSection.svelte';
  import StatementSection from '../components/problem/StatementSection.svelte';
  import TestsSection from '../components/problem/TestsSection.svelte';
  import RevisionHistory from '../components/problem/RevisionHistory.svelte';
  import '../components/problem/editor.css';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    editorStatus,
    fieldKey,
    isFailure,
    isPending,
    ordinal,
    problemPath,
    readPlan,
    toDraft,
    toEdit,
    verdictLabel,
    type Draft,
    type Problem,
    type Validation,
  } from '../lib/problems';
  import { fieldErrors } from '../lib/problemFields';
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { formatMs, type Generation } from '../lib/workspace';

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
  const validatePrimary = $derived(!dirty && !!problem && !(problem.validated && validation?.current !== false));

  const genPending = $derived(generation !== null && (generation.state === 'queued' || generation.state === 'running'));
  const genSummary = $derived.by(() => {
    const g = generation;
    if (!g) return null;
    if (g.state === 'queued' || g.state === 'running') return { tone: 'wait', text: 'Generating outputs', lines: [] as string[] };
    if (g.error) return { tone: 'bad', text: g.error, lines: [] as string[] };
    if (g.results.length === 0 && g.compile_output) {
      return {
        tone: 'bad',
        text: 'The reference solution did not compile, so no outputs were generated.',
        lines: g.compile_output.split('\n').filter((l) => l.trim() !== '').slice(0, 8),
      };
    }
    const problems = g.summary?.problems ?? [];
    if (problems.length > 0) return { tone: 'bad', text: 'Some outputs were not applied.', lines: problems };
    const failed = g.results.filter((r) => isFailure(r.verdict));
    if (failed.length > 0) {
      return {
        tone: 'bad',
        text: `The reference solution failed on ${failed.length} ${failed.length === 1 ? 'test' : 'tests'}, so those outputs are still missing.`,
        lines: failed.map((r) => `${verdictLabel(r.verdict)} on test ${r.idx}`),
      };
    }
    const slowest = g.results.reduce((m, r) => Math.max(m, r.time_ms), 0);
    const n = g.summary?.applied ?? g.results.length;
    return { tone: 'ok', text: `Generated ${n} ${n === 1 ? 'output' : 'outputs'}. Slowest run ${formatMs(slowest)}.`, lines: [] as string[] };
  });

  // A server from M5 on sends "generator" (null when there is none).
  const hasGenerator = $derived(problem !== null && problem.generator !== undefined);
  const aiDraft = $derived(problem !== null && isAIDraft(problem.tags));
  const inputPending = $derived(inputGenPending(inputGen));
  const inputReport = $derived(inputGen && problem ? inputGenReport(inputGen, readPlan(problem.plan)) : null);

  const fieldList = $derived(fieldErrors(fields, draft, hasGenerator));

  /** Handles errors every request can hit; returns true when it did. */
  function handleSessionError(e: unknown): boolean {
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

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
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
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let genTimer: ReturnType<typeof setTimeout> | undefined;
  let inputTimer: ReturnType<typeof setTimeout> | undefined;

  function schedulePoll() {
    clearTimeout(pollTimer);
    if (alive && isPending(validation)) pollTimer = setTimeout(poll, POLL_MS);
  }

  async function poll() {
    const was = validation;
    try {
      const v = await getJSON<Validation>(problemPath(id, '/validation'));
      if (!alive) return;
      validation = v;
      if (isPending(was) && !isPending(v)) refreshProblem();
    } catch (e) {
      if (handleSessionError(e)) return;
      // A failed poll is retried on the next tick.
    }
    schedulePoll();
  }

  function scheduleGenPoll() {
    clearTimeout(genTimer);
    if (alive && genPending) genTimer = setTimeout(pollGeneration, POLL_MS);
  }

  async function pollGeneration() {
    const wasPending = genPending;
    try {
      const g = await getJSON<Generation>(problemPath(id, '/generation'));
      if (!alive) return;
      generation = g;
      if (wasPending && !(g.state === 'queued' || g.state === 'running')) {
        refreshProblem();
        // After generated inputs, the server queues validation next.
        if (hasGenerator && !isPending(validation)) void poll();
      }
    } catch (e) {
      if (handleSessionError(e)) return;
    }
    scheduleGenPoll();
  }

  function scheduleInputPoll() {
    clearTimeout(inputTimer);
    if (alive && inputPending) inputTimer = setTimeout(pollInputGen, POLL_MS);
  }

  async function pollInputGen() {
    const wasPending = inputPending;
    try {
      const g = await getJSON<InputGeneration>(problemPath(id, '/input-generation'));
      if (!alive) return;
      inputGen = g;
      if (wasPending && !inputGenPending(g)) {
        refreshProblem();
        // Output generation follows good inputs; show it and poll it.
        if (g.summary?.generating_outputs) void pollGeneration();
      }
    } catch (e) {
      if (handleSessionError(e)) return;
    }
    scheduleInputPoll();
  }

  async function refreshProblem() {
    try {
      const p = await getJSON<Problem>(problemPath(id));
      if (alive) adopt(p, false);
    } catch (e) {
      handleSessionError(e);
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
      if (handleSessionError(e)) return;
      if (e instanceof ApiError && (e.status === 404 || e.status === 400)) {
        loadState = 'missing';
        setTitle('Problem not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
      return;
    }
    try {
      validation = await getJSON<Validation>(problemPath(id, '/validation'));
      if (problem) validation = { ...validation, current: validation.content_hash === problem.content_hash };
      schedulePoll();
    } catch (e) {
      if (handleSessionError(e)) return;
      validation = null;
    }
    try {
      generation = await getJSON<Generation>(problemPath(id, '/generation'));
      scheduleGenPoll();
    } catch (e) {
      if (handleSessionError(e)) return;
      generation = null;
    }
    if (!hasGenerator) return;
    try {
      inputGen = await getJSON<InputGeneration>(problemPath(id, '/input-generation'));
      scheduleInputPoll();
    } catch (e) {
      if (handleSessionError(e)) return;
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
    const removeGuard = setNavigationGuard((to) => {
      if (!dirty) return true;
      leaveTo = to;
      return false;
    });
    return () => {
      alive = false;
      clearTimeout(pollTimer);
      clearTimeout(genTimer);
      clearTimeout(inputTimer);
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
    if (mod && !event.altKey && (event.key === 's' || event.key === 'S') && draft && loadState === 'ready') {
      event.preventDefault();
      if (!saving) save();
    }
  }

  async function focusFirstInvalid() {
    await tick();
    document.querySelector<HTMLElement>('.editor [aria-invalid="true"]')?.focus();
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
      await focusFirstInvalid();
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
      if (handleSessionError(e)) return false;
      const raw = e instanceof ApiError ? e.fields : {};
      const next: Record<string, string> = {};
      for (const [key, message] of Object.entries(raw)) next[fieldKey(key)] = message;
      fields = next;
      saveError = messageOf(e);
      if (Object.keys(next).length > 0) await focusFirstInvalid();
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
      schedulePoll();
    } catch (e) {
      if (handleSessionError(e)) return;
      validateError = { message: messageOf(e), problems: e instanceof ApiError ? e.problems : [] };
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
      scheduleGenPoll();
    } catch (e) {
      if (handleSessionError(e)) return;
      genError = { message: messageOf(e), problems: e instanceof ApiError ? e.problems : [] };
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
      scheduleInputPoll();
    } catch (e) {
      if (handleSessionError(e)) return;
      inputGenError = { message: messageOf(e), problems: e instanceof ApiError ? e.problems : [] };
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
      if (handleSessionError(e)) return;
      replaceError = { message: messageOf(e), problems: e instanceof ApiError ? e.problems : [] };
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
      if (!handleSessionError(e)) showToast(messageOf(e), 'error');
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

  const pageTitle = $derived(
    loadState === 'missing' ? 'Problem not found' : problem ? (problem.title.trim() !== '' ? problem.title : problem.slug) : 'Problem',
  );
</script>

<svelte:window onbeforeunload={onBeforeUnload} onkeydown={onKeydown} />

<div class="editor">
  <PageHeader title={pageTitle} crumbs={[{ label: 'Problem bank', href: '/admin/problems' }, { label: pageTitle }]}>
    {#snippet actions()}
      {#if problem && loadState === 'ready'}
        <a class="try" href="/admin/problems/{problem.id}/try">Try as a student</a>
      {/if}
    {/snippet}
  </PageHeader>

  {#if loadState === 'forbidden'}
    <PageState kind="forbidden" message="Only admins can manage problems." />
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
      {hasGenerator}
      {saving}
      busy={requesting || saving || validating}
      {validateLabel}
      {validatePrimary}
      onvalidate={validate}
    />

    {#if problem.archived}
      <div class="banner banner-info lead">
        <p>This problem is archived. It is hidden from the bank and kept for the contests that used it.</p>
      </div>
    {/if}

    {#if (problem.live_contests ?? []).length > 0}
      {@const live = problem.live_contests ?? []}
      <div class="banner banner-info lead live-note">
        <p>
          {live.map((c) => c.title).join(', ')}
          {live.length === 1 ? 'is' : 'are'} running with this problem. A save makes a new revision; students keep the revision their
          contest pinned until you update the contest on its console.
        </p>
        <div class="field reason-field">
          <label for="reason">Reason for this change</label>
          <input
            id="reason"
            class="input"
            type="text"
            maxlength="500"
            autocomplete="off"
            bind:value={reason}
            aria-invalid={fields.reason ? 'true' : undefined}
            aria-describedby="reason-help{fields.reason ? ' reason-error' : ''}"
          />
          <p id="reason-help" class="field-help">Required while a contest is running. It is kept with the revision.</p>
          {#if fields.reason}<p id="reason-error" class="field-error">{fields.reason}</p>{/if}
        </div>
      </div>
    {/if}

    {#if aiDraft}
      <div class="banner banner-info lead">
        <p>
          The AI assistant wrote this draft. Read the statement, the solutions and the tests before you use it; students see nothing until it
          is validated and added to a contest. The AI draft tag goes when you save.
        </p>
      </div>
    {/if}

    <form id="problem-form" class="form" novalidate onsubmit={onSubmit}>
      <StatementSection bind:draft {fields} />

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

      <SolutionsSection bind:draft {fields} />

      <EditorFooter {id} warnings={problem.warnings} archived={!!problem.archived} {saveError} {fieldList} onarchive={() => (archiveOpen = true)} />
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

    <section class="block history" aria-labelledby="history-heading">
      <h2 id="history-heading" tabindex="-1">History</h2>
      <RevisionHistory problemId={problem.id} history={problem} />
    </section>

    {#if hasGenerator}
      <ModelSolve {id} onsessionerror={handleSessionError} />
    {/if}
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

  .try {
    border-radius: var(--radius);
    font-weight: 500;
    white-space: nowrap;
  }

  .reason-field {
    max-width: var(--measure-form);
    margin-top: var(--space-3);
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 48rem;
  }

  .validation.block {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: 0;
    border-top: 0;
    max-width: 64rem;
  }

</style>
