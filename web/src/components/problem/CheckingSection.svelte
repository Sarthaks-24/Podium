<!--
  CheckingSection: the problem editor's Checking tab, above the validation
  report: the reference solution, which every expected output comes from,
  and how answers are compared (with the allowed error for numbers). The
  brute force, the input validator, the custom checker's source and, on a
  server that has them, the generator sit under one folded "Optional extra
  checks", since most problems need none of them; while it is folded, each
  one in use shows one line ("Brute force in Python 3"). Choosing the
  custom checker unfolds it and focuses the source. Removing the brute
  force or the validator can be undone until the next save. Props:
  `draft` (bindable), `fields` (the field errors by key) and `generator`
  (a snippet drawing the generator, or none).
-->
<script lang="ts">
  import { tick, type Snippet } from 'svelte';
  import ProgramField from '../ProgramField.svelte';
  import { describedBy, programError } from '../../lib/problemFields';
  import { CHECKERS, LANGUAGES, type Draft, type Program } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
    generator?: Snippet;
  }

  let { draft = $bindable(), fields, generator }: Props = $props();

  let open = $state(false);
  /** The brute force or validator just removed, kept so the removal can be undone until the next save. */
  let removed = $state<{ brute: Program | null; validator: Program | null }>({ brute: null, validator: null });

  // A save or a reload gives the editor a new working copy; the undo ends there.
  $effect(() => {
    void draft;
    removed = { brute: null, validator: null };
  });

  const help = $derived(CHECKERS.find((c) => c.id === draft.checker)?.help ?? '');

  const languageOf = (p: Program) => LANGUAGES.find((l) => l.id === p.language)?.label ?? p.language;

  /** One line per extra check in use, shown while the section is folded. */
  const used = $derived.by(() => {
    const out: string[] = [];
    if (draft.brute) out.push(`Brute force in ${languageOf(draft.brute)}`);
    if (draft.validator) out.push(`Validator in ${languageOf(draft.validator)}`);
    if (draft.checker === 'custom') out.push('Custom checker in C++17 with testlib');
    if (draft.generator) {
      const n = draft.plan?.length ?? 0;
      out.push(`Generator in ${languageOf(draft.generator)}, ${n} planned ${n === 1 ? 'test' : 'tests'}`);
    }
    return out;
  });

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, more = ''): string | undefined {
    return describedBy(fields, key, more);
  }

  async function onChecker() {
    if (draft.checker !== 'custom') return;
    open = true;
    await tick();
    document.getElementById('checker_source')?.focus();
  }

  async function add(role: 'brute' | 'validator') {
    draft[role] = { language: 'python3', source: '' };
    removed[role] = null;
    await tick();
    document.getElementById(`${role}-source`)?.focus();
  }

  async function remove(role: 'brute' | 'validator') {
    const program = draft[role];
    removed[role] = program && program.source.trim() !== '' ? program : null;
    draft[role] = null;
    await tick();
    document.getElementById(removed[role] ? `undo-${role}` : `add-${role}`)?.focus();
  }

  async function undo(role: 'brute' | 'validator') {
    draft[role] = removed[role];
    removed[role] = null;
    await tick();
    document.getElementById(`${role}-source`)?.focus();
  }
</script>

{#snippet none(role: 'brute' | 'validator', name: string, text: string)}
  <div class="optional">
    <h3 class="program-name">{name}</h3>
    {#if removed[role]}
      <p class="field-help">{name} removed. It goes for good when you save.</p>
      <button id="undo-{role}" type="button" class="btn btn-secondary" onclick={() => undo(role)}>Undo remove</button>
    {:else}
      <p class="field-help">{text}</p>
      <button id="add-{role}" type="button" class="btn btn-secondary" onclick={() => add(role)}>Add {name.toLowerCase()}</button>
    {/if}
  </div>
{/snippet}

<section class="checking" aria-labelledby="checking-heading">
  <h2 id="checking-heading" class="visually-hidden" tabindex="-1">Checking</h2>

  <div class="programs one">
    <ProgramField
      id="reference"
      label="Reference solution"
      help="Your correct solution. Validate makes missing expected outputs from it, and it must pass every test in at most half the time limit."
      bind:program={draft.reference}
      error={programError(fields, 'reference')}
    />
  </div>

  <div class="row">
    <div class="field">
      <label for="checker">How answers are compared</label>
      <select
        id="checker"
        class="select"
        bind:value={draft.checker}
        onchange={onChecker}
        aria-invalid={err('checker') ? 'true' : undefined}
        aria-describedby={described('checker', 'checker-choice-help')}
      >
        {#each CHECKERS as c (c.id)}<option value={c.id}>{c.label}</option>{/each}
      </select>
      <p id="checker-choice-help" class="field-help">{help}</p>
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
        <p id="epsilon-help" class="field-help">How far a number may be from the expected one, absolute or relative, for example 0.000001.</p>
        {#if err('epsilon')}<p id="epsilon-error" class="field-error">{err('epsilon')}</p>{/if}
      </div>
    {/if}
  </div>

  <details class="extras" bind:open>
    <summary>Optional extra checks</summary>

    <div class="programs">
      {#if draft.brute}
        <ProgramField
          id="brute"
          label="Brute force"
          help="A slow but simple solution. Validation checks that it agrees with the reference."
          bind:program={draft.brute}
          error={programError(fields, 'brute')}
          onremove={() => remove('brute')}
          removeLabel="Remove brute force"
        />
      {:else}
        {@render none('brute', 'Brute force', 'None. Add a slow but simple solution and validation compares it with the reference.')}
      {/if}
      {#if draft.validator}
        <ProgramField
          id="validator"
          label="Validator"
          help="Reads one test input and exits with 0 when it is valid. Validation runs it on every test, so a test that breaks the constraints is caught before students see it."
          bind:program={draft.validator}
          error={programError(fields, 'validator')}
          onremove={() => remove('validator')}
          removeLabel="Remove validator"
        />
      {:else}
        {@render none('validator', 'Validator', 'None. Add one to check every test input against the constraints.')}
      {/if}
    </div>

    <div class="custom">
      <h3 class="program-name">Custom checker</h3>
      {#if draft.checker === 'custom'}
        <div class="field">
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
          <p id="checker-help" class="field-help">Run as ./checker input output answer. Exit 0 accepts, 1 or 2 rejects.</p>
          {#if err('checker_source')}<p id="checker_source-error" class="field-error">{err('checker_source')}</p>{/if}
        </div>
      {:else}
        <p class="field-help">None. For problems with more than one right answer, choose Custom checker under How answers are compared.</p>
      {/if}
    </div>

    {#if generator}{@render generator()}{/if}
  </details>
  {#if !open}
    <ul class="used" aria-label="Optional extra checks in use">
      {#each used as line (line)}<li>{line}</li>{:else}<li class="muted">None in use.</li>{/each}
    </ul>
  {/if}
</section>

<style>
  /* One column that never grows past the page: the generator's wide table scrolls inside it. */
  .checking {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-5);
    min-width: 0;
  }

  .programs.one {
    margin-top: 0;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
    max-width: var(--measure-wide);
  }

  .row > .field {
    flex: 1 1 14rem;
    min-width: 0;
  }

  .extras {
    max-width: var(--measure-wide);
    border-top: var(--rule) solid var(--line);
    padding-top: var(--space-4);
  }

  .extras > summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    font-weight: 500;
    cursor: pointer;
  }

  .custom {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-6);
  }

  .used {
    margin-top: calc(-1 * var(--space-3));
    padding: 0;
    list-style: none;
    font-size: var(--fs-0);
  }
</style>
