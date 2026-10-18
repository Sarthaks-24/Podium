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
  asks a model for a solution after a confirmation, follows the `wizard`
  and `submission` live events with polling as the fallback, and shows
  "A model solved this zero-shot: N of 100" with what that means for graded
  contests and a link to the submission. Props: `id`, the problem id from
  the address.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ProgramField from '../components/ProgramField.svelte';
  import StatementPreview from '../components/StatementPreview.svelte';
  import ValidationReport from '../components/ValidationReport.svelte';
  import {
    aiPaths,
    inputGenPending,
    inputGenReport,
    isAIDraft,
    isKeyProblem,
    modelLabel,
    solveLine,
    solveMeaning,
    wizardPending,
    type InputGeneration,
    type KeyStatus,
    type Wizard,
  } from '../lib/ai';
  import { ApiError, getJSON, sendBody, sendJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import {
    CHECKERS,
    DIFFICULTIES,
    draftGroups,
    editorStatus,
    fieldKey,
    formatBytes,
    isFailure,
    isPending,
    nextPlanRow,
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
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { EventStream, type StreamMode } from '../lib/stream';
  import { showToast } from '../lib/toast.svelte';
  import { formatMs, type Generation, type Submission } from '../lib/workspace';

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

  let addSubtaskButton: HTMLButtonElement | undefined = $state();

  // M5: the generator, its plan, and generating inputs from it.
  let inputGen = $state<InputGeneration | null>(null);
  let inputGenError = $state<{ message: string; problems: readonly string[] } | null>(null);
  let inputGenBusy = $state(false);
  let addPlanButton: HTMLButtonElement | undefined = $state();

  // M5: "Can a model solve this?"
  let aiStatus = $state<KeyStatus | null>(null);
  let solveOpen = $state(false);
  let solveBusy = $state(false);
  let solveWizard = $state<Wizard | null>(null);
  let solveSubmission = $state<Submission | null>(null);
  let solveError = $state<{ message: string; code: string; reasons: readonly string[] } | null>(null);
  let solveMode = $state<StreamMode>('connecting');

  let phone = $state(false);
  let statementTab = $state<'write' | 'preview'>('write');

  const pointsTotal = $derived(draft ? draft.subtasks.reduce((sum, s) => sum + (s.points ?? 0), 0) : 0);
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

  const groups = $derived(problem && draft ? draftGroups(problem.tests, draft) : new Map<number, string>());
  const unassigned = $derived(problem ? problem.tests.filter((t) => groups.get(t.idx) === 'Not in a subtask').map((t) => t.idx) : []);
  const missing = $derived(problem ? problem.tests.filter((t) => t.output_missing).map((t) => t.idx) : []);
  const testsSummary = $derived.by(() => {
    if (!problem || problem.tests.length === 0) return '';
    const n = problem.tests.length;
    const samples = problem.tests.filter((t) => groups.get(t.idx) === 'Sample').length;
    const named = new Set([...groups.values()].filter((g) => g.startsWith('Subtask ')));
    const inSubtasks = problem.tests.filter((t) => groups.get(t.idx)?.startsWith('Subtask ')).length;
    const parts = [`${samples} ${samples === 1 ? 'sample' : 'samples'}`];
    if (inSubtasks > 0) parts.push(`${inSubtasks} in ${named.size} ${named.size === 1 ? 'subtask' : 'subtasks'}`);
    if (unassigned.length > 0) parts.push(`${unassigned.length} in no subtask`);
    return `${n} ${n === 1 ? 'test' : 'tests'}: ${parts.join(', ')}.`;
  });

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
  const canGenerateInputs = $derived(draft !== null && !!draft.generator && (draft.plan?.length ?? 0) > 0);

  const solveWaiting = $derived(
    solveWizard !== null &&
      solveWizard.state !== 'error' &&
      (wizardPending(solveWizard) || solveSubmission === null || solveSubmission.state === 'queued' || solveSubmission.state === 'running'),
  );
  const solveDone = $derived(solveSubmission !== null && solveSubmission.state === 'done');
  const solveStatus = $derived.by(() => {
    if (solveWizard === null) return '';
    if (solveWizard.state === 'error') return 'The model could not answer';
    if (wizardPending(solveWizard)) return 'Waiting for the model to answer';
    if (solveSubmission === null || solveSubmission.state === 'queued' || solveSubmission.state === 'running') return "Judging the model's solution";
    if (solveSubmission.state === 'error') return "The judge could not finish the model's submission";
    return solveLine(solveSubmission);
  });
  const solveTone = $derived.by(() => {
    const s = solveSubmission;
    if (!s || s.state !== 'done') return 'note';
    if (s.max_score > 0 && s.score >= s.max_score) return 'bad';
    return s.score > 0 ? 'part' : 'ok';
  });
  const keyMissing = $derived(aiStatus !== null && !aiStatus.active);

  // Field errors, each with the control it belongs to where there is one.
  const FIELD_LABELS: Record<string, string> = {
    title: 'Title',
    slug: 'Slug',
    statement_md: 'Statement',
    time_limit_ms: 'Time limit',
    memory_limit_mb: 'Memory',
    checker: 'Checker',
    epsilon: 'Allowed error',
    checker_source: 'Checker source',
    tags: 'Tags',
    difficulty: 'Difficulty',
    samples: 'Sample tests',
    subtasks: 'Subtasks',
    reference: 'Reference solution',
    brute: 'Brute force',
    validator: 'Validator',
    generator: 'Generator',
    plan: 'Planned tests',
  };

  const PLAN_PARTS: Record<string, string> = { idx: 'test', subtask: 'subtask', seed: 'seed', args: 'arguments' };

  function fieldTarget(key: string): { id: string | null; label: string } {
    const args = /^subtasks\.(\d+)\.generator_args$/.exec(key);
    if (args) {
      const i = Number(args[1]);
      const ok = draft !== null && i < draft.subtasks.length && hasGenerator;
      return { id: ok ? `subtask-${i}-args` : null, label: `Subtask ${i + 1} generator arguments` };
    }
    const planRow = /^plan\.(\d+)\.(idx|subtask|seed|args)$/.exec(key);
    if (planRow) {
      const i = Number(planRow[1]);
      const ok = draft?.plan !== undefined && i < draft.plan.length;
      return { id: ok ? `plan-${i}-${planRow[2]}` : null, label: `Planned test ${i + 1} ${PLAN_PARTS[planRow[2]]}` };
    }
    if (key === 'plan') return { id: hasGenerator ? 'add-plan-row' : null, label: FIELD_LABELS.plan };
    const generator = /^generator(?:\.(\w+))?$/.exec(key);
    if (generator) {
      const part = generator[1] === 'language' ? 'language' : 'source';
      return { id: draft?.generator ? `generator-${part}` : null, label: FIELD_LABELS.generator };
    }
    const row = /^subtasks\.(\d+)\.(points|title|tests)$/.exec(key);
    if (row) {
      const i = Number(row[1]);
      const ok = draft !== null && i < draft.subtasks.length;
      return { id: ok ? `subtask-${i}-${row[2]}` : null, label: `Subtask ${i + 1} ${row[2]}` };
    }
    const program = /^(reference|brute|validator)(?:\.(\w+))?$/.exec(key);
    if (program) {
      const role = program[1] as 'reference' | 'brute' | 'validator';
      const present = role === 'reference' || (draft !== null && draft[role] !== null);
      const part = program[2] === 'language' ? 'language' : 'source';
      return { id: present ? `${role}-${part}` : null, label: FIELD_LABELS[role] };
    }
    if (key === 'subtasks') return { id: 'add-subtask', label: FIELD_LABELS.subtasks };
    if (key === 'epsilon' && draft?.checker !== 'float') return { id: null, label: FIELD_LABELS.epsilon };
    if (key === 'checker_source' && draft?.checker !== 'custom') return { id: null, label: FIELD_LABELS.checker_source };
    if (FIELD_LABELS[key]) return { id: key, label: FIELD_LABELS[key] };
    return { id: null, label: '' };
  }

  const fieldList = $derived(
    Object.entries(fields).map(([key, message]) => ({ key, message, ...fieldTarget(key) })),
  );

  /** The error for a program, whichever of its keys the server used. */
  function programError(role: 'reference' | 'brute' | 'validator' | 'generator'): string {
    const key = Object.keys(fields).find((k) => k === role || k.startsWith(`${role}.`));
    return key ? fields[key] : '';
  }

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, help = ''): string | undefined {
    const ids = [help, fields[key] ? `${key.replace(/\./g, '-')}-error` : ''].filter((s) => s !== '');
    return ids.length > 0 ? ids.join(' ') : undefined;
  }

  function focusField(event: MouseEvent, target: string | null) {
    if (!target) return;
    const el = document.getElementById(target);
    if (!el) return;
    event.preventDefault();
    el.scrollIntoView({ block: 'center' });
    el.focus({ preventScroll: true });
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
    setTitle(p.title, 'Problem bank');
  }

  // Polling stops for good when the editor goes away.
  let alive = true;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let genTimer: ReturnType<typeof setTimeout> | undefined;
  let inputTimer: ReturnType<typeof setTimeout> | undefined;
  let solveTimer: ReturnType<typeof setTimeout> | undefined;
  let solveStream: EventStream | null = null;

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
    try {
      aiStatus = await getJSON<KeyStatus>(aiPaths.key);
    } catch {
      // Without the key's state the button still works; the server says when a key is missing.
      aiStatus = null;
    }
  }

  onMount(() => {
    setTitle('Problem');
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
    const query = window.matchMedia('(max-width: 767px)');
    const sync = () => {
      phone = query.matches;
      if (!phone) statementTab = 'write';
    };
    sync();
    query.addEventListener('change', sync);
    return () => {
      alive = false;
      clearTimeout(pollTimer);
      clearTimeout(genTimer);
      clearTimeout(inputTimer);
      stopSolveWatch();
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
      const p = await sendJSON<Problem>('PUT', problemPath(id), result.edit);
      adopt(p, true);
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

  async function addSubtask() {
    if (!draft) return;
    draft.subtasks.push(draft.plan !== undefined ? { points: null, title: '', tests: '', args: '' } : { points: null, title: '', tests: '' });
    await tick();
    document.getElementById(`subtask-${draft.subtasks.length - 1}-points`)?.focus();
  }

  async function removeSubtask(index: number) {
    if (!draft) return;
    draft.subtasks.splice(index, 1);
    // Planned tests name subtasks by number, so later ones move down.
    for (const row of draft.plan ?? []) if (row.subtask > index + 1) row.subtask--;
    // Row errors are keyed by position, so they no longer line up.
    fields = Object.fromEntries(Object.entries(fields).filter(([k]) => !k.startsWith('subtasks.') && !k.startsWith('plan.')));
    await tick();
    addSubtaskButton?.focus();
  }

  async function addGenerator() {
    if (!draft) return;
    draft.generator = {
      language: 'python3',
      source: 'import random\nimport sys\n\nseed = int(sys.argv[1])\nargs = dict(a.split("=", 1) for a in sys.argv[2:])\nrng = random.Random(seed)\n',
    };
    await tick();
    document.getElementById('generator-source')?.focus();
  }

  async function removeGenerator() {
    if (!draft) return;
    draft.generator = null;
    await tick();
    document.getElementById('add-generator')?.focus();
  }

  async function addPlanRow() {
    if (!draft || draft.plan === undefined || !problem) return;
    draft.plan.push(nextPlanRow(draft.plan, problem.tests.map((t) => t.idx), draft.subtasks.length));
    await tick();
    document.getElementById(`plan-${draft.plan.length - 1}-seed`)?.focus();
  }

  async function removePlanRow(index: number) {
    if (!draft || draft.plan === undefined) return;
    draft.plan.splice(index, 1);
    fields = Object.fromEntries(Object.entries(fields).filter(([k]) => !k.startsWith('plan.')));
    await tick();
    addPlanButton?.focus();
  }

  /** The errors of one planned test row, in column order. */
  function planErrors(i: number): { key: string; message: string }[] {
    return ['idx', 'subtask', 'seed', 'args']
      .map((part) => ({ key: `plan.${i}.${part}`, message: err(`plan.${i}.${part}`) }))
      .filter((e) => e.message !== '');
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

  function stopSolveWatch() {
    solveStream?.stop();
    solveStream = null;
    clearTimeout(solveTimer);
  }

  function solveFinished(): boolean {
    if (!solveWizard) return true;
    if (solveWizard.state === 'error') return true;
    const s = solveSubmission;
    return s !== null && (s.state === 'done' || s.state === 'error');
  }

  /** Takes a newer copy of the solve request; true when it just gained its submission. */
  function adoptSolveWizard(w: Wizard): boolean {
    if (!solveWizard || String(w.id) !== String(solveWizard.id)) return false;
    const hadSubmission = solveWizard.submission_id ?? null;
    solveWizard = w;
    if (w.state === 'error') {
      solveError = { message: w.error?.message ?? 'The model could not answer.', code: w.error?.code ?? 'error', reasons: w.error?.reasons ?? [] };
      stopSolveWatch();
      return false;
    }
    return hadSubmission === null && !!w.submission_id;
  }

  function adoptSolveSubmission(s: Submission) {
    if (!solveWizard?.submission_id || s.id !== solveWizard.submission_id) return;
    solveSubmission = s;
    if (solveFinished()) stopSolveWatch();
  }

  function scheduleSolvePoll() {
    clearTimeout(solveTimer);
    if (!alive || solveFinished()) return;
    solveTimer = setTimeout(pollSolve, solveMode === 'live' ? 3 * POLL_MS : POLL_MS);
  }

  async function pollSolve() {
    const w = solveWizard;
    if (!w) return;
    try {
      if (!w.submission_id) {
        const next = await getJSON<Wizard>(aiPaths.wizardOne(w.id));
        if (!alive) return;
        adoptSolveWizard(next);
      }
      const sid = solveWizard?.submission_id;
      if (sid && !solveFinished()) {
        const s = await getJSON<Submission>(`/api/submissions/${encodeURIComponent(String(sid))}`);
        if (!alive) return;
        adoptSolveSubmission(s);
      }
    } catch (e) {
      if (handleSessionError(e)) return;
      // A failed poll is tried again on the next tick.
    }
    scheduleSolvePoll();
  }

  async function solve() {
    solveError = null;
    solveSubmission = null;
    solveBusy = true;
    try {
      const answer = await sendJSON<{ wizard_id: string | number }>('POST', problemPath(id, '/solve'));
      solveWizard = { id: String(answer.wizard_id), state: 'thinking' };
      solveOpen = false;
      stopSolveWatch();
      solveMode = 'connecting';
      solveStream = new EventStream({
        url: '/api/events',
        events: ['wizard', 'submission'],
        onEvent: (type, data) => {
          if (!data || typeof data !== 'object') return;
          // A submission that just appeared is fetched at once; later changes come as events.
          if (type === 'wizard' && adoptSolveWizard(data as Wizard)) void pollSolve();
          else if (type === 'submission') adoptSolveSubmission(data as Submission);
        },
        onReconnect: () => void pollSolve(),
        poll: () => undefined,
        onMode: (m) => {
          solveMode = m;
        },
      });
      solveStream.start();
      scheduleSolvePoll();
    } catch (e) {
      solveOpen = false;
      if (handleSessionError(e)) return;
      solveError = {
        message: messageOf(e),
        code: e instanceof ApiError ? e.code : 'unexpected',
        reasons: e instanceof ApiError ? e.problems : [],
      };
      if (e instanceof ApiError && e.code === 'no_key' && aiStatus) aiStatus = { ...aiStatus, active: false };
    } finally {
      solveBusy = false;
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
</script>

<svelte:window onbeforeunload={onBeforeUnload} onkeydown={onKeydown} />

<div class="editor">
  <div class="head">
    <a class="back" href="/admin/problems">Problem bank</a>
    <div class="title-row">
      <h1 tabindex="-1">
        {#if loadState === 'missing'}Problem not found{:else if problem}{problem.title.trim() !== '' ? problem.title : problem.slug}{:else}Problem{/if}
      </h1>
      {#if problem && loadState === 'ready'}
        <a class="try" href="/admin/problems/{problem.id}/try">Try as a student</a>
      {/if}
    </div>
  </div>

  {#if loadState === 'forbidden'}
    <p class="lead">Only admins can manage problems. Ask an owner if you need access.</p>
  {:else if loadState === 'missing'}
    <p class="lead">There is no problem with that number. <a href="/admin/problems">Go to the problem bank.</a></p>
  {:else if loadState === 'loading'}
    <p class="lead muted" role="status">Loading problem</p>
  {:else if loadState === 'error'}
    <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
    <p class="retry"><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
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

      <section class="block" aria-labelledby="tests-heading">
        <h2 id="tests-heading" tabindex="-1">Tests</h2>

        <div class="field samples">
          <label for="samples">Sample tests</label>
          <input
            id="samples"
            class="input mono"
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder="1, 2"
            bind:value={draft.samples}
            aria-invalid={err('samples') ? 'true' : undefined}
            aria-describedby={described('samples', 'samples-help')}
          />
          <p id="samples-help" class="field-help">Shown to students under the statement. Test numbers like 1, 2.</p>
          {#if err('samples')}<p id="samples-error" class="field-error">{err('samples')}</p>{/if}
        </div>

        <h3 class="sub">Subtasks</h3>
        <p class="field-help sub-help">
          Every test that is not a sample belongs to exactly one subtask. Points add up to {pointsTotal}{pointsTotal !== 100 && draft.subtasks.length > 0 ? '; contests expect 100' : ''}.
        </p>
        {#if err('subtasks')}<p id="subtasks-error" class="field-error">{err('subtasks')}</p>{/if}

        {#if draft.subtasks.length > 0}
          <ol class="subtasks">
            {#each draft.subtasks as subtask, i (i)}
              <li>
                <div class="subtask" role="group" aria-labelledby="subtask-{i}-name">
                  <span id="subtask-{i}-name" class="subtask-name">Subtask {i + 1}</span>
                  <div class="field points">
                    <label for="subtask-{i}-points">Points</label>
                    <input
                      id="subtask-{i}-points"
                      class="input mono"
                      type="number"
                      inputmode="numeric"
                      min="1"
                      max="1000"
                      bind:value={subtask.points}
                      aria-invalid={err(`subtasks.${i}.points`) ? 'true' : undefined}
                      aria-describedby={described(`subtasks.${i}.points`)}
                    />
                  </div>
                  <div class="field subtask-title">
                    <label for="subtask-{i}-title">Title</label>
                    <input
                      id="subtask-{i}-title"
                      class="input"
                      type="text"
                      autocomplete="off"
                      bind:value={subtask.title}
                      aria-invalid={err(`subtasks.${i}.title`) ? 'true' : undefined}
                      aria-describedby={described(`subtasks.${i}.title`)}
                    />
                  </div>
                  <div class="field range">
                    <label for="subtask-{i}-tests">Tests</label>
                    <input
                      id="subtask-{i}-tests"
                      class="input mono"
                      type="text"
                      autocomplete="off"
                      spellcheck="false"
                      placeholder="3-5"
                      bind:value={subtask.tests}
                      aria-invalid={err(`subtasks.${i}.tests`) ? 'true' : undefined}
                      aria-describedby={described(`subtasks.${i}.tests`)}
                    />
                  </div>
                  <button
                    type="button"
                    class="btn btn-quiet btn-danger-quiet remove"
                    aria-label="Remove subtask {i + 1}"
                    onclick={() => removeSubtask(i)}
                  >
                    Remove
                  </button>
                  {#each ['points', 'title', 'tests'] as part (part)}
                    {#if err(`subtasks.${i}.${part}`)}
                      <p id="subtasks-{i}-{part}-error" class="field-error row-error">{err(`subtasks.${i}.${part}`)}</p>
                    {/if}
                  {/each}
                </div>
              </li>
            {/each}
          </ol>
        {/if}
        <p class="add-row">
          <button id="add-subtask" type="button" class="btn btn-secondary" bind:this={addSubtaskButton} onclick={addSubtask}>Add subtask</button>
        </p>

        <div class="files-head">
          <h3 class="sub">Test files</h3>
          <div class="file-actions">
            <button type="button" class="btn btn-secondary" disabled={replacing} onclick={() => (problem && problem.tests.length > 0 ? (replaceOpen = true) : pickTests())}>
              {problem.tests.length === 0 ? 'Upload tests' : 'Replace tests'}
            </button>
            {#if problem.tests.length > 0}
              {#if missing.length > 0}
                <button type="button" class="btn btn-secondary" disabled={genBusy || genPending} onclick={() => generate(false)}>
                  Generate outputs
                </button>
              {/if}
              <button
                type="button"
                class="btn btn-quiet"
                aria-disabled={genBusy || genPending ? 'true' : undefined}
                onclick={() => {
                  if (!genBusy && !genPending) regenOpen = true;
                }}
              >
                Regenerate all outputs
              </button>
            {/if}
          </div>
          <input
            bind:this={testsInput}
            class="file"
            type="file"
            accept=".zip,application/zip"
            aria-label="Tests to upload"
            tabindex="-1"
            onchange={replaceTests}
          />
        </div>
        <p class="field-help">
          A .zip of numbered input and output files, such as 01.in and 01.out; output files may be left out and generated from the
          reference solution. Subtask ranges stay as they are.
        </p>
        {#if replacing}<p class="muted" role="status">Uploading tests</p>{/if}
        {#if replaceError}
          <div class="banner banner-error problems-list" role="alert">
            <p>{replaceError.message}</p>
            {#if replaceError.problems.length > 0}
              <ul>{#each replaceError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
            {/if}
          </div>
        {/if}

        <div class="generation" aria-live="polite">
          {#if genError}
            <div class="banner banner-error problems-list">
              <p>{genError.message}</p>
              {#if genError.problems.length > 0}
                <ul>{#each genError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
              {/if}
            </div>
          {:else if genSummary}
            <p class="gen-line tone-{genSummary.tone}">{genSummary.text}</p>
            {#if genSummary.lines.length > 0}
              <ul class="gen-failures">{#each genSummary.lines as line, i (i)}<li>{line}</li>{/each}</ul>
            {/if}
          {/if}
        </div>

        {#if problem.tests.length === 0}
          <p class="muted empty">No tests yet. Upload a .zip of numbered input and output files to add them.</p>
        {:else}
          <p class="tests-summary">{testsSummary}</p>
          {#each unassigned as idx (idx)}
            <p class="field-error">Test {idx} is in no subtask. Add it to a subtask range or to the samples.</p>
          {/each}
          {#if missing.length > 0}
            <p class="missing-note">
              {missing.length === 1 ? `Test ${missing[0]} has` : `${missing.length} tests have`} no expected output. Generate outputs to fill
              {missing.length === 1 ? 'it' : 'them'} from the reference solution.
            </p>
          {/if}
          <details class="all-tests">
            <summary>Show all {problem.tests.length} {problem.tests.length === 1 ? 'test' : 'tests'}</summary>
            <div class="table-wrap tests">
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">Test</th>
                    <th scope="col">Group</th>
                    <th scope="col">Input</th>
                    <th scope="col">Output</th>
                    <th scope="col">Download</th>
                  </tr>
                </thead>
                <tbody>
                  {#each problem.tests as t (t.idx)}
                    {@const g = groups.get(t.idx) ?? ''}
                    <tr>
                      <th scope="row" class="num test-num">{t.idx}</th>
                      <td class="group" class:unassigned={g === 'Not in a subtask'}>{g}</td>
                      <td class="file-cell">
                        <span class="cell-label">Input</span>
                        <span class="size">{formatBytes(t.input_size)}</span>
                        <pre class="peek">{t.input_preview}</pre>
                      </td>
                      <td class="file-cell">
                        <span class="cell-label">Output</span>
                        {#if t.output_missing}
                          <span class="missing-word">Output missing</span>
                        {:else}
                          <span class="size">{formatBytes(t.output_size)}</span>
                          <pre class="peek">{t.output_preview}</pre>
                        {/if}
                      </td>
                      <td class="downloads">
                        <a href={problemPath(id, `/tests/${t.idx}/input`)} download aria-label="Download input of test {t.idx}">Input</a>
                        {#if !t.output_missing}
                          <a href={problemPath(id, `/tests/${t.idx}/output`)} download aria-label="Download output of test {t.idx}">Output</a>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </details>
        {/if}
      </section>

      {#if hasGenerator && draft.plan !== undefined}
        <section class="block" aria-labelledby="generator-heading">
          <h2 id="generator-heading" tabindex="-1">Generator</h2>
          <p class="field-help sub-help">
            A program that prints one test input. The judge runs it in the sandbox once per planned test, as generator.py with the seed,
            the subtask's arguments and the test's own arguments, and stores what it prints as that test's input.
          </p>

          <div class="programs">
            {#if draft.generator}
              <ProgramField
                id="generator"
                label="Generator program"
                help="Reads the seed from sys.argv[1] and its arguments after it. Draw every random number from the seed, so a test can be made again."
                bind:program={draft.generator}
                error={programError('generator')}
                onremove={removeGenerator}
                removeLabel="Remove generator"
              />
            {:else}
              <div class="optional">
                <h3 class="program-name">Generator program</h3>
                <p class="field-help">No generator. Add one to make test inputs from seeds instead of uploading them.</p>
                <button id="add-generator" type="button" class="btn btn-secondary" onclick={addGenerator}>Add generator</button>
              </div>
            {/if}
          </div>

          <h3 class="sub">Arguments per subtask</h3>
          <p class="field-help sub-help">
            Given to the generator after the seed for every planned test of the subtask, separated by spaces, for example n=1000
            max=1000000000.
          </p>
          {#if draft.subtasks.length === 0}
            <p class="muted empty">Add subtasks first; each gets its own arguments.</p>
          {:else}
            <div class="arg-rows">
              {#each draft.subtasks as subtask, i (i)}
                <div class="field">
                  <label for="subtask-{i}-args">Subtask {i + 1}{subtask.title.trim() !== '' ? `, ${subtask.title.trim()}` : ''}</label>
                  <input
                    id="subtask-{i}-args"
                    class="input mono"
                    type="text"
                    autocomplete="off"
                    autocapitalize="none"
                    spellcheck="false"
                    bind:value={subtask.args}
                    aria-invalid={err(`subtasks.${i}.generator_args`) ? 'true' : undefined}
                    aria-describedby={described(`subtasks.${i}.generator_args`)}
                  />
                  {#if err(`subtasks.${i}.generator_args`)}
                    <p id="subtasks-{i}-generator_args-error" class="field-error">{err(`subtasks.${i}.generator_args`)}</p>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}

          <h3 class="sub" id="plan-heading">Planned tests</h3>
          <p class="field-help sub-help">
            Each row makes one test from a seed and optional arguments of its own. Samples come from the statement, so leave their
            numbers out.
          </p>
          {#if err('plan')}<p id="plan-error" class="field-error">{err('plan')}</p>{/if}

          {#if draft.plan.length > 0}
            <div class="table-wrap plan">
              <table class="table" aria-labelledby="plan-heading">
                <thead>
                  <tr>
                    <th scope="col">Test</th>
                    <th scope="col">Subtask</th>
                    <th scope="col">Seed</th>
                    <th scope="col">Arguments</th>
                    <th scope="col"><span class="visually-hidden">Remove</span></th>
                  </tr>
                </thead>
                <tbody>
                  {#each draft.plan as row, i (i)}
                    {@const rowErrors = planErrors(i)}
                    <tr class:has-errors={rowErrors.length > 0}>
                      <td class="plan-idx">
                        <span class="cell-label" aria-hidden="true">Test</span>
                        <input
                          id="plan-{i}-idx"
                          class="input mono"
                          type="number"
                          inputmode="numeric"
                          min="1"
                          max="999"
                          aria-label="Row {i + 1}, test number"
                          bind:value={row.idx}
                          aria-invalid={err(`plan.${i}.idx`) ? 'true' : undefined}
                          aria-describedby={described(`plan.${i}.idx`)}
                        />
                      </td>
                      <td class="plan-subtask">
                        <span class="cell-label" aria-hidden="true">Subtask</span>
                        <select
                          id="plan-{i}-subtask"
                          class="select"
                          aria-label="Row {i + 1}, subtask"
                          bind:value={row.subtask}
                          aria-invalid={err(`plan.${i}.subtask`) ? 'true' : undefined}
                          aria-describedby={described(`plan.${i}.subtask`)}
                        >
                          {#each draft.subtasks.map((_, j) => j + 1) as n (n)}<option value={n}>{n}</option>{/each}
                          {#if row.subtask > draft.subtasks.length}<option value={row.subtask}>{row.subtask} (removed)</option>{/if}
                        </select>
                      </td>
                      <td class="plan-seed">
                        <span class="cell-label" aria-hidden="true">Seed</span>
                        <input
                          id="plan-{i}-seed"
                          class="input mono"
                          type="number"
                          inputmode="numeric"
                          min="0"
                          aria-label="Row {i + 1}, seed"
                          bind:value={row.seed}
                          aria-invalid={err(`plan.${i}.seed`) ? 'true' : undefined}
                          aria-describedby={described(`plan.${i}.seed`)}
                        />
                      </td>
                      <td class="plan-args">
                        <span class="cell-label" aria-hidden="true">Arguments</span>
                        <input
                          id="plan-{i}-args"
                          class="input mono"
                          type="text"
                          autocomplete="off"
                          autocapitalize="none"
                          spellcheck="false"
                          aria-label="Row {i + 1}, arguments"
                          bind:value={row.args}
                          aria-invalid={err(`plan.${i}.args`) ? 'true' : undefined}
                          aria-describedby={described(`plan.${i}.args`)}
                        />
                      </td>
                      <td class="plan-remove">
                        <button
                          type="button"
                          class="btn btn-quiet btn-danger-quiet btn-compact"
                          aria-label="Remove planned test {row.idx ?? i + 1}"
                          onclick={() => removePlanRow(i)}
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                    {#if rowErrors.length > 0}
                      <tr class="plan-error-row">
                        <td colspan="5">
                          {#each rowErrors as e (e.key)}
                            <p id="{e.key.replace(/\./g, '-')}-error" class="field-error">{e.message}</p>
                          {/each}
                        </td>
                      </tr>
                    {/if}
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <p class="muted empty">No planned tests yet.</p>
          {/if}

          <div class="plan-actions">
            <button id="add-plan-row" type="button" class="btn btn-secondary" bind:this={addPlanButton} onclick={addPlanRow}>
              Add planned test
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              disabled={inputGenBusy || inputPending || saving || !canGenerateInputs}
              aria-describedby={canGenerateInputs ? undefined : 'generate-inputs-help'}
              onclick={generateInputs}
            >
              {inputPending ? 'Generating inputs' : dirty ? 'Save and generate inputs' : 'Generate inputs'}
            </button>
            {#if !canGenerateInputs}
              <p id="generate-inputs-help" class="field-help">Add a generator and at least one planned test first.</p>
            {/if}
          </div>

          <div class="generation" aria-live="polite">
            {#if inputGenError}
              <div class="banner banner-error problems-list">
                <p>{inputGenError.message}</p>
                {#if inputGenError.problems.length > 0}
                  <ul>{#each inputGenError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
                {/if}
              </div>
            {:else if inputReport}
              <p class="gen-line tone-{inputReport.tone}">{inputReport.text}</p>
              {#if inputReport.lines.length > 0}
                <ul class="gen-failures">{#each inputReport.lines as line, i (i)}<li>{line}</li>{/each}</ul>
              {/if}
            {/if}
          </div>
        </section>
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

      <div class="footer">
        {#if problem.warnings.length > 0}
          <div class="warnings" role="note" aria-labelledby="warnings-title">
            <p id="warnings-title" class="warnings-title">Before you validate</p>
            <ul>{#each problem.warnings as warning, i (i)}<li>{warning}</li>{/each}</ul>
          </div>
        {/if}

        {#if saveError !== ''}
          <div id="field-errors" class="banner banner-error problems-list" role="alert">
            <p>{saveError}</p>
            {#if fieldList.length > 0}
              <ul>
                {#each fieldList as f (f.key)}
                  <li>
                    {#if f.id}
                      <a href="#{f.id}" onclick={(e) => focusField(e, f.id)}>{f.label}: {f.message}</a>
                    {:else}
                      {f.label ? `${f.label}: ` : ''}{f.message}
                    {/if}
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}

        <div class="actions">
          <a class="btn btn-secondary" href={problemPath(id, '/export')} download>Download pack</a>
          {#if !problem.archived}
            <button type="button" class="btn btn-quiet btn-danger-quiet archive" onclick={() => (archiveOpen = true)}>
              Archive problem
            </button>
          {/if}
        </div>
      </div>
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

    {#if hasGenerator}
      <section class="block solve" aria-labelledby="solve-heading">
        <h2 id="solve-heading" tabindex="-1">Can a model solve this?</h2>
        <p class="solve-help">
          Sends only the statement to the AI model with your key and asks for a C++ solution, then judges the answer on this problem's
          tests as your submission. It shows what a student who pastes the statement into a model would get.
        </p>

        <p class="visually-hidden" role="status">{solveWizard ? solveStatus : ''}</p>

        {#if solveDone && solveSubmission}
          <div class="solve-result tone-box-{solveTone}">
            <p class="solve-line">{solveLine(solveSubmission)}</p>
            <p>{solveMeaning(solveSubmission)}</p>
            <p>
              <a href="/admin/problems/{problem.id}/try">See the model's submission</a>
              <span class="muted">on the Try as a student page.</span>
            </p>
          </div>
        {:else if solveWaiting}
          <p class="solve-progress muted">{solveStatus}</p>
          {#if solveMode === 'polling'}
            <p class="field-help">Live updates are down, so the result is checked every few seconds.</p>
          {/if}
        {:else if solveSubmission && solveSubmission.state === 'error'}
          <div class="banner banner-error problems-list"><p>{solveStatus}. Ask again.</p></div>
        {/if}

        {#if solveError}
          <div class="banner banner-error problems-list" role="alert">
            <p>
              {solveError.message}
              {#if isKeyProblem(solveError.code)}<a href="/admin/ai">Go to AI assistant</a>{/if}
            </p>
            {#if solveError.reasons.length > 0}
              <ul>{#each solveError.reasons as line, i (i)}<li>{line}</li>{/each}</ul>
            {/if}
          </div>
        {/if}

        <div class="solve-actions">
          <button
            type="button"
            class="btn btn-secondary"
            disabled={keyMissing}
            aria-disabled={solveBusy || solveWaiting ? 'true' : undefined}
            aria-describedby={keyMissing ? 'solve-key-help' : undefined}
            onclick={() => {
              if (!solveBusy && !solveWaiting) solveOpen = true;
            }}
          >
            {solveWaiting ? 'Asking a model' : solveDone ? 'Ask a model again' : 'Ask a model to solve it'}
          </button>
          {#if keyMissing}
            <p id="solve-key-help" class="field-help">No AI key. <a href="/admin/ai">Add a key in AI assistant</a> to use this.</p>
          {/if}
        </div>
      </section>
    {/if}
  {/if}
</div>

<ConfirmDialog
  open={solveOpen}
  title="Ask a model to solve this problem?"
  body="The statement goes to {aiStatus?.active
    ? modelLabel(aiStatus, aiStatus.provider, aiStatus.model)
    : 'the AI model'} with your key, and its answer is judged on the tests as your submission. Nothing else is sent."
  confirmLabel="Ask a model"
  busy={solveBusy}
  onconfirm={solve}
  oncancel={() => (solveOpen = false)}
/>

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

  .head {
    display: grid;
    gap: var(--space-2);
  }

  .back {
    width: fit-content;
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .title-row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2) var(--space-5);
  }

  h1 {
    min-width: 0;
    overflow-wrap: anywhere;
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

  section h2 {
    font-size: var(--fs-3);
    scroll-margin-top: var(--space-7);
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

  .block {
    margin-top: var(--space-7);
    padding-top: var(--space-6);
    border-top: var(--rule) solid var(--line);
    min-width: 0;
  }

  .samples {
    max-width: 24rem;
    margin-top: var(--space-4);
  }

  .sub {
    font-size: var(--fs-2);
  }

  h3.sub {
    margin-top: var(--space-6);
  }

  .sub-help {
    margin-top: var(--space-1);
    max-width: 72ch;
  }

  .subtasks {
    display: grid;
    gap: var(--space-3);
    max-width: 64rem;
    margin-top: var(--space-4);
    padding: 0;
    list-style: none;
  }

  .subtask {
    display: grid;
    grid-template-columns: minmax(0, 6rem) minmax(0, 6rem) minmax(0, 1fr) minmax(0, 10rem) auto;
    align-items: end;
    gap: var(--space-3);
    padding-bottom: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .subtask-name {
    align-self: center;
    padding-top: var(--space-5);
    font-weight: 500;
    white-space: nowrap;
  }

  .row-error {
    grid-column: 1 / -1;
  }

  .add-row {
    margin-top: var(--space-4);
  }

  .files-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    max-width: 64rem;
    margin-top: var(--space-6);
  }

  .files-head .sub {
    margin-top: 0;
  }

  .file-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .file {
    display: none;
  }

  .files-head + .field-help {
    margin-top: var(--space-1);
    max-width: 72ch;
  }

  .problems-list {
    max-width: 48rem;
    margin-top: var(--space-3);
  }

  .problems-list ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .problems-list a {
    color: var(--bad);
  }

  /* Empty, it stays in the page (a live region that appears with its
     first message is often not read), but takes no space. */
  .generation:empty {
    margin-top: 0;
  }

  .generation {
    max-width: 64rem;
    margin-top: var(--space-3);
  }

  .gen-line {
    font-weight: 500;
  }

  .gen-failures {
    margin-top: var(--space-1);
    padding-left: var(--space-5);
    color: var(--bad);
  }

  .empty {
    margin-top: var(--space-4);
  }

  .tests-summary {
    margin-top: var(--space-4);
  }

  .tests-summary ~ .field-error,
  .missing-note {
    margin-top: var(--space-1);
  }

  .missing-note {
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .all-tests {
    max-width: 64rem;
    margin-top: var(--space-3);
  }

  .all-tests summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    cursor: pointer;
  }

  .tests {
    margin-top: var(--space-3);
  }

  .tests :is(th, td) {
    vertical-align: top;
  }

  .table tbody th {
    font-weight: 500;
  }

  .group {
    white-space: nowrap;
  }

  .unassigned {
    color: var(--bad);
    font-weight: 500;
  }

  .cell-label {
    display: none;
  }

  .file-cell {
    min-width: 10rem;
    max-width: 18rem;
    vertical-align: top;
  }

  .size {
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    color: var(--ink-muted);
  }

  .missing-word {
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .peek {
    max-height: calc(4 * var(--lh-body) * var(--fs-0));
    margin-top: var(--space-1);
    overflow: hidden;
    font-size: var(--fs-0);
    white-space: pre;
    text-overflow: ellipsis;
  }

  .downloads {
    white-space: nowrap;
  }

  .downloads a + a {
    margin-left: var(--space-3);
  }

  .programs {
    display: grid;
    gap: var(--space-6);
    max-width: 64rem;
    margin-top: var(--space-4);
  }

  .program-name {
    font-size: var(--fs-2);
  }

  .optional {
    display: grid;
    justify-items: start;
    align-content: start;
    gap: var(--space-2);
  }

  .footer {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-7);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .warnings-title {
    font-weight: 600;
  }

  .editor :is(input, select, textarea) {
    scroll-margin-top: 10rem;
  }

  .warnings {
    max-width: 48rem;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--part);
    background: var(--part-tint);
    color: var(--ink);
  }

  .warnings ul {
    padding-left: var(--space-5);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    max-width: 64rem;
  }

  .archive {
    margin-left: auto;
  }

  .validation {
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

  .arg-rows {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 18rem), 1fr));
    gap: var(--space-3) var(--space-4);
    max-width: 64rem;
    margin-top: var(--space-3);
  }

  .plan {
    max-width: 64rem;
    margin-top: var(--space-3);
  }

  .plan td {
    padding-block: var(--space-1);
  }

  .plan-idx,
  .plan-subtask,
  .plan-seed {
    width: 8rem;
  }

  .plan-args {
    min-width: 12rem;
  }

  .plan-remove {
    width: 1%;
    text-align: right;
  }

  .plan tr.has-errors > td {
    border-bottom: 0;
  }

  .plan-error-row td {
    height: auto;
    padding-top: 0;
  }

  .plan-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
    max-width: 64rem;
    margin-top: var(--space-4);
  }

  .solve {
    display: grid;
    gap: var(--space-3);
    max-width: 64rem;
  }

  .solve-help {
    max-width: 72ch;
    color: var(--ink-muted);
  }

  .solve-result {
    display: grid;
    gap: var(--space-2);
    max-width: 48rem;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .tone-box-bad {
    border-color: var(--bad);
    background: var(--bad-tint);
  }

  .tone-box-part {
    border-color: var(--part);
    background: var(--part-tint);
  }

  .tone-box-ok {
    border-color: var(--ok);
    background: var(--ok-tint);
  }

  .solve-line {
    font-weight: 600;
  }

  .solve-progress {
    font-weight: 500;
  }

  .solve-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
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

  /* Reference and brute force side by side on wide screens. */
  @media (min-width: 1440px) {
    .programs {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      max-width: none;
    }

    .programs > .full {
      grid-column: 1 / -1;
    }
  }

  @media (max-width: 767px) {
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

    .subtask {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      grid-template-areas:
        'name remove'
        'points range'
        'title title';
    }

    .subtask-name {
      grid-area: name;
      padding-top: 0;
    }

    .remove {
      grid-area: remove;
      justify-self: end;
    }

    .points {
      grid-area: points;
    }

    .range {
      grid-area: range;
    }

    .subtask-title {
      grid-area: title;
    }

    /* Tests stack: number and group, then input over output. */
    .tests thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
    }

    .tests tbody tr {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      gap: var(--space-1) var(--space-3);
      padding: var(--space-3);
      border-bottom: var(--rule) solid var(--line);
    }

    .tests tbody tr:last-child {
      border-bottom: 0;
    }

    .tests tbody :is(th, td) {
      height: auto;
      padding: 0;
      border: 0;
    }

    .tests .file-cell,
    .tests .downloads {
      grid-column: 1 / -1;
      max-width: none;
      min-width: 0;
    }

    .tests .peek {
      overflow-x: auto;
    }

    .cell-label {
      display: inline;
      margin-right: var(--space-2);
      font-size: var(--fs-0);
      font-weight: 500;
    }

    .actions .btn,
    .plan-actions .btn,
    .solve-actions .btn {
      flex: 1 1 auto;
    }

    /* Planned tests stack: test and subtask, then seed and arguments. */
    .plan thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
    }

    .plan tbody tr {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
      grid-template-areas:
        'idx subtask remove'
        'seed args args';
      align-items: end;
      gap: var(--space-2) var(--space-3);
      padding: var(--space-3);
      border-bottom: var(--rule) solid var(--line);
    }

    .plan tbody tr.has-errors {
      border-bottom: 0;
      padding-bottom: 0;
    }

    .plan tbody tr.plan-error-row {
      display: block;
      padding-top: var(--space-1);
    }

    .plan tbody tr:last-child {
      border-bottom: 0;
    }

    .plan tbody :is(th, td) {
      display: grid;
      gap: var(--space-1);
      width: auto;
      min-width: 0;
      height: auto;
      padding: 0;
      border: 0;
    }

    .plan-idx {
      grid-area: idx;
    }

    .plan-subtask {
      grid-area: subtask;
    }

    .plan-seed {
      grid-area: seed;
    }

    .plan-args {
      grid-area: args;
    }

    .plan-remove {
      grid-area: remove;
    }

    .plan .cell-label {
      margin-right: 0;
    }

    /* Archive sits apart, below the other actions, after a rule. */
    .actions .archive {
      flex-basis: 100%;
      justify-content: flex-start;
      margin-left: 0;
      margin-top: var(--space-3);
      border-top: var(--rule) solid var(--line);
      border-radius: 0;
      padding-top: var(--space-3);
      min-height: calc(var(--control-h) + var(--space-3));
    }
  }
</style>
