<!--
  AiEditPanel: "Edit with AI" on the problem editor. The organiser types an
  instruction; the server sends the problem's pack files (never test data) and
  the instruction to the model and answers with a draft. The panel shows what
  the model says it changed and, for each file, a line diff of the draft
  against the current text. Nothing is saved until "Accept the edit", which
  makes one new revision, marks the problem as an AI draft and queues
  Validate. "Reject the edit" forgets the draft. The provider is the one the
  session holds a key for, set in the same key form the wizard uses. A
  problem with unsaved changes in the editor asks to save or discard them
  first, because the model works on the saved problem.

  The draft names the settings the AI left out and the server kept. A refused
  edit leads with one sentence and holds two closed panels: "Details" (the
  reasons) and "What the AI sent back" (the model's summary and files, one
  Copy button per file, from that response only).

  Props: `problemId`, `dirty` (the editor has unsaved changes), `origin`
  (the problem's origin; an AI one is marked), `onaccepted` (called after
  Accept, so the editor reloads the problem) and `onclose` (the Close button;
  the editor hides the panel and returns focus to its menu).
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import KeyForm from '../ai/KeyForm.svelte';
  import { aiPaths, keyLine, modelLabel, needsKey, providerLabel, wizardBlocker, type KeyStatus } from '../../lib/ai';
  import Icon from '../Icon.svelte';
  import { answerLabel, changeLine, checkInstruction, editPaths, fileDiff, keptLine, parseSentBack, type EditAccepted, type EditDraft, type SentAnswer } from '../../lib/aiEdit';
  import { ApiError, errorText, getJSON, sendJSON } from '../../lib/api';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    problemId: number;
    dirty: boolean;
    origin: string;
    onaccepted: () => void | Promise<void>;
    onclose: () => void;
  }

  let { problemId, dirty, origin, onaccepted, onclose }: Props = $props();

  /** Fixed by the server's answer; the model can take minutes. */
  const ASK_TIMEOUT_MS = 4 * 60 * 1000;

  let instruction = $state('');
  let keyStatus = $state<KeyStatus | null>(null);
  let keyError = $state('');
  let keyFormOpen = $state(false);
  let fieldError = $state('');
  let asking = $state(false);
  let deciding = $state(false);
  let draft = $state<EditDraft | null>(null);
  let failure = $state<{ message: string; problems: readonly string[]; answers: SentAnswer[] } | null>(null);
  let copied = $state('');
  let copyError = $state(false);
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;

  const blocker = $derived(wizardBlocker(keyStatus));
  const ready = $derived(keyStatus !== null && keyStatus.active && blocker === '');
  const diffs = $derived(draft ? draft.files.map((f) => ({ file: f, lines: fileDiff(f) })) : []);

  async function loadKey() {
    keyError = '';
    try {
      keyStatus = await getJSON<KeyStatus>(aiPaths.key);
    } catch (e) {
      keyError = errorText(e);
    }
  }

  onMount(() => {
    void loadKey();
  });

  async function openKeyForm() {
    keyFormOpen = true;
    await tick();
    document.getElementById('aiedit-provider')?.focus();
  }

  function fail(e: unknown) {
    failure =
      e instanceof ApiError
        ? { message: e.message, problems: e.problems, answers: parseSentBack(e.sentBack) }
        : { message: errorText(e), problems: [], answers: [] };
    copied = '';
    copyError = false;
  }

  async function ask(event: SubmitEvent) {
    event.preventDefault();
    failure = null;
    fieldError = checkInstruction(instruction);
    if (fieldError !== '') {
      document.getElementById('aiedit-instruction')?.focus();
      return;
    }
    if (!keyStatus || !ready) return;
    asking = true;
    draft = null;
    await tick();
    document.getElementById('aiedit-status')?.focus();
    try {
      draft = await sendJSON<EditDraft>('POST', editPaths.ask(problemId), { instruction, provider: keyStatus.provider }, { timeoutMs: ASK_TIMEOUT_MS });
      asking = false;
      await tick();
      document.getElementById('aiedit-result')?.focus();
    } catch (e) {
      fail(e);
      asking = false;
      await tick();
      document.getElementById('aiedit-ask')?.focus();
    }
  }

  async function accept() {
    if (!draft) return;
    deciding = true;
    failure = null;
    try {
      const out = await sendJSON<EditAccepted>('POST', editPaths.accept(problemId, draft.draft_id));
      draft = null;
      instruction = '';
      showToast(out.validation_queued ? 'Edit accepted. Validation has started.' : 'Edit accepted. Validate the problem before you use it.');
      await onaccepted();
    } catch (e) {
      fail(e);
    } finally {
      deciding = false;
    }
  }

  async function copyFile(f: { path: string; content: string }, key: string) {
    clearTimeout(copiedTimer);
    try {
      await navigator.clipboard.writeText(f.content);
      copyError = false;
      copied = key;
      showToast(`Copied ${f.path}`);
      copiedTimer = setTimeout(() => (copied = ''), 2000);
    } catch {
      copied = '';
      copyError = true;
    }
  }

  async function reject() {
    if (!draft) return;
    deciding = true;
    failure = null;
    try {
      await sendJSON<void>('DELETE', editPaths.one(problemId, draft.draft_id));
      draft = null;
      showToast('Draft rejected');
      await tick();
      document.getElementById('aiedit-instruction')?.focus();
    } catch (e) {
      fail(e);
    } finally {
      deciding = false;
    }
  }
