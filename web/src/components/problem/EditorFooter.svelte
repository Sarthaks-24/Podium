<!--
  EditorFooter: the end of the problem editor's form (split out of
  ProblemEditor in v2 P3 F5.1): the "Before you validate" warnings, the
  save error with a link to each field that needs attention, Download pack
  and Archive problem. Props: `id` (the problem id from the address),
  `warnings`, `archived`, `saveError`, `fieldList` (lib/problemFields
  FieldError) and `onarchive`, which opens the editor's archive
  confirmation.
-->
<script lang="ts">
  import type { FieldError } from '../../lib/problemFields';
  import { problemPath } from '../../lib/problems';

  interface Props {
    id: string;
    warnings: readonly string[];
    archived: boolean;
    saveError: string;
    fieldList: FieldError[];
    onarchive: () => void;
  }

  let { id, warnings, archived, saveError, fieldList, onarchive }: Props = $props();

  function focusField(event: MouseEvent, target: string | null) {
    if (!target) return;
    const el = document.getElementById(target);
    if (!el) return;
    event.preventDefault();
    el.scrollIntoView({ block: 'center' });
    el.focus({ preventScroll: true });
  }
</script>

<div class="footer">
  {#if warnings.length > 0}
    <div class="warnings" role="note" aria-labelledby="warnings-title">
      <p id="warnings-title" class="warnings-title">Before you validate</p>
      <ul>{#each warnings as warning, i (i)}<li>{warning}</li>{/each}</ul>
    </div>
  {/if}

  {#if saveError !== ''}
    <div id="field-errors" class="banner banner-error problems-list" role="alert">
      <p>{saveError}</p>
      {#if fieldList.length > 0}
        <ul>
          {#each fieldList as f (f.key)}
            <li>
              {#if f.id}
                <a href="#{f.id}" onclick={(e) => focusField(e, f.id)}>{f.label}: {f.message}</a>
              {:else}
                {f.label ? `${f.label}: ` : ''}{f.message}
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  <div class="actions">
    <a class="btn btn-secondary" href={problemPath(id, '/export')} download>Download pack</a>
    {#if !archived}
      <button type="button" class="btn btn-quiet btn-danger-quiet archive" onclick={onarchive}>Archive problem</button>
    {/if}
  </div>
</div>

<style>
  .footer {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-7);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .warnings-title {
    font-weight: 600;
  }

  .warnings {
    max-width: 48rem;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--part);
    background: var(--part-tint);
    color: var(--ink);
  }

  .warnings ul {
    padding-left: var(--space-5);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    max-width: 64rem;
  }

  .archive {
    margin-left: auto;
  }

  @media (width < 768px) {
    .actions .btn {
      flex: 1 1 auto;
    }

    /* Archive sits apart, below the other actions, after a rule. */
    .actions .archive {
      flex-basis: 100%;
      justify-content: flex-start;
      margin-left: 0;
      margin-top: var(--space-3);
      border-top: var(--rule) solid var(--line);
      border-radius: 0;
      padding-top: var(--space-3);
      min-height: calc(var(--control-h) + var(--space-3));
    }
  }
</style>
