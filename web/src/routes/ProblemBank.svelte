<!--
  ProblemBank: the organiser's list of problems at /admin/problems, most
  recently changed first, each with its status, test count and points,
  Validate when it is not validated and Try to open it as a student would.
  A filter appears past ten problems. "New problem" asks only for a title
  and opens the editor; "New problem with AI" opens the wizard; "Import
  pack" sends one or more .zip files and starts validating each, staying
  on the list so several packs need no page visits. A pack whose slug is
  already a problem's opens a dialog to import it as new, archive the
  existing problem and import, or replace it. "New problem" warns when a
  problem with the same title exists (Open it, or Restore it when it is
  archived). AI drafts and
  problems whose hidden tests practice showed carry a mark beside the
  title. The list loads through lib/loader, so an older answer, or one
  after the page has gone, is dropped. Students see a short explanation
  instead.
-->
<script lang="ts">
  import { administers, organises } from '../lib/roles';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import StaleNote from '../components/StaleNote.svelte';
  import RestoreFlow from '../components/RestoreFlow.svelte';
  import ImportConflictDialog from '../components/ImportConflictDialog.svelte';
  import PackPromptPanel from '../components/PackPromptPanel.svelte';
  import DeletedProblems from '../components/problem/DeletedProblems.svelte';
  import Badge from '../components/Badge.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import { onMount, tick, untrack } from 'svelte';
  import { focusFirstInvalid, refocusRow } from '../lib/focus';
  import { problemTone } from '../lib/badges';
  import { isAIDraft, withoutAIDraft } from '../lib/ai';
  import { ApiError, errorText, getJSON, sendBody, sendJSON } from '../lib/api';
  import { newKey } from '../lib/workspace';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { loader } from '../lib/loader.svelte';
  import {
    DIFFICULTIES,
    importQuery,
    isPending,
    parseExisting,
    problemPath,
    sameTitle,
    statusWord,
    type ImportChoice,
    type ImportExisting,
    type Problem,
    type ProblemSummary,
    type Validation,
  } from '../lib/problems';
  import { navigate, router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let problems = $state<ProblemSummary[]>([]);
  /** Which list the page shows: the archived ones too, or not. */
  let wantArchived = false;
  /** The archived problem the Restore dialog is open for. */
  let restoring = $state<{ id: number; name: string } | null>(null);
  const page = loader(
    () => getJSON<{ problems: ProblemSummary[] }>(`/api/admin/problems${wantArchived ? '?archived=1' : ''}`),
    (data) => (problems = data.problems ?? []),
    () => `/api/admin/problems${wantArchived ? '?archived=1' : ''}`,
  );

  let creating = $state(false);
  let newTitle = $state('');
  let createBusy = $state(false);
  // One key for this visit (v4 U13): a double click or a retried create makes one problem; success leaves the page.
  const createKey = newKey();
  let createError = $state('');
  let createFields = $state<Record<string, string>>({});
  let titleInput: HTMLInputElement | undefined = $state();
  let allProblems = $state<ProblemSummary[]>([]);
  const repeated = $derived(creating ? sameTitle(newTitle, allProblems) : null);
  let newButton: HTMLButtonElement | undefined = $state();

  let fileInput: HTMLInputElement | undefined = $state();
  let importButton: HTMLButtonElement | undefined = $state();
  let importing = $state(false);
  let importError = $state<{ message: string; problems: readonly string[] } | null>(null);
  /** The pack whose slug is taken, while the dialog asks what to do; resolve gets the problem made, or null when skipped. */
  let conflict = $state<{ file: File; existing: ImportExisting; suggested: string; busy: boolean; error: string } | null>(null);
  let conflictResolve: ((p: Problem | null) => void) | null = null;

  /** Problems whose validation this page started and is waiting for. */
  let validating = $state<ReadonlySet<number>>(new Set());
  let pollTimer: ReturnType<typeof setTimeout> | undefined;

  /** The filter appears once the list is longer than this. */
  const FILTER_FROM = 10;
  let filter = $state('');

  const sorted = $derived([...problems].sort((a, b) => b.updated_at - a.updated_at || b.id - a.id));
  const shown = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (q === '') return sorted;
    return sorted.filter(
      (p) => p.title.toLowerCase().includes(q) || p.slug.includes(q) || p.tags.some((t) => t.toLowerCase().includes(q)),
    );
  });

  function difficultyLabel(d: ProblemSummary['difficulty']): string {
    return d === '' ? '' : (DIFFICULTIES.find((x) => x.id === d)?.label ?? d);
  }

  const isOrganiser = $derived(organises(session.user?.role));
  const showArchived = $derived(router.query.get('archived') === '1');
  /** Admins and the owner can read the list of deleted problems instead of the bank. */
  const canSeeDeleted = $derived(administers(session.user?.role));
  const showDeleted = $derived(canSeeDeleted && router.query.get('deleted') === '1');

  /** Handles errors every request can hit; returns true when it did. */
  function handleAuth(err: unknown): boolean {
    if (!(err instanceof ApiError)) return false;
    if (err.status === 401) return true;
    if (err.status === 403) {
      page.forbid();
      return true;
    }
    return false;
  }

  function load(archived: boolean) {
    wantArchived = archived;
    return page.load();
  }

  /** Takes the list again without the loading view, after an import or a finished validation. */
  async function reload() {
    try {
      const data = await getJSON<{ problems: ProblemSummary[] }>(`/api/admin/problems${showArchived ? '?archived=1' : ''}`);
      if (page.live) problems = data.problems ?? [];
    } catch (err) {
      handleAuth(err);
    }
  }

  onMount(() => {
    setTitle(routeTitle('problems'));
    return () => {
      page.stop();
      clearTimeout(pollTimer);
    };
  });

  $effect(() => {
    const archived = showArchived;
    if (!isOrganiser) {
      page.forbid();
      return;
    }
    untrack(() => void load(archived));
  });

  function toggleArchived(event: Event) {
    const on = (event.currentTarget as HTMLInputElement).checked;
    navigate(on ? '/admin/problems?archived=1' : '/admin/problems', { replace: true });
  }

  async function openCreate() {
    creating = true;
    createError = '';
    createFields = {};
    void loadAllTitles();
    await tick();
    titleInput?.focus();
  }

  /** Every problem, archived too, so the create form can tell a repeated title. Best effort: no notice when it fails. */
  async function loadAllTitles() {
    try {
      const data = await getJSON<{ problems: ProblemSummary[] }>('/api/admin/problems?archived=1');
      allProblems = data.problems ?? [];
    } catch {
      allProblems = [];
    }
  }

  async function closeCreate() {
    creating = false;
    newTitle = '';
    createError = '';
    createFields = {};
    await tick();
    newButton?.focus();
  }

  async function create(event: SubmitEvent) {
    event.preventDefault();
    createError = '';
    createFields = {};
    const title = newTitle.trim();
    if (title === '') createFields.title = 'Enter a title.';
    if (Object.keys(createFields).length > 0) {
      await focusFirstInvalid('.create');
      return;
    }
    createBusy = true;
    try {
      const problem = await sendJSON<Problem>('POST', '/api/admin/problems', { title, client_key: createKey });
      showToast(`Created ${problem.title}`);
      navigate(`/admin/problems/${problem.id}`);
    } catch (err) {
      if (handleAuth(err)) return;
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        createFields = { ...err.fields };
        if (Object.keys(err.fields).some((k) => k !== 'title')) createError = Object.values(err.fields).join(' ');
        await focusFirstInvalid('.create');
      } else {
        createError = errorText(err);
      }
    } finally {
      createBusy = false;
    }
  }

  function pickPack() {
    fileInput?.click();
  }

  /** The words a row's status badge says: the page's own validations first. */
  function statusOf(p: ProblemSummary): string {
    if (validating.has(p.id)) return 'Validating';
    return statusWord(p);
  }

  /** Asks the judge to validate a problem; false (with the reason in a toast) when it cannot yet. */
  async function startValidation(id: number, name: string, quiet = false): Promise<boolean> {
    try {
      await sendJSON<Validation>('POST', problemPath(id, '/validate'));
      validating = new Set([...validating, id]);
      schedulePoll();
      if (!quiet) showToast(`Validating ${name}`);
      return true;
    } catch (err) {
      if (handleAuth(err)) return false;
      // Opening the problem fixes what is wrong with it, not a network fault.
      if (err instanceof ApiError && err.problems.length > 0) showToast(`${name} cannot be validated yet: ${err.problems[0]} Open it to fix that.`, 'error');
      else showToast(`Could not start validating ${name}. ${errorText(err)}`, 'error');
      return false;
    }
  }

  function schedulePoll() {
    clearTimeout(pollTimer);
    if (page.live && validating.size > 0) pollTimer = setTimeout(pollValidations, 2000);
  }

  /** Follows the validations this page started; the list reloads when one finishes. */
  async function pollValidations() {
    const finished: number[] = [];
    for (const id of validating) {
      try {
        const v = await getJSON<Validation>(problemPath(id, '/validation'));
        if (!isPending(v)) finished.push(id);
      } catch (err) {
        if (handleAuth(err)) return;
      }
    }
    if (!page.live) return;
    const done = finished.length > 0;
    validating = new Set([...validating].filter((id) => !finished.includes(id)));
    if (done) await reload();
    schedulePoll();
  }

  /** Opens the dialog for a pack whose slug is taken and waits for it: the problem made, or null when the person cancels. */
  function askConflict(file: File, existing: ImportExisting, suggested: string): Promise<Problem | null> {
    conflict = { file, existing, suggested, busy: false, error: '' };
    return new Promise((resolve) => (conflictResolve = resolve));
  }

  function finishConflict(problem: Problem | null) {
    conflict = null;
    conflictResolve?.(problem);
    conflictResolve = null;
  }

  /** Sends the pack again with the person's choice; a failure stays in the dialog so they can choose again. */
  async function chooseConflict(choice: ImportChoice, slug: string) {
    if (!conflict) return;
    const { file } = conflict;
    conflict.busy = true;
    conflict.error = '';
    try {
      const problem = await sendBody<Problem>('POST', `/api/admin/problems/import${importQuery(choice, slug, file.name)}`, file, 'application/zip');
      finishConflict(problem);
    } catch (err) {
      if (handleAuth(err)) {
        finishConflict(null);
        return;
      }
      if (!conflict) return;
      conflict.busy = false;
      const existing = err instanceof ApiError && err.status === 409 ? parseExisting(err.existing) : null;
      if (existing && err instanceof ApiError) {
        // The chosen slug is taken too, or a contest started meanwhile: show the new state.
        conflict.existing = existing;
        if (err.suggestedSlug !== '') conflict.suggested = err.suggestedSlug;
        conflict.error = err.message;
      } else if (err instanceof ApiError && err.fields.slug) conflict.error = err.fields.slug;
      else conflict.error = errorText(err);
    }
  }

  async function importPack() {
    const files = [...(fileInput?.files ?? [])];
    if (!fileInput || files.length === 0) return;
    fileInput.value = '';
    importError = null;
    importing = true;
    const made: string[] = [];
    let asked = false;
    try {
      for (const file of files) {
        let problem: Problem | null;
        try {
          problem = await sendBody<Problem>('POST', '/api/admin/problems/import', file, 'application/zip');
        } catch (err) {
          if (handleAuth(err)) return;
          const existing = err instanceof ApiError && err.status === 409 ? parseExisting(err.existing) : null;
          if (err instanceof ApiError && existing) {
            asked = true;
            problem = await askConflict(file, existing, err.suggestedSlug);
            if (problem === null) continue;
          } else {
            importError = {
              message: files.length > 1 ? `${file.name}: ${errorText(err)}` : errorText(err),
              problems: err instanceof ApiError ? err.problems : [],
            };
            break;
          }
        }
        const ok = await startValidation(problem.id, problem.title, true);
        made.push(ok ? `Imported ${problem.title}. Validating.` : `Imported ${problem.title}.`);
      }
    } finally {
      importing = false;
    }
    // The dialog's opener is the hidden file input; the button is disabled until the import ends.
    // Only when focus was lost, so a user who moved on keeps their place.
    if (asked) {
      await tick();
      const active = document.activeElement;
      if (!active || active === document.body || !active.isConnected) importButton?.focus();
    }
    if (made.length > 0) {
      showToast(made.join(' '));
      await reload();
    }
  }
