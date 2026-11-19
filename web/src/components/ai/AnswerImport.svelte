<!--
  AnswerImport: opens an AI answer saved with Save as file, or pasted text
  (v2.1.1 stage D). The person picks a .json file, which fills the text box,
  or pastes. A saved file knows its own kind; pasted text that is a bare
  model answer needs the person to say whether it is a whole program or one
  function. The server checks the text and writes nothing; the checked answer
  goes to `onopened`. Problems from the server stay on this panel.

  Props: onopened (the checked answer), oncancel.
-->
<script lang="ts">
  import { aiPaths, isAnswerFile, type JavaLists, type OpenedAnswer, type StudentsWrite } from '../../lib/ai';
  import { ApiError, errorText, sendJSON } from '../../lib/api';

  let { onopened, oncancel }: { onopened: (a: OpenedAnswer) => void; oncancel?: () => void } = $props();

  let text = $state('');
  let kind = $state<StudentsWrite | ''>('');
  let javaLists = $state<JavaLists>('array');
  let busy = $state(false);
  let error = $state<{ message: string; problems: readonly string[] } | null>(null);
  let fileInput: HTMLInputElement | undefined;

  const wrapped = $derived(isAnswerFile(text));
  const ready = $derived(text.trim() !== '' && (wrapped || kind !== ''));

  async function pickFile(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    try {
      text = await file.text();
      error = null;
    } catch {
      error = { message: 'Could not read that file. Pick it again, or paste its text.', problems: [] };
    }
  }

  async function open(e: Event) {
    e.preventDefault();
    if (busy) return;
    if (!ready) {
      error = { message: text.trim() === '' ? 'Pick a file or paste the answer.' : 'Say whether the answer is a whole program or one function.', problems: [] };
      return;
    }
    busy = true;
    error = null;
    try {
      const body = wrapped ? { text } : { text, kind, java_lists: kind === 'function' ? javaLists : '' };
      const opened = await sendJSON<OpenedAnswer>('POST', aiPaths.openAnswer, body);
      onopened(opened);
    } catch (err) {
      error = { message: errorText(err), problems: err instanceof ApiError ? err.problems : [] };
    } finally {
      busy = false;
    }
  }
</script>

<form class="import" novalidate onsubmit={open} aria-labelledby="import-heading">
  <h2 id="import-heading" tabindex="-1">Import an AI answer</h2>
  <p class="lead">Open an answer saved with Save as file, or paste the text an AI sent. It opens on the review screen and nothing is created until you press Create draft.</p>

  <div class="field">
    <label for="import-text">Answer file or pasted text</label>
    <textarea id="import-text" class="input code" rows="10" translate="no" spellcheck="false" bind:value={text}></textarea>
    <p class="actions">
      <button type="button" class="btn btn-secondary" onclick={() => fileInput?.click()}>Choose file</button>
      <input bind:this={fileInput} class="file" type="file" accept=".json,application/json" aria-label="Answer file" tabindex="-1" onchange={pickFile} />
    </p>
  </div>

  {#if text.trim() !== '' && !wrapped}
    <fieldset class="kind">
      <legend>This answer is</legend>
      <label class="choice"><input type="radio" name="import-kind" value="program" bind:group={kind} /> A whole program</label>
      <label class="choice"><input type="radio" name="import-kind" value="function" bind:group={kind} /> One function</label>
      {#if kind === 'function'}
        <div class="lists">
          <label class="choice"><input type="radio" name="import-lists" value="array" bind:group={javaLists} /> int[] only</label>
          <label class="choice"><input type="radio" name="import-lists" value="arraylist" bind:group={javaLists} /> ArrayList only</label>
          <label class="choice"><input type="radio" name="import-lists" value="either" bind:group={javaLists} /> Either, the student picks</label>
        </div>
      {/if}
    </fieldset>
  {/if}

  {#if error}
    <div class="banner banner-error" role="alert">
      <p>{error.message}</p>
      {#if error.problems.length > 0}
        <ul>{#each error.problems as p, i (i)}<li>{p}</li>{/each}</ul>
      {/if}
    </div>
  {/if}

  <p class="actions">
    <button type="submit" class="btn btn-primary" disabled={busy}>{busy ? 'Opening the answer' : 'Open answer'}</button>
    {#if oncancel}<button type="button" class="btn btn-quiet" disabled={busy} onclick={oncancel}>Cancel</button>{/if}
  </p>
</form>

<style>
  .import {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-5);
    max-width: var(--measure-prose);
  }

  h2 {
    font-size: var(--fs-2);
  }

  .field {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .code {
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    white-space: pre;
    overflow: auto;
  }

  .file {
    display: none;
  }

  .kind {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
  }

  .lists {
    display: grid;
    gap: var(--space-1);
    padding-left: var(--space-5);
  }

  .choice {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
  }

  .banner ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
