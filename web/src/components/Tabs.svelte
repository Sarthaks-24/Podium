<!--
  Tabs: a row of tabs that follows the WAI-ARIA tabs pattern (F5): one tab
  in the Tab order, arrows, Home and End move and choose. The panels are
  the caller's; each tab names its panel through `controls`. State is a
  shape (F9): the selected tab is underlined by a border, which forced
  colours keep. A tab can carry a count ("Submissions 3") and a dot with
  ", judging" for screen readers while something it shows is being judged.
  Props: `tabs` ({ id, label, count?, busy? }), `selected`, `onselect`,
  `label` (the tablist's name), `idPrefix` (each tab's id is
  `{idPrefix}-{id}`), `controls` (the panel id for a tab) and `compact`
  (narrower padding for a phone row).
-->
<script lang="ts" generics="T extends string">
  import { tick } from 'svelte';

  interface Tab {
    id: T;
    label: string;
    count?: number;
    busy?: boolean;
  }

  interface Props {
    tabs: readonly Tab[];
    selected: T;
    onselect: (id: T) => void;
    label: string;
    idPrefix: string;
    controls: (id: T) => string;
    compact?: boolean;
  }

  let { tabs, selected, onselect, label, idPrefix, controls, compact = false }: Props = $props();

  function onKey(event: KeyboardEvent) {
    const ids = tabs.map((t) => t.id);
    const i = ids.indexOf(selected);
    let next = -1;
    if (event.key === 'ArrowRight') next = (i + 1) % ids.length;
    else if (event.key === 'ArrowLeft') next = (i - 1 + ids.length) % ids.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = ids.length - 1;
    if (next < 0) return;
    event.preventDefault();
    onselect(ids[next]);
    void tick().then(() => document.getElementById(`${idPrefix}-${ids[next]}`)?.focus());
  }
</script>

<div class="tabs" class:compact role="tablist" aria-label={label}>
  {#each tabs as t (t.id)}
    <button
      type="button"
      role="tab"
      id="{idPrefix}-{t.id}"
      class="tab"
      aria-selected={selected === t.id}
      aria-controls={controls(t.id)}
      tabindex={selected === t.id ? 0 : -1}
      onclick={() => onselect(t.id)}
      onkeydown={onKey}
    >
      {t.label}{#if t.count}<span class="mono count"> {t.count}</span>{/if}{#if t.busy}<span class="visually-hidden">, judging</span><span
          class="dot"
          aria-hidden="true"
        ></span>{/if}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: var(--space-1);
    margin-bottom: calc(-1 * var(--rule));
  }

  .tab {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h-touch);
    padding-inline: var(--space-3);
    border: 0;
    border-bottom: calc(2 * var(--rule)) solid transparent;
    border-radius: var(--radius) var(--radius) 0 0;
    background: none;
    color: var(--ink-muted);
    font-weight: 500;
  }

  .compact .tab {
    padding-inline: var(--space-2);
  }

  .tab[aria-selected='true'] {
    border-bottom-color: var(--act);
    color: var(--ink);
  }

  .count {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: var(--wait);
  }

  /* A transparent border would be forced to CanvasText: unselected tabs paint theirs as the canvas. */
  @media (forced-colors: active) {
    .tab {
      border-bottom-color: Canvas;
    }

    .tab[aria-selected='true'] {
      border-bottom-color: Highlight;
    }
  }
</style>
