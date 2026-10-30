<!--
  StartersField: the problem's own starter code (v4 E5), on the editor's
  Statement tab. One language at a time, chosen in a select; the box holds
  what a student's editor opens with in that language, and an empty box
  means Podium's own starter (fast input for the language). Like the
  statement, a change reaches a contest only through "Update students
  now". Shown only when the server knows about starters. Props: `draft`
  (bindable) and `fields` (the field errors by key).
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
  const set = $derived(LANGUAGES.filter((l) => (draft.starters?.[l.id] ?? '').trim() !== '').map((l) => l.label));

  function write(code: string) {
    draft.starters = { ...(draft.starters ?? {}), [language]: code };
  }
</script>

{#if draft.starters !== undefined}
  <fieldset class="starters">
    <legend>Starter code</legend>
    <p class="muted small" id="starters-help">
      What a student's editor opens with. Leave a language empty to give Podium's own starter, which reads input the fast way.
      {#if set.length > 0}Set for: {set.join(', ')}.{/if}
    </p>
    <div class="field lang">
      <label for="starter-language">Language</label>
      <select id="starter-language" class="select" bind:value={language}>
        {#each LANGUAGES as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="starter-code">Starter in {LANGUAGES.find((l) => l.id === language)?.label}</label>
      <textarea
        id="starter-code"
        class="input mono code"
        rows="8"
        spellcheck="false"
        translate="no"
        value={draft.starters[language] ?? ''}
        oninput={(e) => write(e.currentTarget.value)}
        aria-describedby="starters-help{fields.starters ? ' starters-error' : ''}"
        aria-invalid={fields.starters ? 'true' : undefined}
      ></textarea>
      {#if fields.starters}<p id="starters-error" class="field-error">{fields.starters}</p>{/if}
    </div>
  </fieldset>
{/if}

<style>
  .starters {
    display: grid;
    gap: var(--space-3);
    margin: var(--space-5) 0 0;
    padding: 0;
    border: 0;
  }

  .starters legend {
    font-weight: 500;
  }

  .lang {
    max-width: 16rem;
  }

  .code {
    min-height: 10rem;
    resize: vertical;
    tab-size: 4;
  }
</style>
