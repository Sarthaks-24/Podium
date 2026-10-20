<!--
  ProblemEditor: one problem at /admin/problems/{id} (design.md 5.6, with
  the M2 UI review applied). The header has a link back to the bank, the
  title and a "Try as a student" link. Under it a sticky bar keeps the
  status in words ("Validated 16 Oct 19:42", "Validation failed, 2
  problems", "Validating, 3rd in queue", "Not validated", each a link to
  the report), "Unsaved changes", the count of fields that need attention,
  jump links to each section, Save changes and Validate ("Save and
  validate" when there are unsaved changes, "Validating" with the queue
  position while the judge has it); the button that matters now is the
  primary one. Ctrl+S (Cmd+S) saves.

  Below: the details form (title, slug, Markdown statement that grows with
  its text, limits, checker, tags, difficulty) beside a live preview; under
  1024 px the preview follows the form, and under 768 px the statement has
  Write and Preview tabs instead. Then the tests: sample numbers, subtask
  rows, a one-line summary of the stored tests computed from the subtasks
  as typed, tests in no subtask and tests without an expected output named
  outside the table, the table itself in a disclosure, "Upload tests" (or
  "Replace tests") from a .zip, and "Generate outputs" for missing outputs
  or "Regenerate all outputs" after a confirmation, with the result shown
  in place. Then the solutions (reference and brute force side by side
  from 1440 px), the warnings, Download pack and Archive problem, and the
  validation report, polled every 2 s while the judge has the job. Server
  field errors appear next to their fields and as a list of links above
  the actions. Leaving with unsaved changes asks first.

  From M5 (when the server sends a "generator" key) there is a Generator
  section between the tests and the solutions: the generator program, its
  arguments per subtask, and the plan as a compact table of test, subtask,
  seed and arguments with rows to add and remove, then "Generate inputs"
  with its result in place (failures name the test, subtask and seed); good
  inputs chain into output generation and validation, each shown where it
  already is. A problem the wizard made carries an "AI draft" tag in the
  bar and a note until a person saves it. Last, "Can a model solve this?"
  (components/problem/ModelSolve). Props: `id`, the problem id from the
  address.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { media } from '../lib/breakpoints';
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ProgramField from '../components/ProgramField.svelte';
  import StatementPreview from '../components/StatementPreview.svelte';
  import ValidationReport from '../components/ValidationReport.svelte';
  import { inputGenPending, inputGenReport, isAIDraft, type InputGeneration } from '../lib/ai';
  import { ApiError, getJSON, sendBody, sendJSON } from '../lib/api';
  import EditorFooter from '../components/problem/EditorFooter.svelte';
  import GeneratorPanel from '../components/problem/GeneratorPanel.svelte';
  import ModelSolve from '../components/problem/ModelSolve.svelte';
  import TestsSection from '../components/problem/TestsSection.svelte';
  import RevisionHistory from '../components/problem/RevisionHistory.svelte';
  import '../components/problem/editor.css';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    CHECKERS,
    DIFFICULTIES,
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
  import { describedBy, fieldErrors, programError as programErrorOf } from '../lib/problemFields';
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

  let phone = $state(false);
  let statementTab = $state<'write' | 'preview'>('write');

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

  function programError(role: 'reference' | 'brute' | 'validator' | 'generator'): string {
    return programErrorOf(fields, role);
  }

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, help = ''): string | undefined {
    return describedBy(fields, key, help);
  }

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
    const query = media('tablet');
    const sync = () => {
      phone = !query.matches;
      if (!phone) statementTab = 'write';
    };
    sync();
    query.addEventListener('change', sync);
    return () => {
      alive = false;
      clearTimeout(pollTimer);
      clearTimeout(genTimer);
      clearTimeout(inputTimer);
      removeGuard();
      query.removeEventListener('change', sync);
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

  async function addProgram(role: 'brute' | 'validator') {
    if (!draft) return;
    draft[role] = { language: 'python3', source: '' };
    await tick();
    document.getElementById(`${role}-source`)?.focus();
  }

  async function removeProgram(role: 'brute' | 'validator') {
    if (!draft) return;
    draft[role] = null;
    await tick();
    document.getElementById(`add-${role}`)?.focus();
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

  // Statement tabs under 768 px follow the ARIA tabs pattern.
  function onStatementTabKey(event: KeyboardEvent) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    statementTab = statementTab === 'write' ? 'preview' : 'write';
    tick().then(() => document.getElementById(`statement-tab-${statementTab}`)?.focus());
  }

  /** Grows a textarea with its content, between its rows and 40 lines. */
  function autosize(node: HTMLTextAreaElement, value: string) {
    let last = value;
    const fit = () => {
      node.style.height = 'auto';
      const line = parseFloat(getComputedStyle(node).lineHeight) || 20;
      node.style.height = `${Math.min(node.scrollHeight + 2, line * 40)}px`;
    };
    requestAnimationFrame(fit);
    return {
      update(next: string) {
        if (next === last) return;
        last = next;
        fit();
      },
    };
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
    <div class="action-bar">
      <div class="bar-status">
        {#if status}
          <a class="status tone-{status.tone}" href="#validation-heading">
            {#if status.tone === 'ok'}<span class="dot" aria-hidden="true"></span>{/if}{status.text}
          </a>
        {/if}
        {#if aiDraft}<span class="ai-draft">AI draft</span>{/if}
        {#if dirty}<span class="unsaved">Unsaved changes</span>{/if}
        {#if fieldList.length > 0}
          <a class="error-count" href="#field-errors">
            {fieldList.length} {fieldList.length === 1 ? 'field needs' : 'fields need'} attention
          </a>
        {/if}
      </div>
      <nav class="jumps" aria-label="Sections">
        <a href="#details-heading">Details</a>
        <a href="#tests-heading">Tests</a>
        {#if hasGenerator}<a href="#generator-heading">Generator</a>{/if}
        <a href="#solutions-heading">Solutions</a>
        <a href="#validation-heading">Validation</a>
        <a href="#history-heading">History</a>
      </nav>
      <div class="bar-actions">
        <button type="submit" form="problem-form" class="btn {dirty ? 'btn-primary' : 'btn-secondary'}" disabled={saving}>
          Save changes
        </button>
        <button
          type="button"
          class="btn {validatePrimary ? 'btn-primary' : 'btn-secondary'}"
          disabled={requesting || saving || validating}
          onclick={validate}
        >
          {validateLabel}
        </button>
      </div>
    </div>

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
      <div class="top">
        <section class="details" aria-labelledby="details-heading">
          <h2 id="details-heading" tabindex="-1">Details</h2>

          <div class="field">
            <label for="title">Title</label>
            <input
              id="title"
              class="input"
              type="text"
              autocomplete="off"
              bind:value={draft.title}
              aria-invalid={err('title') ? 'true' : undefined}
              aria-describedby={described('title')}
            />
            {#if err('title')}<p id="title-error" class="field-error">{err('title')}</p>{/if}
          </div>

          <div class="field">
            <label for="slug">Slug</label>
            <input
              id="slug"
              class="input mono"
              type="text"
              autocomplete="off"
              autocapitalize="none"
              spellcheck="false"
              bind:value={draft.slug}
              aria-invalid={err('slug') ? 'true' : undefined}
              aria-describedby={described('slug', 'slug-help')}
            />
            <p id="slug-help" class="field-help">Lower case letters, digits and hyphens. Used in pack file names.</p>
            {#if err('slug')}<p id="slug-error" class="field-error">{err('slug')}</p>{/if}
          </div>

          <div class="field">
            <div class="statement-head">
              <label for="statement_md">Statement</label>
              {#if phone}
                <div class="mini-tabs" role="tablist" aria-label="Statement">
                  {#each [{ id: 'write', label: 'Write' }, { id: 'preview', label: 'Preview' }] as t (t.id)}
                    <button
                      type="button"
                      role="tab"
                      id="statement-tab-{t.id}"
                      class="mini-tab"
                      aria-selected={statementTab === t.id}
                      aria-controls="statement-panel-{t.id}"
                      tabindex={statementTab === t.id ? 0 : -1}
                      onclick={() => (statementTab = t.id as 'write' | 'preview')}
                      onkeydown={onStatementTabKey}
                    >
                      {t.label}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>
            <div
              id="statement-panel-write"
              class="statement-write"
              hidden={phone && statementTab === 'preview'}
              role={phone ? 'tabpanel' : undefined}
              aria-labelledby={phone ? 'statement-tab-write' : undefined}
            >
              <textarea
                id="statement_md"
                class="input textarea-code statement-input"
                rows="12"
                spellcheck="true"
                bind:value={draft.statement_md}
                use:autosize={draft.statement_md}
                aria-invalid={err('statement_md') ? 'true' : undefined}
                aria-describedby={described('statement_md', 'statement-help')}
              ></textarea>
              <p id="statement-help" class="field-help">Markdown. Write maths between dollar signs: $n \le 10^5$, or $$ on its own lines for a block.</p>
              {#if err('statement_md')}<p id="statement_md-error" class="field-error">{err('statement_md')}</p>{/if}
            </div>
            {#if phone && statementTab === 'preview'}
              <div id="statement-panel-preview" class="preview-body inline-preview" role="tabpanel" aria-labelledby="statement-tab-preview">
                <StatementPreview
                  title={draft.title}
                  markdown={draft.statement_md}
                  timeLimitMs={draft.time_limit_ms}
                  memoryMb={draft.memory_limit_mb}
                />
              </div>
            {/if}
          </div>

          <div class="row">
            <div class="field">
              <label for="time_limit_ms">Time limit (ms)</label>
              <input
                id="time_limit_ms"
                class="input mono"
                type="number"
                inputmode="numeric"
                min="100"
                max="10000"
                step="100"
                bind:value={draft.time_limit_ms}
                aria-invalid={err('time_limit_ms') ? 'true' : undefined}
                aria-describedby={described('time_limit_ms')}
              />
              {#if err('time_limit_ms')}<p id="time_limit_ms-error" class="field-error">{err('time_limit_ms')}</p>{/if}
            </div>
            <div class="field">
              <label for="memory_limit_mb">Memory (MB)</label>
              <input
                id="memory_limit_mb"
                class="input mono"
                type="number"
                inputmode="numeric"
                min="16"
                max="1024"
                bind:value={draft.memory_limit_mb}
                aria-invalid={err('memory_limit_mb') ? 'true' : undefined}
                aria-describedby={described('memory_limit_mb')}
              />
              {#if err('memory_limit_mb')}<p id="memory_limit_mb-error" class="field-error">{err('memory_limit_mb')}</p>{/if}
            </div>
          </div>

          <div class="row">
            <div class="field">
              <label for="checker">Checker</label>
              <select
                id="checker"
                class="select"
                bind:value={draft.checker}
                aria-invalid={err('checker') ? 'true' : undefined}
                aria-describedby={described('checker')}
              >
                {#each CHECKERS as c (c.id)}<option value={c.id}>{c.label}</option>{/each}
              </select>
              {#if err('checker')}<p id="checker-error" class="field-error">{err('checker')}</p>{/if}
            </div>
            {#if draft.checker === 'float'}
              <div class="field">
                <label for="epsilon">Allowed error</label>
                <input
                  id="epsilon"
                  class="input mono"
                  type="number"
                  inputmode="decimal"
                  min="0"
                  step="any"
                  bind:value={draft.epsilon}
                  aria-invalid={err('epsilon') ? 'true' : undefined}
                  aria-describedby={described('epsilon', 'epsilon-help')}
                />
                <p id="epsilon-help" class="field-help">Absolute or relative, for example 0.000001.</p>
                {#if err('epsilon')}<p id="epsilon-error" class="field-error">{err('epsilon')}</p>{/if}
              </div>
            {/if}
          </div>

          <div class="row">
            <div class="field grow">
              <label for="tags">Tags</label>
              <input
                id="tags"
                class="input"
                type="text"
                autocomplete="off"
                bind:value={draft.tags}
                aria-invalid={err('tags') ? 'true' : undefined}
                aria-describedby={described('tags', 'tags-help')}
              />
              <p id="tags-help" class="field-help">Separate tags with commas.</p>
              {#if err('tags')}<p id="tags-error" class="field-error">{err('tags')}</p>{/if}
            </div>
            <div class="field">
              <label for="difficulty">Difficulty</label>
              <select
                id="difficulty"
                class="select"
                bind:value={draft.difficulty}
                aria-invalid={err('difficulty') ? 'true' : undefined}
                aria-describedby={described('difficulty')}
              >
                {#each DIFFICULTIES as d (d.id)}<option value={d.id}>{d.label}</option>{/each}
              </select>
              {#if err('difficulty')}<p id="difficulty-error" class="field-error">{err('difficulty')}</p>{/if}
            </div>
          </div>
        </section>

        {#if !phone}
          <section class="preview" aria-labelledby="preview-heading">
            <h2 id="preview-heading">Preview</h2>
            <div class="preview-body">
              <StatementPreview
                title={draft.title}
                markdown={draft.statement_md}
                timeLimitMs={draft.time_limit_ms}
                memoryMb={draft.memory_limit_mb}
              />
            </div>
          </section>
        {/if}
      </div>

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

      <section class="block" aria-labelledby="solutions-heading">
        <h2 id="solutions-heading" tabindex="-1">Solutions</h2>

        <div class="programs">
          <ProgramField
            id="reference"
            label="Reference solution"
            help="Must pass every test in at most half the time limit."
            bind:program={draft.reference}
            error={programError('reference')}
          />

          {#if draft.brute}
            <ProgramField
              id="brute"
              label="Brute force"
              help="A slow but simple solution. Validation checks that it agrees with the reference."
              bind:program={draft.brute}
              error={programError('brute')}
              onremove={() => removeProgram('brute')}
              removeLabel="Remove brute force"
            />
          {:else}
            <div class="optional">
              <h3 class="program-name">Brute force</h3>
              <p class="field-help">No brute force. Add a slow but simple solution and validation compares it with the reference.</p>
              <button id="add-brute" type="button" class="btn btn-secondary" onclick={() => addProgram('brute')}>Add brute force</button>
            </div>
          {/if}

          <div class="full">
            {#if draft.validator}
              <ProgramField
                id="validator"
                label="Validator"
                help="Reads one test input and exits with 0 when it is valid."
                bind:program={draft.validator}
                error={programError('validator')}
                onremove={() => removeProgram('validator')}
                removeLabel="Remove validator"
              />
            {:else}
              <div class="optional">
                <h3 class="program-name">Validator</h3>
                <p class="field-help">No validator. Add one to check inputs against the constraints.</p>
                <button id="add-validator" type="button" class="btn btn-secondary" onclick={() => addProgram('validator')}>Add validator</button>
              </div>
            {/if}
          </div>

          {#if draft.checker === 'custom'}
            <div class="field full">
              <h3 class="program-name">Checker</h3>
              <label for="checker_source">Checker source (C++17 with testlib)</label>
              <textarea
                id="checker_source"
                class="input textarea-code textarea-well"
                rows="12"
                spellcheck="false"
                bind:value={draft.checker_source}
                aria-invalid={err('checker_source') ? 'true' : undefined}
                aria-describedby={described('checker_source', 'checker-help')}
              ></textarea>
              <p id="checker-help" class="field-help">
                Run as ./checker input output answer. Exit 0 accepts, 1 or 2 rejects.
              </p>
              {#if err('checker_source')}<p id="checker_source-error" class="field-error">{err('checker_source')}</p>{/if}
            </div>
          {/if}
        </div>
      </section>

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

  /* The sticky bar keeps status and the two main actions in reach. */
  .action-bar {
    position: sticky;
    top: 0;
    z-index: 10;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-5);
    margin-top: var(--space-4);
    padding-block: var(--space-2);
    border-block: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .bar-status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-4);
    min-width: 0;
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border-radius: var(--radius);
    font-weight: 500;
    text-decoration: none;
  }

  .status:hover {
    text-decoration: underline;
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

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: currentColor;
  }

  .unsaved {
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .error-count {
    border-radius: var(--radius);
    color: var(--bad);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .jumps {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    font-size: var(--fs-0);
  }

  .jumps a {
    border-radius: var(--radius);
    color: var(--ink-muted);
  }

  .jumps a:hover {
    color: var(--ink);
  }

  .bar-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-left: auto;
  }

  .reason-field {
    max-width: var(--measure-form);
    margin-top: var(--space-3);
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 48rem;
  }

  .retry {
    margin-top: var(--space-4);
  }

  .top {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
    margin-top: var(--space-6);
  }

  .details {
    display: grid;
    gap: var(--space-4);
    align-content: start;
    min-width: 0;
  }

  .statement-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .statement-head label {
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .statement-write {
    display: grid;
    gap: var(--space-1);
  }

  .statement-write[hidden] {
    display: none;
  }

  .mini-tabs {
    display: flex;
    gap: var(--space-1);
  }

  .mini-tab {
    min-height: var(--control-h);
    padding-inline: var(--space-3);
    border: var(--rule) solid transparent;
    border-radius: var(--radius);
    background: none;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .mini-tab[aria-selected='true'] {
    border-color: var(--line-strong);
    background: var(--surface);
    color: var(--ink);
  }

  /* Markdown is prose, so it wraps; source code does not. */
  .statement-input {
    min-height: calc(12 * var(--lh-body) * var(--fs-0));
    resize: vertical;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
  }

  .row > .field {
    flex: 1 1 10rem;
    min-width: 0;
  }

  .row > .grow {
    flex-grow: 3;
  }

  .preview {
    min-width: 0;
  }

  .preview-body {
    margin-top: var(--space-4);
    padding: var(--space-4) var(--space-5);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .inline-preview {
    margin-top: 0;
    padding: var(--space-3) var(--space-4);
  }

  .editor :is(input, select, textarea) {
    scroll-margin-top: 10rem;
  }

  .validation.block {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: 0;
    border-top: 0;
    max-width: 64rem;
  }

  .ai-draft {
    padding: 0 var(--space-1);
    border: var(--rule) solid var(--part);
    border-radius: var(--radius);
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
    white-space: nowrap;
  }

  @media (min-width: 1024px) {
    .top {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }

    .preview {
      position: sticky;
      top: calc(var(--space-7) + var(--space-4));
      align-self: start;
      max-height: calc(100vh - var(--space-7) - 2 * var(--space-4));
      overflow-y: auto;
    }
  }

  @media (width < 768px) {
    .jumps {
      display: none;
    }

    .bar-actions {
      margin-left: 0;
      width: 100%;
    }

    .bar-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
