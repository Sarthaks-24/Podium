<!--
  TestsSection: the problem editor's Tests tab. One table lists every
  stored test with the first two lines of its input and output (and the
  whole size when that is longer) and a Role select per
  row (Example, Subtask 1, Subtask 2, ..., Not in a subtask), in place of
  typed test ranges; a choice rewrites the draft's example list and subtask
  ranges (lib/problemEditor setRole), so a save sends the server the same
  lists as before. A test in no subtask, and tests without an expected
  output, are named above the table; Validate makes missing outputs from
  the reference solution, so there is no Generate button here. Under the
  table each subtask has its points, an optional title and how many tests
  it has, with Add subtask; the points should add up to 100. "Upload
  tests" or "Replace tests" takes a .zip; uploading and its confirmation
  stay with the editor.
  Props: `id`, `draft` and `fields` (bindable; removing a subtask drops
  the row errors, which are keyed by position), `fileInput` (bindable, the
  hidden .zip picker), `tests`, `replacing`, `replaceError` and the
  callbacks `onupload` and `onfile`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { describedBy } from '../../lib/problemFields';
  import { formatBytes, problemPath, rangeText, type Draft, type Problem } from '../../lib/problems';
  import { parseRole, roleOf, roleValue, setRole, subtaskCounts } from '../../lib/problemEditor';

  type Report = { message: string; problems: readonly string[] } | null;

  interface Props {
    id: string;
    draft: Draft;
    fields: Record<string, string>;
    fileInput?: HTMLInputElement;
    tests: Problem['tests'];
    replacing: boolean;
    replaceError: Report;
    onupload: () => void;
    onfile: () => void;
  }

  let {
    id,
    draft = $bindable(),
    fields = $bindable(),
    fileInput = $bindable(),
    tests,
    replacing,
    replaceError,
    onupload,
    onfile,
  }: Props = $props();

  let addSubtaskButton: HTMLButtonElement | undefined = $state();

  const pointsTotal = $derived(draft.subtasks.reduce((sum, s) => sum + (s.points ?? 0), 0));
  const roles = $derived(new Map(tests.map((t) => [t.idx, roleOf(t.idx, draft)])));
  const counts = $derived(subtaskCounts(tests, draft));
  const unassigned = $derived(tests.filter((t) => roles.get(t.idx) === 'none').map((t) => t.idx));
  const missing = $derived(tests.filter((t) => t.output_missing).map((t) => t.idx));
  const examples = $derived(tests.filter((t) => roles.get(t.idx) === 'example').length);

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, help = ''): string | undefined {
    return describedBy(fields, key, help);
  }

  function choose(idx: number, value: string) {
    setRole(draft, idx, parseRole(value));
    // The lists were rewritten, so their errors no longer apply.
    fields = Object.fromEntries(Object.entries(fields).filter(([k]) => k !== 'samples' && !/^subtasks\.\d+\.tests$/.test(k)));
  }

  async function addSubtask() {
    draft.subtasks.push(draft.plan !== undefined ? { points: null, title: '', tests: '', args: '' } : { points: null, title: '', tests: '' });
    await tick();
    document.getElementById(`subtask-${draft.subtasks.length - 1}-points`)?.focus();
  }

  async function removeSubtask(index: number) {
    draft.subtasks.splice(index, 1);
    // Planned tests name subtasks by number, so later ones move down.
    for (const row of draft.plan ?? []) if (row.subtask > index + 1) row.subtask--;
    // Row errors are keyed by position, so they no longer line up.
    fields = Object.fromEntries(Object.entries(fields).filter(([k]) => !k.startsWith('subtasks.') && !k.startsWith('plan.')));
    await tick();
    addSubtaskButton?.focus();
  }
</script>