</script>

<PageHeader title="Problems" />

{#if page.state === 'forbidden'}
  <PageState kind="forbidden" message="Only organisers can manage problems." />
{:else}
  <div class="toolbar">
    <div class="buttons">
      <button
        type="button"
        class="btn btn-primary"
        aria-expanded={creating}
        aria-controls="create-problem"
        bind:this={newButton}
        onclick={() => (creating ? closeCreate() : openCreate())}
      >
        New problem
      </button>
      <a class="btn btn-secondary" href="/admin/ai/new">New problem with AI</a>
      <button type="button" class="btn btn-secondary" disabled={importing} bind:this={importButton} onclick={pickPack}>Import pack</button>
      <input
        bind:this={fileInput}
        class="file"
        type="file"
        multiple
        accept=".zip,application/zip"
        aria-label="Pack to import"
        tabindex="-1"
        onchange={importPack}
      />
    </div>
    <div class="aside">
      {#if !showDeleted}
        <label class="toggle">
          <input type="checkbox" checked={showArchived} onchange={toggleArchived} />
          Show archived
        </label>
      {/if}
      {#if canSeeDeleted}
        {#if showDeleted}
          <a class="btn btn-quiet" href="/admin/problems">Back to problems</a>
        {:else}
          <a class="btn btn-quiet" href="/admin/problems?deleted=1">Deleted problems</a>
        {/if}
      {/if}
    </div>
  </div>

  {#if showDeleted}
    <DeletedProblems />
  {:else}
  <PackPromptPanel />

  {#if importing}
    <p class="status muted" role="status">Importing pack</p>
  {/if}

  {#if importError}
    <div class="banner banner-error import-error" role="alert">
      <p>{importError.message}</p>
      {#if importError.problems.length > 0}
        <ul>
          {#each importError.problems as problem, i (i)}<li>{problem}</li>{/each}
        </ul>
      {/if}
    </div>
  {/if}

  {#if creating}
    <form id="create-problem" class="create" novalidate onsubmit={create} aria-labelledby="create-heading">
      <h2 id="create-heading" class="create-heading">New problem</h2>
      <div class="create-fields">
        <div class="field grow">
          <label for="new-title">Title</label>
          <input
            id="new-title"
            aria-required="true"
            class="input"
            type="text"
            autocomplete="off"
            bind:this={titleInput}
            bind:value={newTitle}
            aria-invalid={createFields.title ? 'true' : undefined}
            aria-describedby={createFields.title ? 'new-title-error' : undefined}
          />
          {#if createFields.title}<p id="new-title-error" class="field-error">{createFields.title}</p>{/if}
          {#if repeated}
            <p class="field-help same-title" role="status">
              A problem called {repeated.title} already exists.
              {#if repeated.archived}
                <button type="button" class="btn btn-link btn-compact" onclick={() => (restoring = { id: repeated.id, name: repeated.title })}>Restore it</button>
              {:else}
                <a class="btn btn-link btn-compact" href="/admin/problems/{repeated.id}">Open it</a>
              {/if}
            </p>
          {/if}
        </div>
      </div>
      <div class="create-actions">
        <button type="submit" class="btn btn-primary" disabled={createBusy}>Create problem</button>
        <button type="button" class="btn btn-quiet" onclick={closeCreate}>Cancel</button>
      </div>
      {#if createError !== ''}<p class="field-error" role="alert">{createError}</p>{/if}
    </form>
  {/if}

  <StaleNote {page} />
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading problems" shape="rows" />
  {:else if page.state === 'error' || page.state === 'missing'}
    <PageState kind="error" message={page.error} onretry={() => load(showArchived)} />
  {:else if problems.length === 0}
    <PageState kind="empty" message="No problems yet. Import a pack, create one, or draft one with AI. Imported packs are validated at once." />
  {:else}
    {#if problems.length > FILTER_FROM}
      <div class="field filter">
        <label for="bank-filter">Filter by title or tag</label>
        <input id="bank-filter" class="input" type="search" autocomplete="off" bind:value={filter} />
      </div>
    {/if}
    {#if shown.length === 0}
      <p class="lead muted" role="status">No problem matches "{filter.trim()}". Try another word.</p>
    {:else}
      <div class="table-wrap list">
        <table class="table table-stack">
          <thead>
            <tr>
              <th scope="col">Title</th>
              <th scope="col">Difficulty</th>
              <th scope="col">Tags</th>
              <th scope="col" class="right">Tests</th>
              <th scope="col" class="right">Points</th>
              <th scope="col">Status</th>
              <th scope="col">Updated</th>
              <th scope="col"><span class="visually-hidden">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each shown as p (p.id)}
              {@const name = p.title.trim() !== '' ? p.title : p.slug}
              {@const word = statusOf(p)}
              <tr data-focus-row="problem-{p.id}">
                <th scope="row" class="title">
                  <a class="row-link" data-focus-title href="/admin/problems/{p.id}">{name}</a>{#if p.archived && p.archived_slug}<span class="orig-slug mono">{p.archived_slug}</span>{/if}{#if isAIDraft(p.tags)}<span class="ai-draft"><Badge tone="wait" text="AI draft" /></span>{/if}{#if p.shown_in_practice}<span class="ai-draft"><Badge text="Hidden tests shown in practice" /></span>{/if}
                </th>
                <td class="difficulty">{difficultyLabel(p.difficulty)}</td>
                <td class="tags">{withoutAIDraft(p.tags).join(', ')}</td>
                <td class="num right tests"><span class="cell-label">Tests</span>{p.test_count}</td>
                <td class="num right points">{p.points}</td>
                <td class="state"><Badge tone={word === 'Validating' ? 'wait' : problemTone(word)} text={word} /></td>
                <td class="updated"><MonoDigits text={formatDateTime(p.updated_at)} /></td>
                <td class="try">
                  <span class="row-actions">
                    {#if p.archived}
                      <button type="button" class="btn btn-quiet btn-compact" aria-label="Restore {name}" onclick={() => (restoring = { id: p.id, name })}>Restore</button>
                    {/if}
                    {#if !p.validated && !p.archived && word !== 'Validating'}
                      <button type="button" class="btn btn-quiet btn-compact" aria-label="Validate {name}" onclick={() => startValidation(p.id, name)}>Validate</button>
                    {/if}
                    <a class="btn btn-quiet btn-compact" href="/admin/problems/{p.id}/try" aria-label="Try {name} as a student">Try</a>
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
  {/if}
{/if}

{#if conflict}
  <ImportConflictDialog
    open
    fileName={conflict.file.name}
    existing={conflict.existing}
    suggestedSlug={conflict.suggested}
    busy={conflict.busy}
    error={conflict.error}
    onchoose={chooseConflict}
    oncancel={() => finishConflict(null)}
  />
{/if}

{#if restoring}
  <RestoreFlow
    open
    kind="problem"
    title={restoring.name}
    path={problemPath(restoring.id, '/restore')}
    onclose={() => (restoring = null)}
    ondone={() => {
      const key = `problem-${restoring?.id}`;
      restoring = null;
      void reload().then(() => refocusRow(key));
      if (creating) void loadAllTitles();
    }}
  />
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-narrow);
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3) var(--space-5);
    margin-top: var(--space-5);
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .file {
    display: none;
  }

  .aside {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-5);
  }

  .ai-draft {
    margin-left: var(--space-2);
  }

  .orig-slug {
    margin-left: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .toggle input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .status {
    margin-top: var(--space-3);
  }

  .import-error {
    max-width: var(--measure-form);
    margin-top: var(--space-4);
  }

  .import-error ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .create {
    max-width: var(--measure-form);
    margin-top: var(--space-5);
    padding: var(--space-4);
    border: var(--rule) solid var(--line);
    background: var(--surface);
  }

  .create-heading {
    font-size: var(--fs-2);
  }

  .create-fields {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3) var(--space-4);
    margin-top: var(--space-3);
  }

  .same-title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  /* The notice's action lines up with the sentence above it. */
  .same-title .btn {
    padding-inline: 0;
  }

  .create-fields .grow {
    flex: 1 1 14rem;
  }

  .create-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  .create .field-error[role='alert'] {
    margin-top: var(--space-2);
  }

  .list {
    margin-top: var(--space-5);
  }

  .title {
    font-weight: 500;
    min-width: 9rem;
  }

  .filter {
    max-width: 24rem;
    margin-top: var(--space-5);
  }

  .difficulty,
  .tags {
    color: var(--ink-muted);
  }

  .tags {
    min-width: 8rem;
  }

  .cell-label {
    display: none;
  }

  .try {
    text-align: right;
    white-space: nowrap;
  }

  .right {
    text-align: right;
  }

  .table .right {
    text-align: right;
  }

  .state {
    white-space: nowrap;
  }

  .row-actions {
    display: inline-flex;
    gap: var(--space-1);
  }

  .row-actions :is(a, button) {
    position: relative;
    z-index: 1;
  }

  /* Phones: each row is the title and status, then tests and the date. */
  @media (width < 768px) {
    .list tbody tr {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'title title state'
        'tests updated try';
      align-items: baseline;
    }

    .title {
      grid-area: title;
      min-width: 0;
    }

    .state {
      grid-area: state;
      text-align: right;
    }

    .difficulty,
    .tags,
    .points {
      display: none;
    }

    .tests,
    .updated {
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: left;
    }

    .tests {
      grid-area: tests;
    }

    .updated {
      grid-area: updated;
    }

    .cell-label {
      display: inline;
      margin-right: var(--space-1);
      font-family: var(--font-ui);
    }

    .try {
      grid-area: try;
      justify-self: end;
    }
  }
</style>
