<!--
  JudgingSection: how the problem's answers are judged beyond the
  reference (split out of ProblemEditor in v2 P3 F5.1): the validator,
  which checks every test input against the constraints, and the source
  of a custom checker when the checker is "custom". It renders inside the
  Solutions grid (SolutionsSection). Props: `draft` (bindable), `fields`,
  and `onadd` and `onremove` for the validator.
-->
<script lang="ts">
  import ProgramField from '../ProgramField.svelte';
  import { describedBy, programError } from '../../lib/problemFields';
  import type { Draft } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
    onadd: () => void;
    onremove: () => void;
  }

  let { draft = $bindable(), fields, onadd, onremove }: Props = $props();

  const checkerError = $derived(fields.checker_source ?? '');
</script>

<div class="full">
  {#if draft.validator}
    <ProgramField
      id="validator"
      label="Validator"
      help="Reads one test input and exits with 0 when it is valid."
      bind:program={draft.validator}
      error={programError(fields, 'validator')}
      {onremove}
      removeLabel="Remove validator"
    />
  {:else}
    <div class="optional">
      <h3 class="program-name">Validator</h3>
      <p class="field-help">No validator. Add one to check inputs against the constraints.</p>
      <button id="add-validator" type="button" class="btn btn-secondary" onclick={onadd}>Add validator</button>
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
      aria-invalid={checkerError ? 'true' : undefined}
      aria-describedby={describedBy(fields, 'checker_source', 'checker-help')}
    ></textarea>
    <p id="checker-help" class="field-help">
      Run as ./checker input output answer. Exit 0 accepts, 1 or 2 rejects.
    </p>
    {#if checkerError}<p id="checker_source-error" class="field-error">{checkerError}</p>{/if}
  </div>
{/if}
