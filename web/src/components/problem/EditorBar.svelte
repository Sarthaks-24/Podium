<!--
  EditorBar: the problem editor's sticky bar (split out of ProblemEditor in
  v2 P3 F5.1). It keeps the status in words ("Validated 16 Oct 19:42",
  "Validation failed, 2 problems", "Validating, 3rd in queue", "Not
  validated", each a link to the report), "AI draft", "Unsaved changes",
  the count of fields that need attention, jump links to each section, and
  Save changes and Validate; the button that matters now is the primary
  one. Save changes submits the editor's form. Props: `status` (from
  lib/problems editorStatus), `aiDraft`, `dirty`, `fieldCount`,
  `hasGenerator`, `saving`, `busy` (Validate is disabled), `validateLabel`,
  `validatePrimary` and `onvalidate`.
-->
<script lang="ts">
  interface Props {
    status: { tone: string; text: string } | null;
    aiDraft: boolean;
    dirty: boolean;
    fieldCount: number;
    hasGenerator: boolean;
    saving: boolean;
    busy: boolean;
    validateLabel: string;
    validatePrimary: boolean;
    onvalidate: () => void;
  }

  let { status, aiDraft, dirty, fieldCount, hasGenerator, saving, busy, validateLabel, validatePrimary, onvalidate }: Props = $props();
</script>

<div class="action-bar">
  <div class="bar-status">
    {#if status}
      <a class="status tone-{status.tone}" href="#validation-heading">
        {#if status.tone === 'ok'}<span class="dot" aria-hidden="true"></span>{/if}{status.text}
      </a>
    {/if}
    {#if aiDraft}<span class="ai-draft">AI draft</span>{/if}
    {#if dirty}<span class="unsaved">Unsaved changes</span>{/if}
    {#if fieldCount > 0}
      <a class="error-count" href="#field-errors">
        {fieldCount} {fieldCount === 1 ? 'field needs' : 'fields need'} attention
      </a>
    {/if}
  </div>
  <nav class="jumps" aria-label="Sections">
    <a href="#details-heading">Details</a>
    <a href="#tests-heading">Tests</a>
    {#if hasGenerator}<a href="#generator-heading">Generator</a>{/if}
    <a href="#solutions-heading">Solutions</a>
    <a href="#validation-heading">Validation</a>
    <a href="#history-heading">History</a>
  </nav>
  <div class="bar-actions">
    <button type="submit" form="problem-form" class="btn {dirty ? 'btn-primary' : 'btn-secondary'}" disabled={saving}>
      Save changes
    </button>
    <button
      type="button"
      class="btn {validatePrimary ? 'btn-primary' : 'btn-secondary'}"
      disabled={busy}
      onclick={onvalidate}
    >
      {validateLabel}
    </button>
  </div>
</div>

<style>
  /* The sticky bar keeps status and the two main actions in reach. */
  .action-bar {
    position: sticky;
    top: 0;
    z-index: 10;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-5);
    margin-top: var(--space-4);
    padding-block: var(--space-2);
    border-block: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .bar-status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-4);
    min-width: 0;
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border-radius: var(--radius);
    font-weight: 500;
    text-decoration: none;
  }

  .status:hover {
    text-decoration: underline;
  }

  .tone-ok {
    color: var(--ok);
  }

  .tone-bad {
    color: var(--bad);
  }

  .tone-wait,
  .tone-note {
    color: var(--ink-muted);
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: currentColor;
  }

  .unsaved {
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .error-count {
    border-radius: var(--radius);
    color: var(--bad);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .jumps {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    font-size: var(--fs-0);
  }

  .jumps a {
    border-radius: var(--radius);
    color: var(--ink-muted);
  }

  .jumps a:hover {
    color: var(--ink);
  }

  .bar-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-left: auto;
  }

  .ai-draft {
    padding: 0 var(--space-1);
    border: var(--rule) solid var(--part);
    border-radius: var(--radius);
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 500;
    white-space: nowrap;
  }

  @media (width < 768px) {
    .jumps {
      display: none;
    }

    .bar-actions {
      margin-left: 0;
      width: 100%;
    }

    .bar-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
