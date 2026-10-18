<!--
  ProblemEditor: one problem at /admin/problems/{id} (design.md 5.6). The
  header has a link back to the bank, the title and the validation status
  in words. Below it, the details form (title, slug, Markdown statement,
  limits, checker, tags, difficulty) sits beside a live preview of the
  statement; under 1024 px the preview follows the form. Then the tests
  (sample numbers, subtask rows with points, title and a range such as
  "3-5", the stored test files with previews and downloads, and "Replace
  tests" from a .zip), the solutions (reference, optional brute force and
  validator, and the checker source for a custom checker), the actions
  (Save changes, Validate, Download pack, Archive problem) with the
  server's warnings, and the validation report, which is polled every 2 s
  while the judge has the job. Server field errors appear next to their
  fields. Leaving with unsaved changes asks first, both for in-app links
  and for closing the tab. Props: `id`, the problem id from the address.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ProgramField from '../components/ProgramField.svelte';
  import StatementPreview from '../components/StatementPreview.svelte';
  import ValidationReport from '../components/ValidationReport.svelte';
  import { ApiError, getJSON, sendBody, sendJSON } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import {
    CHECKERS,
    DIFFICULTIES,
    fieldKey,
    formatBytes,
    isPending,
    problemPath,
    toDraft,
    toEdit,
    type Draft,
    type Problem,
    type TestCase,
    type Validation,
  } from '../lib/problems';
  import { navigate, setNavigationGuard } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  /** How often the validation is asked for while the judge has it. */
  const POLL_MS = 2000;

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

  let archiveOpen = $state(false);
  let archiving = $state(false);
  let leaveTo = $state<string | null>(null);

  let addSubtaskButton: HTMLButtonElement | undefined = $state();

  const pointsTotal = $derived(draft ? draft.subtasks.reduce((sum, s) => sum + (s.points ?? 0), 0) : 0);

  // Field errors with no control of their own on this screen are listed
  // with the save message, so none is lost.
  const OWN_FIELDS = new Set([
    'title', 'slug', 'statement_md', 'time_limit_ms', 'memory_limit_mb', 'checker', 'epsilon',
    'checker_source', 'tags', 'difficulty', 'samples', 'subtasks', 'reference', 'brute', 'validator',
  ]);
  const strayFields = $derived(
    Object.entries(fields)
      .filter(([key]) => {
        if (OWN_FIELDS.has(key)) return false;
        const row = /^subtasks\.(\d+)\.(points|title|tests)$/.exec(key);
        if (row && draft && Number(row[1]) < draft.subtasks.length) return false;
        const program = /^(reference|brute|validator)\./.exec(key);
        if (program && (program[1] === 'reference' || (draft && draft[program[1] as 'brute' | 'validator']))) return false;
        return true;
      })
      .map(([, message]) => message),
  );

  /** The error for a program, whichever of its keys the server used. */
  function programError(role: 'reference' | 'brute' | 'validator'): string {
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
    setTitle(p.title, 'Problems');
  }

  // Polling stops for good when the editor goes away.
  let alive = true;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;

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
    return () => {
      alive = false;
      clearTimeout(pollTimer);
      removeGuard();
    };
  });

  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
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

  async function addSubtask() {
    if (!draft) return;
    draft.subtasks.push({ points: null, title: '', tests: '' });
    await tick();
    document.getElementById(`subtask-${draft.subtasks.length - 1}-points`)?.focus();
  }

  async function removeSubtask(index: number) {
    if (!draft) return;
    draft.subtasks.splice(index, 1);
    // Row errors are keyed by position, so they no longer line up.
    fields = Object.fromEntries(Object.entries(fields).filter(([k]) => !k.startsWith('subtasks.')));
    await tick();
    addSubtaskButton?.focus();
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
    try {
      const p = await sendBody<Problem>('PUT', problemPath(id, '/tests'), file, 'application/zip');
      adopt(p, false);
      showToast(`Replaced tests. ${p.tests.length} ${p.tests.length === 1 ? 'test' : 'tests'} stored.`);
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

  function group(t: TestCase): string {
    if (t.sample) return 'Sample';
    if (t.subtask_idx !== null) return `Subtask ${t.subtask_idx}`;
    return 'Not in a subtask';
  }
</script>

<svelte:window onbeforeunload={onBeforeUnload} />

<div class="editor">
  <div class="head">
    <a class="back" href="/admin/problems">Problem bank</a>
    <div class="title-row">
      <h1 tabindex="-1">
        {#if loadState === 'missing'}Problem not found{:else if problem}{problem.title.trim() !== '' ? problem.title : problem.slug}{:else}Problem{/if}
      </h1>
      {#if problem && loadState === 'ready'}
        <p class="status">
          {#if problem.archived}
            <span class="state">Archived</span>
          {:else if problem.validated && problem.validated_at !== null}
            <span class="state ok"><span class="dot" aria-hidden="true"></span>Validated {formatDateTime(problem.validated_at)}</span>
          {:else}
            <span class="state">Not validated</span>
          {/if}
          {#if dirty}<span class="unsaved">Unsaved changes</span>{/if}
        </p>
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
    {#if problem.archived}
      <div class="banner banner-info lead">
        <p>This problem is archived. It is hidden from the bank and kept for the contests that used it.</p>
      </div>
    {/if}

    <form class="form" novalidate onsubmit={onSubmit}>
      <div class="top">
        <section class="details" aria-labelledby="details-heading">
          <h2 id="details-heading">Details</h2>

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
            <label for="statement_md">Statement</label>
            <textarea
              id="statement_md"
              class="input textarea-code statement-input"
              rows="18"
              spellcheck="true"
              bind:value={draft.statement_md}
              aria-invalid={err('statement_md') ? 'true' : undefined}
              aria-describedby={described('statement_md', 'statement-help')}
            ></textarea>
            <p id="statement-help" class="field-help">Markdown. Write maths between dollar signs: $n \le 10^5$, or $$ on its own lines for a block.</p>
            {#if err('statement_md')}<p id="statement_md-error" class="field-error">{err('statement_md')}</p>{/if}
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
      </div>

      <section class="block" aria-labelledby="tests-heading">
        <h2 id="tests-heading">Tests</h2>

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
          <button type="button" class="btn btn-secondary" bind:this={addSubtaskButton} onclick={addSubtask}>Add subtask</button>
        </p>

        <div class="files-head">
          <h3 class="sub">Test files</h3>
          <button type="button" class="btn btn-secondary" disabled={replacing} onclick={pickTests}>Replace tests</button>
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
          Upload a .zip of numbered input and output files, such as 01.in and 01.out. Subtask ranges stay as they are.
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

        {#if problem.tests.length === 0}
          <p class="muted empty">No tests yet. Replace tests with a .zip to add them.</p>
        {:else}
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
                  <tr>
                    <th scope="row" class="num">{t.idx}</th>
                    <td class="group" class:unassigned={!t.sample && t.subtask_idx === null}>{group(t)}</td>
                    <td class="file-cell">
                      <span class="size">{formatBytes(t.input_size)}</span>
                      <pre class="peek">{t.input_preview}</pre>
                    </td>
                    <td class="file-cell">
                      <span class="size">{formatBytes(t.output_size)}</span>
                      <pre class="peek">{t.output_preview}</pre>
                    </td>
                    <td class="downloads">
                      <a href={problemPath(id, `/tests/${t.idx}/input`)} download aria-label="Download input of test {t.idx}">Input</a>
                      <a href={problemPath(id, `/tests/${t.idx}/output`)} download aria-label="Download output of test {t.idx}">Output</a>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <p class="field-help">Groups show the saved subtasks. Save to see changes here.</p>
        {/if}
      </section>

      <section class="block" aria-labelledby="solutions-heading">
        <h2 id="solutions-heading">Solutions</h2>

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
              <p class="field-help">Optional. A slow but simple solution that validation compares with the reference.</p>
              <button id="add-brute" type="button" class="btn btn-secondary" onclick={() => addProgram('brute')}>Add brute force</button>
            </div>
          {/if}

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
              <p class="field-help">Optional. Reads one test input and exits with 0 when it is valid.</p>
              <button id="add-validator" type="button" class="btn btn-secondary" onclick={() => addProgram('validator')}>Add validator</button>
            </div>
          {/if}

          {#if draft.checker === 'custom'}
            <div class="field">
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
          <div class="warnings" role="note" aria-label="Before you validate">
            <ul>{#each problem.warnings as warning, i (i)}<li>{warning}</li>{/each}</ul>
          </div>
        {/if}

        {#if saveError !== ''}
          <div class="banner banner-error problems-list" role="alert">
            <p>{saveError}</p>
            {#if strayFields.length > 0}
              <ul>{#each strayFields as line, i (i)}<li>{line}</li>{/each}</ul>
            {/if}
          </div>
        {/if}

        <div class="actions">
          <button type="submit" class="btn btn-primary" disabled={saving}>Save changes</button>
          <button
            type="button"
            class="btn btn-secondary"
            disabled={requesting || saving || isPending(validation)}
            onclick={validate}
          >
            Validate
          </button>
          <a class="btn btn-secondary" href={problemPath(id, '/export')} download>Download pack</a>
          {#if !problem.archived}
            <button type="button" class="btn btn-quiet btn-danger-quiet archive" onclick={() => (archiveOpen = true)}>
              Archive problem
            </button>
          {/if}
        </div>
        {#if dirty}<p class="field-help">You have unsaved changes. Validate saves them first.</p>{/if}
      </div>
    </form>

    <section class="block validation" aria-labelledby="validation-heading">
      <h2 id="validation-heading">Validation</h2>
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

  .status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-4);
  }

  .state {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--ink-muted);
    font-weight: 500;
  }

  .state.ok {
    color: var(--ok);
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
  }

  .details {
    display: grid;
    gap: var(--space-4);
    align-content: start;
    min-width: 0;
  }

  /* Markdown is prose, so it wraps; source code does not. */
  .statement-input {
    min-height: calc(12 * var(--lh-body) * var(--fs-0));
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

  .file {
    display: none;
  }

  .files-head + .field-help {
    margin-top: var(--space-1);
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

  .empty {
    margin-top: var(--space-4);
  }

  .tests {
    max-width: 64rem;
    margin-top: var(--space-4);
  }

  .tests :is(th, td) {
    vertical-align: top;
  }

  .tests + .field-help {
    margin-top: var(--space-2);
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
    gap: var(--space-2);
  }

  .footer {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-7);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
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

  @media (min-width: 1024px) {
    .top {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }

    .preview {
      position: sticky;
      top: var(--space-4);
      align-self: start;
      max-height: calc(100vh - 2 * var(--space-4));
      overflow-y: auto;
    }
  }

  @media (max-width: 767px) {
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

    .actions .btn {
      flex: 1 1 auto;
    }

    .archive {
      margin-left: 0;
    }
  }
</style>
