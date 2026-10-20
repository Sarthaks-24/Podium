<!--
  SolutionsSection: the problem editor's Solutions section (split out of
  ProblemEditor in v2 P3 F5.1): the reference solution, the optional brute
  force (reference and brute force side by side from 1440 px), then the
  validator and custom checker (JudgingSection). Adding a program focuses
  its source; removing one focuses its Add button. Props: `draft`
  (bindable) and `fields`, the field errors by key.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import ProgramField from '../ProgramField.svelte';
  import JudgingSection from './JudgingSection.svelte';
  import { programError } from '../../lib/problemFields';
  import type { Draft } from '../../lib/problems';

  let { draft = $bindable(), fields }: { draft: Draft; fields: Record<string, string> } = $props();

  async function addProgram(role: 'brute' | 'validator') {
    draft[role] = { language: 'python3', source: '' };
    await tick();
    document.getElementById(`${role}-source`)?.focus();
  }

  async function removeProgram(role: 'brute' | 'validator') {
    draft[role] = null;
    await tick();
    document.getElementById(`add-${role}`)?.focus();
  }
</script>

<section class="block" aria-labelledby="solutions-heading">
  <h2 id="solutions-heading" tabindex="-1">Solutions</h2>

  <div class="programs">
    <ProgramField
      id="reference"
      label="Reference solution"
      help="Must pass every test in at most half the time limit."
      bind:program={draft.reference}
      error={programError(fields, 'reference')}
    />

    {#if draft.brute}
      <ProgramField
        id="brute"
        label="Brute force"
        help="A slow but simple solution. Validation checks that it agrees with the reference."
        bind:program={draft.brute}
        error={programError(fields, 'brute')}
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

    <JudgingSection bind:draft {fields} onadd={() => addProgram('validator')} onremove={() => removeProgram('validator')} />
  </div>
</section>
