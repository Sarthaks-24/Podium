<!--
  Splitter: the WAI-ARIA window splitter (F5) between the editor and the
  result from 1024 px (F7.5), or with `orientation` "columns" between the
  statement and the code (B6, E5). Drag the grip, or use the keyboard on
  it: arrows move it by 5 percent, Page Up and Page Down by 15, Home and
  End go to the ends, Enter hides or shows the pane; a step down from the
  smallest size hides it too, and so does dragging well past it. For rows
  the arrows are Up and Down and the share is the lower pane's; for
  columns they are Left and Right and the share is the left pane's. The
  button beside it ("Hide result", "Show result") is left out when
  `button` is false; a hidden result keeps its one line (`summary`) in the
  bar. Props: `share` (percent, `min` to `max`), `collapsed`, `summary`,
  `controls` (the pane's id), `column` (the element a drag measures),
  `orientation`, `name` (the pane's name, "Result" or "Statement"),
  `button` and `onchange` (the new share and collapsed, and whether to
  remember them now: a drag remembers on release).
-->
<script lang="ts">
  interface Props {
    share: number;
    collapsed: boolean;
    min: number;
    max: number;
    summary: string;
    controls: string;
    column: HTMLElement | undefined;
    orientation?: 'rows' | 'columns';
    name?: string;
    button?: boolean;
    onchange: (next: { share: number; collapsed: boolean }, remember: boolean) => void;
  }

  let { share, collapsed, min, max, summary, controls, column, orientation = 'rows', name = 'Result', button = true, onchange }: Props = $props();

  const cols = $derived(orientation === 'columns');

  let dragging = false;

  const clamp = (n: number) => Math.round(Math.min(max, Math.max(min, n)));

  function set(next: number) {
    onchange({ share: clamp(next), collapsed: false }, true);
  }

  function toggle() {
    onchange({ share, collapsed: !collapsed }, true);
  }

  function onKey(event: KeyboardEvent) {
    const steps: Record<string, number> = cols
      ? { ArrowRight: 5, ArrowLeft: -5, PageUp: 15, PageDown: -15 }
      : { ArrowUp: 5, ArrowDown: -5, PageUp: 15, PageDown: -15 };
    const step = steps[event.key] ?? 0;
    if (step !== 0) {
      event.preventDefault();
      if (collapsed) {
        if (step > 0) set(min);
      } else if (step < 0 && share <= min) {
        toggle();
      } else {
        set(share + step);
      }
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault();
      set(event.key === 'Home' ? min : max);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      toggle();
    }
  }

  function onDown(event: PointerEvent) {
    if (event.button !== 0 || !column) return;
    dragging = true;
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function onMove(event: PointerEvent) {
    if (!dragging || !column) return;
    const box = column.getBoundingClientRect();
    const next = cols ? ((event.clientX - box.left) / box.width) * 100 : ((box.bottom - event.clientY) / box.height) * 100;
    // Dragged well past the smallest size: hidden, as the step down from the smallest is.
    onchange(next < min / 2 ? { share, collapsed: true } : { share: clamp(next), collapsed: false }, false);
  }

  function onUp() {
    if (!dragging) return;
    dragging = false;
    onchange({ share, collapsed }, true);
  }
</script>

<div class="split" class:collapsed class:cols>
  <!-- A focusable separator is the WAI-ARIA window splitter, an interactive widget; Svelte lists the role as static. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
  <div
    class="grip"
    role="separator"
    tabindex="0"
    aria-orientation={cols ? 'vertical' : 'horizontal'}
    aria-label="{name} panel size"
    aria-controls={controls}
    aria-valuemin={0}
    aria-valuemax={max}
    aria-valuenow={collapsed ? 0 : share}
    aria-valuetext={collapsed ? `${name} hidden` : `${name} ${share} percent of the ${cols ? 'width' : 'column'}`}
    onkeydown={onKey}
    onpointerdown={onDown}
    onpointermove={onMove}
    onpointerup={onUp}
    onpointercancel={onUp}
  >
    <span class="grip-line" aria-hidden="true"></span>
    {#if collapsed && summary}<span class="split-summary">{summary}</span>{/if}
  </div>
  {#if button}
    <button type="button" class="btn btn-quiet btn-compact" aria-expanded={!collapsed} aria-controls={controls} onclick={toggle}>
      {collapsed ? `Show ${name.toLowerCase()}` : `Hide ${name.toLowerCase()}`}
    </button>
  {/if}
</div>

<style>
  .split {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding-inline-end: var(--space-2);
    border-top: var(--rule) solid var(--line);
  }

  /* The grip line sits in the middle; a hidden result's one line sits before it. */
  .grip {
    display: grid;
    flex: 1;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    min-height: var(--control-h);
    padding-inline: var(--space-4);
    cursor: row-resize;
    touch-action: none;
    user-select: none;
  }

  .grip:focus-visible {
    outline: calc(2 * var(--rule)) solid var(--act);
    outline-offset: calc(-2 * var(--rule));
  }

  .grip-line {
    grid-column: 2;
    width: var(--space-7);
    height: calc(2 * var(--rule));
    border-radius: var(--rule);
    background: var(--line-strong);
  }

  .grip:hover .grip-line,
  .grip:focus-visible .grip-line {
    background: var(--ink-muted);
  }

  /* Between the statement and the code: a thin full-height grip. */
  .split.cols {
    padding: 0;
    border-top: 0;
  }

  .cols .grip {
    width: var(--space-3);
    height: 100%;
    min-height: 0;
    padding: 0;
    grid-template-columns: 1fr;
    justify-items: center;
    cursor: col-resize;
  }

  .cols .grip-line {
    grid-column: 1;
    width: calc(2 * var(--rule));
    height: var(--space-7);
  }

  .split-summary {
    grid-column: 1;
    grid-row: 1;
    overflow: hidden;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