<section class="tests-tab" aria-labelledby="tests-heading">
  <div class="tests-head">
    <h2 id="tests-heading" tabindex="-1">Tests</h2>
    <button type="button" class="btn btn-secondary" disabled={replacing} onclick={onupload}>
      {tests.length === 0 ? 'Upload tests' : 'Replace tests'}
    </button>
    <input
      bind:this={fileInput}
      class="file"
      type="file"
      accept=".zip,application/zip"
      aria-label="Tests to upload"
      tabindex="-1"
      onchange={onfile}
    />
  </div>
  <p class="field-help upload-help">
    A .zip of numbered input and output files, such as 01.in and 01.out. Leave an output out and Validate makes it from the
    reference solution.
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

  {#if err('samples')}<p id="samples-error" class="field-error">{err('samples')}</p>{/if}

  {#if tests.length === 0}
    <p class="muted empty">No tests yet. Upload a .zip of numbered input and output files to add them.</p>
  {:else}
    <p class="tests-summary">
      {tests.length}
      {tests.length === 1 ? 'test' : 'tests'}: {examples}
      {examples === 1 ? 'example' : 'examples'} that students see, the rest hidden.
    </p>
    {#if unassigned.length === 1}
      <p class="field-error">Test {unassigned[0]} is in no subtask. Choose its role.</p>
    {:else if unassigned.length > 1}
      <p class="field-error">Tests {rangeText(unassigned)} are in no subtask. Choose their roles.</p>
    {/if}
    {#if missing.length > 0}
      <p class="missing-note">
        {missing.length === 1 ? `Test ${missing[0]} has` : `${missing.length} tests have`} no expected output yet. Validate makes
        {missing.length === 1 ? 'it' : 'them'} from the reference solution.
      </p>
    {/if}
    <div class="table-wrap tests">
      <table class="table table-stack" aria-labelledby="tests-heading">
        <thead>
          <tr>
            <th scope="col">Test</th>
            <th scope="col">Input</th>
            <th scope="col">Output</th>
            <th scope="col">Role</th>
            <th scope="col">Download</th>
          </tr>
        </thead>
        <tbody>
          {#each tests as t (t.idx)}
            {@const role = roles.get(t.idx) ?? 'none'}
            <tr>
              <th scope="row" class="num test-num">{t.idx}</th>
              <td class="file-cell">
                <span class="cell-label">Input</span>
                <pre class="peek" translate="no">{t.input_preview}</pre>
                {#if t.input_size > t.input_preview.length}<span class="size">{formatBytes(t.input_size)} in all</span>{/if}
              </td>
              <td class="file-cell">
                <span class="cell-label">Output</span>
                {#if t.output_missing}
                  <span class="missing-word">Made on Validate</span>
                {:else}
                  <pre class="peek" translate="no">{t.output_preview}</pre>
                  {#if t.output_size > t.output_preview.length}<span class="size">{formatBytes(t.output_size)} in all</span>{/if}
                {/if}
              </td>
              <td class="role">
                <select
                  id="role-{t.idx}"
                  class="select"
                  class:unassigned={role === 'none'}
                  aria-label="Role of test {t.idx}"
                  value={roleValue(role)}
                  onchange={(e) => choose(t.idx, e.currentTarget.value)}
                >
                  <option value="example">Example</option>
                  {#each draft.subtasks.map((_s, i) => i + 1) as n (n)}<option value={String(n)}>Subtask {n}</option>{/each}
                  <option value="none">Not in a subtask</option>
                </select>
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
  {/if}

  <h3 class="sub">Subtasks</h3>
  <p class="field-help sub-help">
    Every test that is not an example belongs to one subtask. {draft.subtasks.length === 0
      ? 'Contests expect the points to add up to 100.'
      : `Points add up to ${pointsTotal}${pointsTotal !== 100 ? '; contests expect 100' : ''}.`}
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
              <label for="subtask-{i}-title">Title (optional)</label>
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
            <span class="count" class:none={counts[i] === 0}>{counts[i]} {counts[i] === 1 ? 'test' : 'tests'}</span>
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
</section>

<style>
  .tests-tab {
    min-width: 0;
  }

  .tests-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    max-width: var(--measure-wide);
  }

  .file {
    display: none;
  }

  .upload-help {
    margin-top: var(--space-1);
    max-width: var(--measure-prose);
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

  .tests {
    max-width: var(--measure-wide);
    margin-top: var(--space-3);
  }

  .tests :is(th, td) {
    vertical-align: top;
  }

  .table tbody th {
    font-weight: 500;
  }

  .role .select {
    min-width: 11rem;
  }

  .role .select.unassigned {
    border-color: var(--bad);
    color: var(--bad);
    font-weight: 500;
  }

  .file-cell {
    min-width: 10rem;
    max-width: 18rem;
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
    max-height: calc(2 * var(--lh-body) * var(--fs-0));
    margin: 0;
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

  .subtasks {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-wide);
    margin-top: var(--space-3);
    padding: 0;
    list-style: none;
  }

  .subtask {
    display: grid;
    grid-template-columns: minmax(0, 6rem) minmax(0, 6rem) minmax(0, 1fr) minmax(0, 6rem) auto;
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

  .count {
    align-self: center;
    padding-top: var(--space-5);
    color: var(--ink-muted);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    white-space: nowrap;
  }

  .count.none {
    color: var(--bad);
    font-weight: 500;
  }

  .row-error {
    grid-column: 1 / -1;
  }

  .add-row {
    margin-top: var(--space-3);
  }

  @media (width < 768px) {
    .subtask {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      grid-template-areas:
        'name remove'
        'points count'
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

    .count {
      grid-area: count;
    }

    .subtask-title {
      grid-area: title;
    }

    /* Tests stack: number and role, then input over output. */
    .tests tbody tr {
      grid-template-columns: auto minmax(0, 1fr);
    }

    .tests .role {
      grid-row: 1;
      grid-column: 2;
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
  }
</style>