</script>

<section class="panel ai-edit" aria-labelledby="aiedit-heading">
  <div class="head">
    <h2 id="aiedit-heading" tabindex="-1">Edit with AI</h2>
    <button type="button" class="btn btn-quiet" onclick={onclose}>Close</button>
  </div>
  <p class="muted">
    Describe a change. The model sees the statement, programs and settings, not the test data, and writes a draft. Nothing is saved until you accept it,
    and an accepted edit has to pass Validate again.
  </p>
  {#if origin === 'ai'}
    <p class="muted origin">The AI wrote or edited this problem.</p>
  {/if}

  <p class="key-state">
    {#if keyStatus && keyStatus.active && blocker !== ''}
      {blocker}
    {:else if keyStatus && keyStatus.active}
      Using {providerLabel(keyStatus, keyStatus.provider)}{needsKey(keyStatus.provider) ? `, ${modelLabel(keyStatus, keyStatus.provider, keyStatus.model)}` : ''}.
      {`${keyLine(keyStatus)}.`}
      <button id="aiedit-key-change" type="button" class="btn btn-link btn-compact" aria-expanded={keyFormOpen} onclick={() => (keyFormOpen ? (keyFormOpen = false) : void openKeyForm())}>Change key or model</button>
    {:else if keyStatus}
      No key yet. Add one below; it stays in the server's memory only.
    {:else if keyError !== ''}
      {keyError} <button type="button" class="btn btn-link btn-compact" onclick={loadKey}>Check again</button>
    {:else}
      <span class="muted">Checking the key</span>
    {/if}
  </p>

  {#if keyStatus && (blocker !== '' || keyFormOpen)}
    <div class="key">
      <KeyForm
        bind:status={keyStatus}
        id="aiedit"
        heading={keyFormOpen && blocker === '' ? undefined : keyStatus.active ? 'Choose a model to start' : 'Add a key to start'}
        onsaved={(s) => {
          if (wizardBlocker(s) === '') keyFormOpen = false;
        }}
      />
    </div>
  {/if}

  <form novalidate onsubmit={ask}>
    <div class="field">
      <label for="aiedit-instruction">What should change</label>
      <textarea
        id="aiedit-instruction"
        class="input"
        rows="4"
        bind:value={instruction}
        aria-describedby={fieldError ? 'aiedit-instruction-error' : undefined}
        aria-invalid={fieldError ? 'true' : undefined}
      ></textarea>
      {#if fieldError}<p id="aiedit-instruction-error" class="field-error" role="alert">{fieldError}</p>{/if}
    </div>
    {#if dirty}
      <p class="muted" id="aiedit-dirty">You have unsaved changes. Save or discard them first: the model works on the saved problem.</p>
    {/if}
    <p class="actions">
      <button id="aiedit-ask" type="submit" class="btn btn-secondary" disabled={asking || deciding || dirty || !ready} aria-describedby={dirty ? 'aiedit-dirty' : undefined}>
        {asking ? 'Asking the AI' : 'Ask for a draft'}
      </button>
    </p>
  </form>

  {#if asking}
    <p id="aiedit-status" class="muted" tabindex="-1">This takes a minute or two, and the AI may try twice. Stay on this page.</p>
  {/if}
  {#if failure}
    <div class="banner banner-error refusal" role="alert">
      <p>{failure.message}</p>
      {#if failure.problems.length > 0}
        <details>
          <summary>Details</summary>
          <ul>{#each failure.problems as line, i (i)}<li>{line}</li>{/each}</ul>
        </details>
      {/if}
      {#if failure.answers.length > 0}
        <details>
          <summary>What the AI sent back</summary>
          <p>Send this to whoever runs Podium if it keeps happening.</p>
          {#each failure.answers as a, ai (ai)}
            {#if answerLabel(ai, failure.answers.length) !== ''}<p class="answer-label">{answerLabel(ai, failure.answers.length)}</p>{/if}
            {#if a.summary !== ''}<p>{a.summary}</p>{/if}
            {#if a.raw !== ''}
              <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
              <div class="compare" role="region" aria-label="The answer, as the AI sent it" tabindex="0">
                <pre translate="no"><code>{a.raw}</code></pre>
              </div>
            {/if}
            {#each a.files as f, fi (fi)}
              <div class="sent-file">
                <span class="path">{f.path}</span>
                <button type="button" class="btn btn-quiet btn-compact" onclick={() => copyFile(f, `${ai}:${fi}`)}>
                  <Icon name="copy" />{copied === `${ai}:${fi}` ? 'Copied' : 'Copy'}<span class="visually-hidden"> {f.path}</span>
                </button>
              </div>
              <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
              <div class="compare" role="region" aria-label="{f.path}, as the AI sent it" tabindex="0">
                <pre translate="no"><code>{f.content}</code></pre>
              </div>
            {/each}
            {#if a.cut}<p class="muted">Cut at 200 KB. The rest was not kept.</p>{/if}
          {/each}
          {#if copyError}<p class="muted">Could not copy. Select the text and copy it by hand.</p>{/if}
        </details>
      {/if}
    </div>
  {/if}

  {#if draft}
    <div class="result">
      <h3 id="aiedit-result" tabindex="-1">Draft</h3>
      {#if draft.summary !== ''}<p>{draft.summary}</p>{/if}
      {#if keptLine(draft.kept) !== ''}<p class="muted">{keptLine(draft.kept)}</p>{/if}
      <p class="muted">{changeLine(draft.files)} Lines marked - are in the current problem only, + only in the draft.</p>
      {#each diffs as d (d.file.path)}
        <details open>
          <summary><span class="path">{d.file.path}</span>{d.file.status === 'added' ? ', new file' : ''}</summary>
          {#if d.lines === null}
            <p class="muted">This file is too long to compare here.</p>
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div class="compare" role="region" aria-label="{d.file.path}, the draft compared with the current text" tabindex="0">
              <pre translate="no"><code>{#each d.lines as l, i (i)}<span class="dl" class:del={l.kind === '-'} class:ins={l.kind === '+'}>{l.kind} {l.text}</span>{/each}</code></pre>
            </div>
          {/if}
        </details>
      {/each}
      <p class="actions">
        <button type="button" class="btn btn-primary" disabled={deciding} onclick={accept}>Accept the edit</button>
        <button type="button" class="btn btn-secondary" disabled={deciding} onclick={reject}>Reject the edit</button>
      </p>
    </div>
  {/if}
</section>

<style>
  .ai-edit,
  form,
  .result {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    justify-items: start;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    width: 100%;
  }

  .refusal {
    width: 100%;
    min-width: 0;
    display: grid;
    gap: var(--space-3);
  }

  .sent-file {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .answer-label {
    font-weight: 600;
  }

  .field,
  form,
  .result,
  details,
  .key {
    width: 100%;
    min-width: 0;
  }

  textarea {
    width: 100%;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .path {
    font-family: var(--font-mono);
  }

  .compare {
    max-height: 50vh;
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
  }

  .compare pre {
    width: max-content;
    min-width: 100%;
    padding-block: var(--space-2);
    font-size: var(--fs-0);
    line-height: var(--lh-body);
  }

  .dl {
    display: block;
    padding-inline: var(--space-2);
    white-space: pre;
  }

  .dl.del {
    background: var(--bad-tint);
  }

  .dl.ins {
    background: var(--ok-tint);
  }
</style>
