<!--
  GeneratorPanel: the problem editor's Generator part, inside the Checking
  tab's "Optional extra checks" (M5, split out of ProblemEditor in v2 P3
  F5.1): the generator program, its arguments per
  subtask, and the plan as a compact table of test, subtask, seed and
  arguments with rows to add and remove, then "Generate inputs" with its
  result in place (failures name the test, subtask and seed). It edits the
  working copy in place; generating inputs, and the polling that follows,
  stay with the editor. Props: `draft` and `fields` (bindable: removing a
  row drops the row errors, which are keyed by position), `testIdx` (the
  stored test numbers), `dirty`, `saving`, `busy`, `pending`, `error` and
  `report` (the input generation's state and result), and
  `ongenerate`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import ProgramField from '../ProgramField.svelte';
  import { describedBy, planErrors, programError } from '../../lib/problemFields';
  import { nextPlanRow, type Draft, type Program } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
    testIdx: number[];
    dirty: boolean;
    saving: boolean;
    busy: boolean;
    pending: boolean;
    error: { message: string; problems: readonly string[] } | null;
    report: { tone: string; text: string; lines: string[] } | null;
    ongenerate: () => void;
  }

  let {
    draft = $bindable(),
    fields = $bindable(),
    testIdx,
    dirty,
    saving,
    busy,
    pending,
    error,
    report,
    ongenerate,
  }: Props = $props();

  let addPlanButton: HTMLButtonElement | undefined = $state();

  /** The generator just removed, kept so the removal can be undone until the next save (A6). */
  let removed = $state<Program | null>(null);

  // A save or a reload gives the editor a new working copy; the undo ends there.
  $effect(() => {
    void draft;
    removed = null;
  });

  /** With no generator and no plan the section is one line (audit O2). */
  const unused = $derived(!draft.generator && (draft.plan?.length ?? 0) === 0);

  const canGenerateInputs = $derived(!!draft.generator && (draft.plan?.length ?? 0) > 0);

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, help = ''): string | undefined {
    return describedBy(fields, key, help);
  }

  async function addGenerator() {
    removed = null;
    draft.generator = {
      language: 'python3',
      source: 'import random\nimport sys\n\nseed = int(sys.argv[1])\nargs = dict(a.split("=", 1) for a in sys.argv[2:])\nrng = random.Random(seed)\n',
    };
    await tick();
    document.getElementById('generator-source')?.focus();
  }

  async function removeGenerator() {
    removed = draft.generator && draft.generator.source.trim() !== '' ? draft.generator : null;
    draft.generator = null;
    await tick();
    document.getElementById(removed ? 'undo-generator' : 'add-generator')?.focus();
  }

  async function undoRemove() {
    draft.generator = removed;
    removed = null;
    await tick();
    document.getElementById('generator-source')?.focus();
  }

  async function addPlanRow() {
    if (draft.plan === undefined) return;
    draft.plan.push(nextPlanRow(draft.plan, testIdx, draft.subtasks.length));
    await tick();
    document.getElementById(`plan-${draft.plan.length - 1}-seed`)?.focus();
  }

  async function removePlanRow(index: number) {
    if (draft.plan === undefined) return;
    draft.plan.splice(index, 1);
    fields = Object.fromEntries(Object.entries(fields).filter(([k]) => !k.startsWith('plan.')));
    await tick();
    addPlanButton?.focus();
  }
</script>

{#if draft.plan !== undefined}
  <section class="generator" aria-labelledby="generator-heading">
    <h3 id="generator-heading" class="program-name" tabindex="-1">Generator</h3>
    {#if unused}
      <div class="optional one-line">
        {#if removed}
          <p class="field-help">Generator removed. It goes for good when you save.</p>
          <button id="undo-generator" type="button" class="btn btn-secondary" onclick={undoRemove}>Undo remove</button>
        {:else}
          <p class="field-help">No generator. Add one to make test inputs from seeds instead of uploading them.</p>
          <button id="add-generator" type="button" class="btn btn-secondary" onclick={addGenerator}>Add generator</button>
        {/if}
      </div>
    {:else}
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
          error={programError(fields, 'generator')}
          onremove={removeGenerator}
          removeLabel="Remove generator"
        />
      {:else}
        <div class="optional">
          <h4 class="program-name">Generator program</h4>
          <p class="field-help">No generator. Add one to make test inputs from seeds instead of uploading them.</p>
          <button id="add-generator" type="button" class="btn btn-secondary" onclick={addGenerator}>Add generator</button>
        </div>
      {/if}
    </div>

    <h4 class="sub">Arguments per subtask</h4>
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

    <h4 class="sub" id="plan-heading">Planned tests</h4>
    <p class="field-help sub-help">
      Each row makes one test from a seed and optional arguments of its own. Examples come from the statement, so leave their
      numbers out.
    </p>
    {#if err('plan')}<p id="plan-error" class="field-error">{err('plan')}</p>{/if}

    {#if draft.plan.length > 0}
      <div class="table-wrap plan">
        <table class="table table-stack" aria-labelledby="plan-heading">
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
              {@const rowErrors = planErrors(fields, i)}
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
        disabled={busy || pending || saving || !canGenerateInputs}
        aria-describedby={canGenerateInputs ? undefined : 'generate-inputs-help'}
        onclick={ongenerate}
      >
        {pending ? 'Generating inputs' : dirty ? 'Save and generate inputs' : 'Generate inputs'}
      </button>
      {#if !canGenerateInputs}
        <p id="generate-inputs-help" class="field-help">Add a generator and at least one planned test first.</p>
      {/if}
    </div>

    {/if}

    <div class="generation" aria-live="polite">
      {#if error}
        <div class="banner banner-error problems-list">
          <p>{error.message}</p>
          {#if error.problems.length > 0}
            <ul>{#each error.problems as line, i (i)}<li>{line}</li>{/each}</ul>
          {/if}
        </div>
      {:else if report}
        <p class="gen-line tone-{report.tone}">{report.text}</p>
        {#if report.lines.length > 0}
          <ul class="gen-failures">{#each report.lines as line, i (i)}<li>{line}</li>{/each}</ul>
        {/if}
      {/if}
    </div>
  </section>
{/if}

<style>
  .generator {
    margin-top: var(--space-6);
  }

  .generator h4.sub {
    margin-top: var(--space-5);
    font-size: var(--fs-1);
  }

  .one-line {
    margin-top: var(--space-2);
  }

  .arg-rows {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 18rem), 1fr));
    gap: var(--space-3) var(--space-4);
    max-width: var(--measure-wide);
    margin-top: var(--space-3);
  }

  .plan {
    max-width: var(--measure-wide);
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
    max-width: var(--measure-wide);
    margin-top: var(--space-4);
  }

  @media (width < 768px) {
    .plan-actions .btn {
      flex: 1 1 auto;
    }

    /* Planned tests stack: test and subtask, then seed and arguments. */

    .plan tbody tr {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
      grid-template-areas:
        'idx subtask remove'
        'seed args args';
      align-items: end;
      gap: var(--space-2) var(--space-3);
    }

    .plan tbody tr.has-errors {
      border-bottom: 0;
      padding-bottom: 0;
    }

    .plan tbody tr.plan-error-row {
      display: block;
      padding-top: var(--space-1);
    }

    .plan tbody :is(th, td) {
      display: grid;
      gap: var(--space-1);
      width: auto;
      min-width: 0;
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
  }
</style>
