<!--
  TestsSection: the problem editor's Tests section (split out of
  ProblemEditor in v2 P3 F5.1): the sample numbers, the open test numbers
  (D115: Run checks them after the samples; no points, no subtask), subtask rows with
  points that should add up to 100, a one-line summary of the stored tests
  computed from the subtasks as typed, tests in no subtask and tests
  without an expected output named outside the table, the table itself in
  a disclosure, "Upload tests" or "Replace tests" from a .zip, and
  "Generate outputs" or "Regenerate all outputs" with the result in place.
  Uploading, generating and their confirmations stay with the editor.
  Props: `id`, `draft` and `fields` (bindable; removing a subtask drops
  the row errors, which are keyed by position), `fileInput` (bindable, the
  hidden .zip picker), `tests`, `replacing`, `replaceError`, `genBusy`,
  `genPending`, `genError`, `genSummary`, and the callbacks `onupload`,
  `ongenerate`, `onregenerate` and `onfile`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { describedBy } from '../../lib/problemFields';
  import { draftGroups, formatBytes, problemPath, type Draft, type Problem } from '../../lib/problems';

  type Report = { message: string; problems: readonly string[] } | null;

  interface Props {
    id: string;
    draft: Draft;
    fields: Record<string, string>;
    fileInput?: HTMLInputElement;
    tests: Problem['tests'];
    replacing: boolean;
    replaceError: Report;
    genBusy: boolean;
    genPending: boolean;
    genError: Report;
    genSummary: { tone: string; text: string; lines: string[] } | null;
    onupload: () => void;
    ongenerate: () => void;
    onregenerate: () => void;
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
    genBusy,
    genPending,
    genError,
    genSummary,
    onupload,
    ongenerate,
    onregenerate,
    onfile,
  }: Props = $props();

  let addSubtaskButton: HTMLButtonElement | undefined = $state();

  const pointsTotal = $derived(draft.subtasks.reduce((sum, s) => sum + (s.points ?? 0), 0));
  const groups = $derived(draftGroups(tests, draft));
  const unassigned = $derived(tests.filter((t) => groups.get(t.idx) === 'Not in a subtask').map((t) => t.idx));
  const missing = $derived(tests.filter((t) => t.output_missing).map((t) => t.idx));
  const testsSummary = $derived.by(() => {
    if (tests.length === 0) return '';
    const n = tests.length;
    const samples = tests.filter((t) => groups.get(t.idx) === 'Sample').length;
    const open = tests.filter((t) => groups.get(t.idx) === 'Open test').length;
    const named = new Set([...groups.values()].filter((g) => g.startsWith('Subtask ')));
    const inSubtasks = tests.filter((t) => groups.get(t.idx)?.startsWith('Subtask ')).length;
    const parts = [`${samples} ${samples === 1 ? 'sample' : 'samples'}`];
    if (open > 0) parts.push(`${open} ${open === 1 ? 'open test' : 'open tests'}`);
    if (inSubtasks > 0) parts.push(`${inSubtasks} in ${named.size} ${named.size === 1 ? 'subtask' : 'subtasks'}`);
    if (unassigned.length > 0) parts.push(`${unassigned.length} in no subtask`);
    return `${n} ${n === 1 ? 'test' : 'tests'}: ${parts.join(', ')}.`;
  });

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, help = ''): string | undefined {
    return describedBy(fields, key, help);
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

  <div class="field samples">
    <label for="open">Open tests (optional)</label>
    <input
      id="open"
      class="input mono"
      type="text"
      autocomplete="off"
      spellcheck="false"
      placeholder="7, 8"
      bind:value={draft.open}
      aria-invalid={err('open') ? 'true' : undefined}
      aria-describedby={described('open', 'open-help')}
    />
    <p id="open-help" class="field-help">
      Run checks these after the samples and shows a failing one in full. They give no points and belong to no subtask.
    </p>
    {#if err('open')}<p id="open-error" class="field-error">{err('open')}</p>{/if}
  </div>

  <h3 class="sub">Subtasks</h3>
  <p class="field-help sub-help">
    Every test that is not a sample or an open test belongs to exactly one subtask. Points add up to {pointsTotal}{pointsTotal !== 100 && draft.subtasks.length > 0 ? '; contests expect 100' : ''}.
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
      <button type="button" class="btn btn-secondary" disabled={replacing} onclick={onupload}>
        {tests.length === 0 ? 'Upload tests' : 'Replace tests'}
      </button>
      {#if tests.length > 0}
        {#if missing.length > 0}
          <button type="button" class="btn btn-secondary" disabled={genBusy || genPending} onclick={ongenerate}>
            Generate outputs
          </button>
        {/if}
        <button
          type="button"
          class="btn btn-quiet"
          aria-disabled={genBusy || genPending ? 'true' : undefined}
          onclick={() => {
            if (!genBusy && !genPending) onregenerate();
          }}
        >
          Regenerate all outputs
        </button>
      {/if}
    </div>
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

  {#if tests.length === 0}
    <p class="muted empty">No tests yet. Upload a .zip of numbered input and output files to add them.</p>
  {:else}
    <p class="tests-summary">{testsSummary}</p>
    {#each unassigned as idx (idx)}
      <p class="field-error">Test {idx} is in no subtask. Add it to a subtask range, to the samples or to the open tests.</p>
    {/each}
    {#if missing.length > 0}
      <p class="missing-note">
        {missing.length === 1 ? `Test ${missing[0]} has` : `${missing.length} tests have`} no expected output. Generate outputs to fill
        {missing.length === 1 ? 'it' : 'them'} from the reference solution.
      </p>
    {/if}
    <details class="all-tests">
      <summary>Show all {tests.length} {tests.length === 1 ? 'test' : 'tests'}</summary>
      <div class="table-wrap tests">
        <table class="table table-stack">
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
            {#each tests as t (t.idx)}
              {@const g = groups.get(t.idx) ?? ''}
              <tr>
                <th scope="row" class="num test-num">{t.idx}</th>
                <td class="group" class:unassigned={g === 'Not in a subtask'}>{g}</td>
                <td class="file-cell">
                  <span class="cell-label">Input</span>
                  <span class="size">{formatBytes(t.input_size)}</span>
                  <pre class="peek" translate="no">{t.input_preview}</pre>
                </td>
                <td class="file-cell">
                  <span class="cell-label">Output</span>
                  {#if t.output_missing}
                    <span class="missing-word">Output missing</span>
                  {:else}
                    <span class="size">{formatBytes(t.output_size)}</span>
                    <pre class="peek" translate="no">{t.output_preview}</pre>
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

<style>
  .samples {
    max-width: 24rem;
    margin-top: var(--space-4);
  }

  .subtasks {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-wide);
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
    max-width: var(--measure-wide);
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

  .all-tests {
    max-width: var(--measure-wide);
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

  @media (width < 768px) {
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

    .tests tbody tr {
      grid-template-columns: auto minmax(0, 1fr);
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
