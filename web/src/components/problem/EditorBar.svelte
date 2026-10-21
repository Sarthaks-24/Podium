<!--
  EditorBar: the problem editor's sticky bar (v2 P3 F5.1). It keeps the
  status in words ("Validated 16 Oct 19:42", "Validation failed, 2
  problems", "Validating, 3rd in queue", "Not validated", each a link to
  the report), "AI draft" as a badge, "Unsaved changes", the count of
  fields that need attention, and Save changes and Validate; the button
  that matters now is the primary one. Once a validation finishes while
  the editor is open, the result takes one line in the bar with "See the
  report", which moves focus to the report (audit 2026-10-20 O2). Under it
  the jump links name each section with a short state (A3); on phones they
  are one row that scrolls sideways. Save changes submits the editor's
  form. While it is on screen the bar publishes its real height, wrapped
  rows and touch-sized controls included, as --sticky-offset on the root,
  so the sticky preview and context column sit under it (R3). Props: `status` (lib/problems editorStatus), `aiDraft`, `dirty`,
  `fieldCount`, `jumps` ({id, label, state}), `saving`, `busy` (Validate is
  disabled), `validateLabel`, `validatePrimary`, `result`
  (validationResultLine, or null to show none), `onvalidate` and
  `onreport`.
-->
<script lang="ts">
  import Badge from '../Badge.svelte';

  interface Props {
    status: { tone: string; text: string } | null;
    aiDraft: boolean;
    dirty: boolean;
    fieldCount: number;
    jumps: { id: string; label: string; state: string }[];
    saving: boolean;
    busy: boolean;
    validateLabel: string;
    validatePrimary: boolean;
    result: { tone: 'ok' | 'bad'; text: string } | null;
    onvalidate: () => void;
    onreport: (event: MouseEvent) => void;
  }

  let { status, aiDraft, dirty, fieldCount, jumps, saving, busy, validateLabel, validatePrimary, result, onvalidate, onreport }: Props = $props();

  let bar: HTMLDivElement | undefined = $state();
  $effect(() => {
    const el = bar;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const root = document.documentElement;
    const ro = new ResizeObserver(() => root.style.setProperty('--sticky-offset', `${el.offsetHeight}px`));
    ro.observe(el);
    return () => {
      ro.disconnect();
      root.style.removeProperty('--sticky-offset');
    };
  });
</script>

<div class="action-bar" bind:this={bar}>
  <div class="bar-row">
    <div class="bar-status">
      {#if status}
        <a class="status tone-{status.tone}" href="#validation-heading">
          {#if status.tone === 'ok'}<span class="dot" aria-hidden="true"></span>{/if}{status.text}
        </a>
      {/if}
      {#if aiDraft}<Badge tone="wait" text="AI draft" />{/if}
      {#if dirty}<span class="unsaved">Unsaved changes</span>{/if}
      {#if fieldCount > 0}
        <a class="error-count" href="#field-errors">
          {fieldCount} {fieldCount === 1 ? 'field needs' : 'fields need'} attention
        </a>
      {/if}
    </div>
    <div class="bar-actions">
      <button type="submit" form="problem-form" class="btn {dirty ? 'btn-primary' : 'btn-secondary'}" disabled={saving}>
        Save changes
      </button>
      <button type="button" class="btn {validatePrimary ? 'btn-primary' : 'btn-secondary'}" disabled={busy} onclick={onvalidate}>
        {validateLabel}
      </button>
    </div>
  </div>
  {#if result}
    <p class="bar-result">
      <span class="tone-{result.tone}">{result.text}</span>
      <a href="#validation-heading" onclick={onreport}>See the report</a>
    </p>
  {/if}
  <nav class="jumps" aria-label="Sections">
    {#each jumps as j (j.id)}
      <a href="#{j.id}">{j.label}{#if j.state}<span class="jump-state">{` ${j.state}`}</span>{/if}</a>
    {/each}
  </nav>
</div>

<style>
  /* The sticky bar keeps status and the two main actions in reach. */
  .action-bar {
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
    margin-top: var(--space-4);
    padding-block: var(--space-2);
    border-block: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .bar-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-5);
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

  .bar-result {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--space-3);
    font-weight: 500;
  }

  .jumps {
    display: flex;
    flex-wrap: wrap;
    min-width: 0;
    gap: var(--space-1) var(--space-4);
    font-size: var(--fs-0);
  }

  .jumps a {
    border-radius: var(--radius);
    color: var(--ink);
    text-decoration: none;
    white-space: nowrap;
  }

  .jumps a:hover {
    text-decoration: underline;
  }

  .jump-state {
    color: var(--ink-muted);
  }

  .bar-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-left: auto;
  }

  @media (width < 768px) {
    /* One row that scrolls sideways inside the bar, never the page. */
    .jumps {
      flex-wrap: nowrap;
      overflow-x: auto;
      overscroll-behavior-x: contain;
      padding-bottom: var(--space-1);
    }

    .jumps a {
      display: inline-flex;
      align-items: center;
      gap: var(--space-1);
      min-height: var(--control-h);
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
