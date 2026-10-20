<!--
  SolutionsSection: the problem editor's Solutions section (v2 P3 F5.1):
  the reference solution, which every test's expected output comes from,
  and the optional brute force, a slow but simple solution validation
  compares with it (side by side from 1440 px). Adding the brute force
  focuses its source; removing it can be undone until the next save (A6).
  Props: `draft` (bindable) and `fields`, the field errors by key.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import ProgramField from '../ProgramField.svelte';
  import { programError } from '../../lib/problemFields';
  import type { Draft, Program } from '../../lib/problems';

  let { draft = $bindable(), fields }: { draft: Draft; fields: Record<string, string> } = $props();

  /** The brute force just removed, kept so the removal can be undone until the next save. */
  let removed = $state<Program | null>(null);

  // A save or a reload gives the editor a new working copy; the undo ends there.
  $effect(() => {
    void draft;
    removed = null;
  });

  async function add() {
    draft.brute = { language: 'python3', source: '' };
    removed = null;
    await tick();
    document.getElementById('brute-source')?.focus();
  }

  async function remove() {
    removed = draft.brute && draft.brute.source.trim() !== '' ? draft.brute : null;
    draft.brute = null;
    await tick();
    document.getElementById(removed ? 'undo-brute' : 'add-brute')?.focus();
  }

  async function undo() {
    draft.brute = removed;
    removed = null;
    await tick();
    document.getElementById('brute-source')?.focus();
  }
</script>

<section class="block" aria-labelledby="solutions-heading">
  <h2 id="solutions-heading" tabindex="-1">Solutions</h2>

  <div class="programs">
    <ProgramField
      id="reference"
      label="Reference solution"
      help="Your correct solution. Expected outputs can be generated from it, and it must pass every test in at most half the time limit."
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
        onremove={remove}
        removeLabel="Remove brute force"
      />
    {:else}
      <div class="optional">
        <h3 class="program-name">Brute force</h3>
        {#if removed}
          <p class="field-help">Brute force removed. It goes for good when you save.</p>
          <button id="undo-brute" type="button" class="btn btn-secondary" onclick={undo}>Undo remove</button>
        {:else}
          <p class="field-help">No brute force. Add a slow but simple solution and validation compares it with the reference.</p>
          <button id="add-brute" type="button" class="btn btn-secondary" onclick={add}>Add brute force</button>
        {/if}
      </div>
    {/if}
  </div>
</section>
