<!--
  EditorSettingsFields: the four editor preferences as form controls (font
  size, tab size, word wrap, Tab moves focus), the one source for both the
  workspace's Settings panel (EditorSettings) and the profile page (v4 M1).
  Props: `fontSize`, `sizes` (the choices, in px), `tabSize`, `wrap`,
  `tabMovesFocus`, `idPrefix` (so two copies never share ids) and
  `onchange`, called with the changed setting.
-->
<script lang="ts">
  interface Settings {
    fontSize: number;
    tabSize: 2 | 4;
    wrap: boolean;
    tabMovesFocus: boolean;
  }

  interface Props extends Settings {
    sizes: readonly number[];
    idPrefix: string;
    onchange: (next: Partial<Settings>) => void;
  }

  let { fontSize, sizes, tabSize, wrap, tabMovesFocus, idPrefix, onchange }: Props = $props();
</script>

<div class="field">
  <label for="{idPrefix}-font">Font size</label>
  <select id="{idPrefix}-font" class="select" value={fontSize} onchange={(e) => onchange({ fontSize: Number(e.currentTarget.value) })}>
    {#each sizes as size (size)}<option value={size}>{size} px</option>{/each}
  </select>
</div>
<div class="field">
  <label for="{idPrefix}-tab-size">Tab size</label>
  <select id="{idPrefix}-tab-size" class="select" value={tabSize} onchange={(e) => onchange({ tabSize: e.currentTarget.value === '2' ? 2 : 4 })}>
    <option value={2}>2 spaces</option>
    <option value={4}>4 spaces</option>
  </select>
</div>
<label class="check">
  <input type="checkbox" checked={wrap} onchange={(e) => onchange({ wrap: e.currentTarget.checked })} />
  Word wrap
</label>
<label class="check">
  <input type="checkbox" checked={tabMovesFocus} onchange={(e) => onchange({ tabMovesFocus: e.currentTarget.checked })} />
  Tab moves focus
</label>
<p class="field-help">With it on, Tab leaves the editor instead of indenting. Esc then Tab always leaves it.</p>

<style>
  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-size: var(--fs-0);
    cursor: pointer;
  }

  .check input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
  }
</style>
