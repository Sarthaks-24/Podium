<!--
  AnswerReview: the AI's answer on the screen before any problem exists
  (v2.1.1 stage C). The answer is one JSON text, bound as `text`, shown two
  ways that stay the same answer: Fields (a text area per string, a number
  input per number, labelled groups for objects and lists) and JSON (the raw
  text). Fields only change values. JSON that does not parse keeps the person
  in the JSON view with the parser's error, and Create draft waits. The page
  shows the answer as the model sent it; `repairs` lists what Podium will
  change when the draft is made, and `reasons` lists what the checks refused,
  so a failed answer can be corrected and created. A cut answer (over 2 MB)
  is shown read-only and cannot be created.

  Admins and the owner also get Save as file, which downloads the answer in
  the podium-ai-answer wrapper (no key or account data) for Import AI answer.

  Props: kind, javaLists (written into the saved file), text (bindable, the answer's JSON), reasons, repairs, cut, busy (a
  Create draft request is running), problems (what the server refused on the
  last Create draft), message (the server's sentence for that refusal),
  oncreate, ondiscard.
-->
<script lang="ts">
  import { fieldId, fieldTree, parseAnswer, sendableAnswer, setAt, showAnswer, type FieldNode } from '../../lib/answerFields';
  import { tick } from 'svelte';
  import { answerFileText, type JavaLists, type StudentsWrite } from '../../lib/ai';
  import { administers } from '../../lib/roles';
  import { session } from '../../lib/session.svelte';

  let {
    text = $bindable(),
    reasons = [],
    repairs = [],
    cut = false,
    busy = false,
    problems = [],
    message = '',
    kind = 'program',
    javaLists = '',
    oncreate,
    ondiscard,
  }: {
    text: string;
    reasons?: readonly string[];
    repairs?: readonly string[];
    cut?: boolean;
    busy?: boolean;
    problems?: readonly string[];
    message?: string;
    kind?: StudentsWrite;
    javaLists?: JavaLists | '';
    oncreate: () => void;
    ondiscard: () => void;
  } = $props();

  const first = parseAnswer(text);
  /** The answer as a value; the fields view reads and edits this. */
  let value = $state<unknown>(first.ok ? first.value : undefined);
  /** The JSON view's text. Starts as the model's answer, printed with indents. */
  let json = $state(first.ok ? showAnswer(first.value) : text);
  let jsonError = $state(first.ok ? '' : first.error);
  /** A text that does not parse starts in the JSON view; Fields wait until it does. */
  let view = $state<'fields' | 'json'>(first.ok ? 'fields' : 'json');
  const tree = $derived(fieldTree(value));
  const shownReasons = $derived([...reasons, ...problems]);

  /** A change in Fields: the value changes, and so do the JSON text and the text to send. */
  function edit(node: FieldNode, raw: string | number | boolean) {
    value = setAt(value, node.path, raw);
    json = showAnswer(value);
    text = sendableAnswer(value);
  }

  function onNumber(node: FieldNode, e: Event) {
    const raw = (e.currentTarget as HTMLInputElement).value;
    if (raw !== '' && Number.isFinite(Number(raw))) edit(node, Number(raw));
  }

  /** A change in JSON: a parse error keeps the last good answer and shows the error. */
  function onJson(e: Event) {
    json = (e.currentTarget as HTMLTextAreaElement).value;
    const p = parseAnswer(json);
    if (p.ok) {
      jsonError = '';
      value = p.value;
      text = sendableAnswer(p.value);
    } else {
      jsonError = p.error;
    }
  }

  async function show(next: 'fields' | 'json') {
    if (next === view) return;
    if (next === 'fields' && jsonError !== '') {
      await tick();
      document.getElementById('answer-json')?.focus();
      return;
    }
    view = next;
  }

  async function create() {
    if (jsonError !== '') {
      view = 'json';
      await tick();
      document.getElementById('answer-json')?.focus();
      return;
    }
    oncreate();
  }

  /** Save as file is for admins and the owner. The text in the JSON view is saved even when it does not parse. */
  const canSave = $derived(administers(session.user?.role) && !cut);

  function saveFile() {
    const body = answerFileText(jsonError !== '' ? json : text, kind, javaLists);
    const url = URL.createObjectURL(new Blob([body], { type: 'application/json' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'podium-ai-answer.json';
    document.body.append(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  const canCreate = $derived(!cut && !busy && jsonError === '');
</script>

{#snippet field(node: FieldNode)}
  {#if node.kind === 'group'}
    <fieldset class="group">
      <legend>{node.label}</legend>
      {#each node.children as child (child.path.join('/'))}{@render field(child)}{/each}
    </fieldset>
  {:else if node.kind === 'text'}
    <div class="field">
      <label for={fieldId(node.path)}>{node.label}</label>
      <textarea
        id={fieldId(node.path)}
        class="input code"
        rows={Math.min(14, Math.max(1, String(node.value).split('\n').length))}
        translate="no"
        value={String(node.value)}
        oninput={(e) => edit(node, e.currentTarget.value)}
      ></textarea>
    </div>
  {:else if node.kind === 'number'}
    <div class="field number">
      <label for={fieldId(node.path)}>{node.label}</label>
      <input id={fieldId(node.path)} class="input mono" type="number" value={Number(node.value)} oninput={(e) => onNumber(node, e)} />
    </div>
  {:else if node.kind === 'bool'}
    <label class="check">
      <input type="checkbox" checked={node.value === true} onchange={(e) => edit(node, e.currentTarget.checked)} />
      {node.label}
    </label>
  {:else}
    <p class="muted">{node.label}: empty</p>
  {/if}
{/snippet}

<section class="review" aria-labelledby="review-heading">
  <h2 id="review-heading" tabindex="-1">Review the AI's answer</h2>
  <p class="lead">
    No problem exists yet. Read the answer, change anything that is wrong, then create the draft. The answer stays on this page and on the server for an hour.
  </p>

  {#if shownReasons.length > 0}
    <div class="banner banner-error" role="alert">
      <p>{message !== '' ? message : 'The answer has faults. Correct them below, then create the draft.'}</p>
      <ul>{#each shownReasons as reason, i (i)}<li>{reason}</li>{/each}</ul>
    </div>
  {/if}

  {#if repairs.length > 0}
    <div class="banner banner-info">
      <p>This is the answer as the model sent it. Creating the draft will change it as follows.</p>
      <ul>{#each repairs as note, i (i)}<li>{note}</li>{/each}</ul>
    </div>
  {/if}

  {#if cut}
    <div class="banner banner-error" role="alert">
      <p>The answer is longer than 2 MB, so only the start is shown and it cannot become a draft. Discard it and ask again.</p>
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <pre class="raw" tabindex="0" translate="no" aria-label="The start of the AI's answer">{text}</pre>
  {:else}
    <div class="views" role="group" aria-label="View">
      <button type="button" class="btn btn-compact {view === 'fields' ? 'btn-secondary' : 'btn-quiet'}" aria-pressed={view === 'fields'} onclick={() => show('fields')}>Fields</button>
      <button type="button" class="btn btn-compact {view === 'json' ? 'btn-secondary' : 'btn-quiet'}" aria-pressed={view === 'json'} onclick={() => show('json')}>JSON</button>
    </div>

    {#if view === 'fields'}
      <div class="fields">
        {#each tree as n (n.path.join('/'))}{@render field(n)}{/each}
      </div>
    {:else}
      <div class="field">
        <label for="answer-json">The answer as JSON</label>
        <textarea
          id="answer-json"
          class="input code json"
          rows="24"
          translate="no"
          spellcheck="false"
          value={json}
          oninput={onJson}
          aria-invalid={jsonError !== '' ? 'true' : undefined}
          aria-describedby={jsonError !== '' ? 'answer-json-error' : undefined}
        ></textarea>
        {#if jsonError !== ''}
          <p id="answer-json-error" class="field-error" role="alert">This is not valid JSON, so the answer is unchanged: {jsonError}</p>
        {/if}
      </div>
    {/if}
  {/if}

  <p class="actions">
    <button type="button" class="btn btn-primary" disabled={!canCreate} onclick={create}>{busy ? 'Creating the draft' : 'Create draft'}</button>
    {#if canSave}<button type="button" class="btn btn-secondary" onclick={saveFile}>Save as file</button>{/if}
    <button type="button" class="btn btn-quiet" disabled={busy} onclick={ondiscard}>Discard answer</button>
  </p>
</section>

<style>
  .review {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  h2 {
    font-size: var(--fs-2);
  }

  .lead {
    max-width: var(--measure-prose);
  }

  .banner ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .views {
    display: flex;
    gap: var(--space-2);
  }

  .fields {
    display: grid;
    gap: var(--space-4);
  }

  .group {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
  }

  .group legend {
    padding: 0 var(--space-2);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .field {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }

  .field.number {
    max-width: 14rem;
  }

  .code {
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    white-space: pre;
    overflow: auto;
  }

  .json {
    min-height: 24rem;
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
  }

  .raw {
    max-height: 24rem;
    margin: 0;
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
    color: var(--ink);
    font-size: var(--fs-0);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  @media (width < 768px) {
    .actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
