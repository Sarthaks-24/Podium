<!--
  CustomThemeBuilder: the controls for the custom interface theme, shown by
  ThemePicker under the "Custom" row. A background colour and an accent
  colour (each a colour well plus a hex field, so the keyboard and a pasted
  value work), a contrast choice (standard or high), a live preview of a page
  fragment painted from the derived tokens, and, when the engine moved a
  colour, the list "Adjusted so text stays readable". The engine
  (lib/theme-engine.ts) derives every colour, so any input is safe; the
  preview shows exactly what the page will become. The colour wells report
  each drag with `commit` false and the end of a drag with `commit` true, so
  the page follows the drag and the account is saved once. Props: `value`
  (the three inputs) and `onchange(next, commit)`.
-->
<script lang="ts">
  import { derive } from '../lib/theme';
  import { isHex, type ThemeInput } from '../lib/theme-engine';

  let { value, onchange }: { value: ThemeInput; onchange: (next: ThemeInput, commit: boolean) => void } = $props();

  const uid = $props.id();
  const result = $derived(derive(value));
  const styleText = $derived(Object.entries(result.vars).map(([k, v]) => `${k}: ${v}`).join('; '));

  // The hex fields hold what was typed until it is a colour.
  let typed = $state<{ background: string | null; accent: string | null }>({ background: null, accent: null });
  const bad = $derived({
    background: typed.background !== null && !isHex(typed.background),
    accent: typed.accent !== null && !isHex(typed.accent),
  });

  function typeHex(which: 'background' | 'accent', text: string) {
    typed[which] = text;
    if (isHex(text)) {
      onchange({ ...value, [which]: text.toLowerCase() }, true);
      typed[which] = null;
    }
  }

  function leaveHex(which: 'background' | 'accent') {
    if (typed[which] !== null && isHex(typed[which])) typed[which] = null;
  }
</script>

<div class="builder">
  {#each [['background', 'Background'], ['accent', 'Accent']] as const as [key, label] (key)}
    <div class="field">
      <label for="{uid}-{key}-hex">{label}</label>
      <div class="pair">
        <input
          type="color"
          class="well"
          aria-label="{label} colour picker"
          value={value[key]}
          oninput={(e) => onchange({ ...value, [key]: e.currentTarget.value }, false)}
          onchange={(e) => onchange({ ...value, [key]: e.currentTarget.value }, true)}
        />
        <input
          id="{uid}-{key}-hex"
          type="text"
          class="input hex"
          inputmode="text"
          autocomplete="off"
          spellcheck="false"
          maxlength="7"
          aria-invalid={bad[key]}
          aria-describedby={bad[key] ? `${uid}-${key}-err` : undefined}
          value={typed[key] ?? value[key]}
          oninput={(e) => typeHex(key, e.currentTarget.value)}
          onblur={() => leaveHex(key)}
        />
      </div>
      {#if bad[key]}
        <p class="field-error" id="{uid}-{key}-err">Use six hex digits after the #, for example 1A2B3C.</p>
      {/if}
    </div>
  {/each}

  <fieldset class="contrast">
    <legend>Contrast</legend>
    <label class="choice">
      <input type="radio" name="{uid}-contrast" checked={value.contrast === 'standard'} onchange={() => onchange({ ...value, contrast: 'standard' }, true)} />
      Standard
    </label>
    <label class="choice">
      <input type="radio" name="{uid}-contrast" checked={value.contrast === 'high'} onchange={() => onchange({ ...value, contrast: 'high' }, true)} />
      High
    </label>
  </fieldset>

  <div class="preview" data-family={result.family} data-contrast={result.contrast === 'high' ? 'high' : undefined} style={styleText} aria-hidden="true">
    <p class="title">Round 3 results</p>
    <p class="muted">12 of 30 solved</p>
    <p class="actions"><span class="button">Submit</span><span class="link">View standings</span></p>
    <p class="verdicts"><span class="ok">Accepted</span><span class="part">Partial</span><span class="bad">Wrong</span></p>
  </div>

  <div aria-live="polite">
    {#if result.adjustments.length > 0}
      <p class="adjusted">Adjusted so text stays readable</p>
      <ul class="notes">
        {#each result.adjustments as note (note)}<li>{note}</li>{/each}
      </ul>
    {/if}
  </div>
</div>

<style>
  .builder {
    display: grid;
    gap: var(--space-3);
    margin: var(--space-1) var(--space-2) var(--space-2) calc(1rem + var(--space-3) + var(--space-2));
  }

  .field {
    display: grid;
    gap: var(--space-1);
  }

  label,
  legend {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .pair {
    display: flex;
    gap: var(--space-2);
  }

  .well {
    flex: none;
    width: var(--space-7);
    height: var(--control-h);
    padding: 0;
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    cursor: pointer;
  }

  .hex {
    width: 8rem;
    font-family: var(--font-mono);
  }

  .field-error {
    margin: 0;
    color: var(--bad);
    font-size: var(--fs-0);
  }

  .contrast {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-4);
    margin: 0;
    padding: 0;
    border: 0;
  }

  .contrast legend {
    width: 100%;
    padding: 0;
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    color: var(--ink);
    font-weight: 400;
    cursor: pointer;
  }

  .choice input {
    width: 1rem;
    height: 1rem;
    margin: 0;
    accent-color: var(--act);
  }

  .preview {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-3);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--paper);
    color: var(--ink);
  }

  .preview p {
    margin: 0;
  }

  .title {
    font-weight: 600;
  }

  .muted {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .actions,
  .verdicts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
    margin-top: var(--space-1);
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .button {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius);
    background: var(--act);
    color: var(--act-ink);
  }

  .link {
    color: var(--act);
    text-decoration: underline;
  }

  .verdicts span {
    padding: 0 var(--space-2);
    border-radius: var(--radius);
  }

  .ok {
    background: var(--ok-tint);
    color: var(--ok);
  }

  .part {
    background: var(--part-tint);
    color: var(--part);
  }

  .bad {
    background: var(--bad-tint);
    color: var(--bad);
  }

  .adjusted {
    margin: 0;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 600;
  }

  .notes {
    margin: var(--space-1) 0 0;
    padding-inline-start: var(--space-4);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }
</style>
