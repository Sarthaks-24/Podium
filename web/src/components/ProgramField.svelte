<!--
  ProgramField: one program of a problem (reference solution, brute force or
  validator) in the problem editor: a group named by `label` holding a
  language select and a mono source textarea (the student workspace uses
  CodeMirror; setters keep the plain textarea for now). Props: `id` (prefix for the control ids), `label` (the
  group's name), `program` (bindable {language, source}), `help` (one line
  under the name, optional), `error` (a sentence shown under the source
  and tied to it, optional), and `onremove` with `removeLabel` (adds a
  quiet remove button for optional programs).
-->
<script lang="ts">
  import { LANGUAGES, type Program } from '../lib/problems';

  interface Props {
    id: string;
    label: string;
    program: Program;
    help?: string;
    error?: string;
    onremove?: () => void;
    removeLabel?: string;
  }

  let { id, label, program = $bindable(), help = '', error = '', onremove, removeLabel = 'Remove' }: Props = $props();

  const describedBy = $derived(
    [help !== '' ? `${id}-help` : '', error !== '' ? `${id}-error` : ''].filter((s) => s !== '').join(' ') || undefined,
  );
</script>

<div class="program" role="group" aria-labelledby="{id}-label">
  <div class="program-head">
    <h3 id="{id}-label" class="program-name">{label}</h3>
    {#if onremove}
      <button type="button" class="btn btn-quiet btn-danger-quiet btn-compact" onclick={onremove}>{removeLabel}</button>
    {/if}
  </div>
  {#if help !== ''}<p id="{id}-help" class="field-help">{help}</p>{/if}

  <div class="field language">
    <label for="{id}-language">Language</label>
    <select id="{id}-language" class="select" bind:value={program.language}>
      {#each LANGUAGES as lang (lang.id)}
        <option value={lang.id}>{lang.label}</option>
      {/each}
    </select>
  </div>

  <div class="field">
    <label for="{id}-source">Source code</label>
    <textarea
      id="{id}-source"
      class="input textarea-code textarea-well"
      rows="12"
      spellcheck="false"
      autocapitalize="off"
      autocomplete="off"
      bind:value={program.source}
      aria-invalid={error !== '' ? 'true' : undefined}
      aria-describedby={describedBy}
    ></textarea>
  </div>
  {#if error !== ''}<p id="{id}-error" class="field-error">{error}</p>{/if}
</div>

<style>
  .program {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .program-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .program-name {
    font-size: var(--fs-2);
  }

  .program-head + .field-help {
    margin-top: calc(-1 * var(--space-2));
  }

  .language {
    max-width: 16rem;
  }
</style>
