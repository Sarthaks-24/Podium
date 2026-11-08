<!--
  LockedCodeField: whether students write the whole program or only a
  function (v2.0.2), on the editor's Statement tab under the starter code.
  For a function problem, one language at a time: the locked top (common
  imports) and the locked bottom (reads the input, calls the student's
  function, prints the result). The judge compiles top, the student's code
  and bottom as one file; the starter for the language is the function a
  student fills in. A language is offered only with all three. The mode
  cannot change once a student has a submission or a draft; the server
  refuses it then. Folded like the starter code, and opened on a field
  error. Shown only when the server knows about function problems. Props:
  `draft` (bindable) and `fields` (the field errors by key).
-->
<script lang="ts">
  import type { Draft } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
  }

  let { draft = $bindable(), fields }: Props = $props();

  const LANGUAGES = [
    { id: 'cpp17', label: 'C++17' },
    { id: 'java21', label: 'Java 21' },
    { id: 'python3', label: 'Python 3' },
    { id: 'node22', label: 'JavaScript (Node 22)' },
  ] as const;

  let language = $state<string>('cpp17');
  let open = $state(false);
  $effect(() => {
    if (fields.mode || fields.locked_top || fields.locked_bottom) open = true;
  });
  const offered = $derived(LANGUAGES.filter((l) => (draft.locked_bottom?.[l.id] ?? '').trim() !== '').map((l) => l.label));
  const label = $derived(LANGUAGES.find((l) => l.id === language)?.label ?? language);

  function write(part: 'locked_top' | 'locked_bottom', code: string) {
    draft[part] = { ...(draft[part] ?? {}), [language]: code };
  }
</script>

{#if draft.mode !== undefined}
  <details class="fold" bind:open>
    <summary>
      What students write<span class="set"
        >, {draft.mode === 'function' ? `a function${offered.length > 0 ? ` in ${offered.join(', ')}` : ''}` : 'the whole program'}</span
      >
    </summary>
    <fieldset class="mode">
      <legend class="visually-hidden">What students write</legend>
      <div class="choices" role="radiogroup" aria-describedby={fields.mode ? 'mode-error' : undefined}>
        <label class="choice"><input type="radio" name="problem-mode" value="whole" bind:group={draft.mode} /> The whole program</label>
        <label class="choice"><input type="radio" name="problem-mode" value="function" bind:group={draft.mode} /> One function, between locked code</label>
      </div>
      {#if fields.mode}<p id="mode-error" class="field-error">{fields.mode}</p>{/if}
      <p class="muted small">The mode cannot change once a student has a submission or a draft. Make a new problem instead.</p>

      {#if draft.mode === 'function'}
        <p class="muted small" id="locked-help">
          The judge runs the locked top, the student's code and the locked bottom as one file. Students see the locked code but cannot
          change it. The starter for each language is the function they fill in. A language is offered only when it has all three.
        </p>
        <div class="field lang">
          <label for="locked-language">Language</label>
          <select id="locked-language" class="select" bind:value={language}>
            {#each LANGUAGES as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
          </select>
        </div>
        <div class="field">
          <label for="locked-top">Locked top in {label}</label>
          <textarea
            id="locked-top"
            class="input mono code"
            rows="4"
            spellcheck="false"
            translate="no"
            value={draft.locked_top?.[language] ?? ''}
            oninput={(e) => write('locked_top', e.currentTarget.value)}
            aria-describedby="locked-top-help{fields.locked_top ? ' locked-top-error' : ''}"
            aria-invalid={fields.locked_top ? 'true' : undefined}
          ></textarea>
          <p id="locked-top-help" class="field-help">Common imports or includes only, no declarations.</p>
          {#if fields.locked_top}<p id="locked-top-error" class="field-error">{fields.locked_top}</p>{/if}
        </div>
        <div class="field">
          <label for="locked-bottom">Locked bottom in {label}</label>
          <textarea
            id="locked-bottom"
            class="input mono code"
            rows="8"
            spellcheck="false"
            translate="no"
            value={draft.locked_bottom?.[language] ?? ''}
            oninput={(e) => write('locked_bottom', e.currentTarget.value)}
            aria-describedby="locked-bottom-help{fields.locked_bottom ? ' locked-bottom-error' : ''}"
            aria-invalid={fields.locked_bottom ? 'true' : undefined}
          ></textarea>
          <p id="locked-bottom-help" class="field-help">
            Reads the input, calls the student's function and prints the result. In Java this holds public class Main, and the
            student writes class Solution.
          </p>
          {#if fields.locked_bottom}<p id="locked-bottom-error" class="field-error">{fields.locked_bottom}</p>{/if}
        </div>
      {/if}
    </fieldset>
  </details>
{/if}

<style>
  .fold {
    margin-top: var(--space-5);
  }

  .fold > summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    font-weight: 500;
    cursor: pointer;
  }

  .set {
    color: var(--ink-muted);
    font-weight: 400;
  }

  .mode {
    display: grid;
    gap: var(--space-3);
    margin: var(--space-3) 0 0;
    padding: 0;
    border: 0;
  }

  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-5);
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

  .lang {
    max-width: 16rem;
  }

  .code {
    min-height: 6rem;
    resize: vertical;
    tab-size: 4;
  }
</style>
