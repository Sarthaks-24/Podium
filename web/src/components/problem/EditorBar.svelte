<!--
  EditorBar: the problem editor's sticky bar: the three tabs (Statement,
  Tests with its count, Checking) on the shared Tabs, then "AI draft" as a
  badge, "Unsaved changes", the count of fields that need attention (a link
  to the list), Save changes and one more button. That button is Validate,
  the primary action until the problem passes; once it has passed and
  nothing is unsaved, "Add to contest" takes its place. Save changes
  submits the editor's form. While it is on screen the bar publishes its
  real height as --sticky-bar-h on the root, so the sticky preview sits
  under it, and, as a data-sticky-bar, anything focused or jumped to lands
  below it (base.css). Props: `tab`, `tabs` ({ id, label, count? }),
  `ontab`, `aiDraft`, `dirty`, `fieldCount`, `saving`, `busy` (Validate is
  disabled), `validateLabel`, `validatePrimary`, `ready` (show Add to
  contest), `onvalidate` and `onadd`.
-->
<script lang="ts">
  import Badge from '../Badge.svelte';
  import Tabs from '../Tabs.svelte';
  import { publishHeight } from '../../lib/publishHeight';

  type TabId = 'statement' | 'tests' | 'checking';

  interface Props {
    tab: TabId;
    tabs: { id: TabId; label: string; count?: number }[];
    ontab: (id: TabId) => void;
    aiDraft: boolean;
    dirty: boolean;
    fieldCount: number;
    saving: boolean;
    busy: boolean;
    validateLabel: string;
    validatePrimary: boolean;
    ready: boolean;
    onvalidate: () => void;
    onadd: () => void;
  }

  let { tab, tabs, ontab, aiDraft, dirty, fieldCount, saving, busy, validateLabel, validatePrimary, ready, onvalidate, onadd }: Props = $props();
</script>

<div class="action-bar" data-sticky-bar use:publishHeight={'--sticky-bar-h'}>
  <Tabs {tabs} selected={tab} onselect={ontab} label="Problem" idPrefix="tab" controls={(id) => `panel-${id}`} />
  <div class="bar-end">
    {#if aiDraft}<Badge tone="wait" text="AI draft" />{/if}
    {#if dirty}<span class="unsaved">Unsaved changes</span>{/if}
    {#if fieldCount > 0}
      <a class="error-count" href="#field-errors">
        {fieldCount} {fieldCount === 1 ? 'field needs' : 'fields need'} attention
      </a>
    {/if}
    <div class="bar-actions">
      <button type="submit" form="problem-form" class="btn {dirty ? 'btn-primary' : 'btn-secondary'}" disabled={saving}>
        Save changes
      </button>
      {#if ready}
        <button type="button" class="btn btn-primary" onclick={onadd}>Add to contest</button>
      {:else}
        <button type="button" class="btn {validatePrimary ? 'btn-primary' : 'btn-secondary'}" disabled={busy} onclick={onvalidate}>
          {validateLabel}
        </button>
      {/if}
    </div>
  </div>
</div>

<style>
  .action-bar {
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-2) var(--space-5);
    margin-top: var(--space-4);
    padding-top: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
    background: var(--paper);
  }

  .bar-end {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
    margin-left: auto;
    padding-bottom: var(--space-2);
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

  .bar-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  @media (width < 768px) {
    /* The buttons go first, so the tabs keep their line on the rule. */
    .bar-end {
      order: -1;
      margin-left: 0;
      width: 100%;
    }

    .bar-actions {
      width: 100%;
    }

    .bar-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
