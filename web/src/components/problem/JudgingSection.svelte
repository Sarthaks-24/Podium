<!--
  JudgingSection: how answers are judged (v2 P3 F5.1, then its own section
  in F5.3 A3): the checker in plain words with its allowed error for
  numbers, the source of a custom checker, and the validator, which checks
  every test input against the constraints. Removing the validator can be
  undone until the next save (A6). Props: `draft` (bindable) and
  `fields`, the field errors by key.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import ProgramField from '../ProgramField.svelte';
  import { describedBy, programError } from '../../lib/problemFields';
  import { CHECKERS, type Draft, type Program } from '../../lib/problems';

  let { draft = $bindable(), fields }: { draft: Draft; fields: Record<string, string> } = $props();

  /** The validator just removed, kept so the removal can be undone until the next save. */
  let removed = $state<Program | null>(null);

  // A save or a reload gives the editor a new working copy; the undo ends there.
  $effect(() => {
    void draft;
    removed = null;
  });

  const help = $derived(CHECKERS.find((c) => c.id === draft.checker)?.help ?? '');

  function err(key: string): string {
    return fields[key] ?? '';
  }

  function described(key: string, more = ''): string | undefined {
    return describedBy(fields, key, more);
  }

  async function add() {
    draft.validator = { language: 'python3', source: '' };
    removed = null;
    await tick();
    document.getElementById('validator-source')?.focus();
  }

  async function remove() {
    removed = draft.validator && draft.validator.source.trim() !== '' ? draft.validator : null;
    draft.validator = null;
    await tick();
    document.getElementById(removed ? 'undo-validator' : 'add-validator')?.focus();
  }

  async function undo() {
    draft.validator = removed;
    removed = null;
    await tick();
    document.getElementById('validator-source')?.focus();
  }
</script>

<section class="block" aria-labelledby="judging-heading">
  <h2 id="judging-heading" tabindex="-1">Judging</h2>

  <div class="row">
    <div class="field">
      <label for="checker">Checker</label>
      <select
        id="checker"
        class="select"
        bind:value={draft.checker}
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

  {#if draft.checker === 'custom'}
    <div class="field custom">
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
  {/if}

  <div class="programs">
    {#if draft.validator}
      <ProgramField
        id="validator"
        label="Validator"
        help="Reads one test input and exits with 0 when it is valid. Validation runs it on every test, so a test that breaks the constraints is caught before students see it."
        bind:program={draft.validator}
        error={programError(fields, 'validator')}
        onremove={remove}
        removeLabel="Remove validator"
      />
    {:else}
      <div class="optional">
        <h3 class="program-name">Validator</h3>
        {#if removed}
          <p class="field-help">Validator removed. It goes for good when you save.</p>
          <button id="undo-validator" type="button" class="btn btn-secondary" onclick={undo}>Undo remove</button>
        {:else}
          <p class="field-help">No validator. Add one to check every test input against the constraints.</p>
          <button id="add-validator" type="button" class="btn btn-secondary" onclick={add}>Add validator</button>
        {/if}
      </div>
    {/if}
  </div>
</section>

<style>
  .row {
    margin-top: var(--space-4);
  }

  .custom {
    max-width: var(--measure-wide);
    margin-top: var(--space-4);
  }
</style>
