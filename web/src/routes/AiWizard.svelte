<!--
  AiWizard: "New problem with AI" at /admin/ai/new (cache/v1/m5-plan.md
  sections 2, 4 and 7). A line under the heading names the model and the
  key's state ("Using Claude Opus 5.5. Key active until 14:32.") with a
  link to the AI assistant settings, or says to add a key first. Two modes,
  chosen with radio buttons: "From a brief" (topic, difficulty,
  constraints, subtask ideas) and "From pasted text" (a textarea of at most
  20,000 characters, with the copyright note: the rewrite is in our own
  words, and such packs are not redistributed). Generate posts the request
  to POST /api/admin/ai/wizard and keeps the request id in the address
  (?wizard=), so a reload picks the progress up again. Progress comes from
  the `wizard` live event, with polling of GET /api/admin/ai/wizard/{id}
  while the stream is down: "Writing the problem", then "Draft created,
  generating tests" with a link to the draft in the editor. A failure shows
  the server's sentence and its reasons. One status line is a live region;
  nothing else announces. Takes no props.
-->
<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount, tick } from 'svelte';
  import {
    aiPaths,
    isKeyProblem,
    keyLine,
    MAX_TEXT,
    modelLabel,
    needsKey,
    WIZARD_DIFFICULTIES,
    wizardPending,
    wizardRequest,
    wizardStatusText,
    wizardSteps,
    type Brief,
    type KeyStatus,
    type Wizard,
  } from '../lib/ai';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { navigate, router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { EventStream, type StreamMode } from '../lib/stream';

  /** How often the wizard is asked for while the live stream is down, and while it is up. */
  const POLL_DOWN_MS = 2000;
  const POLL_LIVE_MS = 6000;

  let loadState = $state<'loading' | 'ready' | 'forbidden'>('loading');
  let keyStatus = $state<KeyStatus | null>(null);
  let keyError = $state('');

  let mode = $state<'brief' | 'paste'>('brief');
  let brief = $state<Brief>({ topic: '', difficulty: 'medium', constraints: '', subtasks: '' });
  let text = $state('');
  let fields = $state<Record<string, string>>({});
  let requestError = $state<{ message: string; problems: readonly string[]; code: string } | null>(null);
  let sending = $state(false);

  let wizard = $state<Wizard | null>(null);
  let lostWizard = $state(false);

  const keyActive = $derived(keyStatus === null || keyStatus.active);
  const pending = $derived(wizardPending(wizard));
  const steps = $derived(wizard ? wizardSteps(wizard) : []);
  const textLength = $derived(text.trim().length);

  /** Handles errors every request can hit; returns true when it did. */
  function handleSessionError(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  // Watching one wizard: the live stream, and polling as the fallback.
  let alive = true;
  let stream: EventStream | null = null;
  let streamMode = $state<StreamMode>('connecting');
  let pollTimer: ReturnType<typeof setTimeout> | undefined;

  function stopWatch() {
    stream?.stop();
    stream = null;
    clearTimeout(pollTimer);
  }

  function adoptWizard(w: Wizard) {
    if (!wizard || String(w.id) !== String(wizard.id)) return;
    wizard = w;
    if (!wizardPending(w)) stopWatch();
  }

  function schedulePoll() {
    clearTimeout(pollTimer);
    if (!alive || !wizardPending(wizard)) return;
    pollTimer = setTimeout(pollWizard, streamMode === 'live' ? POLL_LIVE_MS : POLL_DOWN_MS);
  }

  async function pollWizard() {
    const w = wizard;
    if (!w) return;
    try {
      const next = await getJSON<Wizard>(aiPaths.wizardOne(w.id));
      if (alive) adoptWizard(next);
    } catch (e) {
      if (handleSessionError(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        lostWizard = true;
        wizard = null;
        stopWatch();
        return;
      }
      // A failed poll is tried again on the next tick.
    }
    schedulePoll();
  }

  function startWatch() {
    stopWatch();
    streamMode = 'connecting';
    stream = new EventStream({
      url: '/api/events',
      events: ['wizard'],
      onEvent: (_type, data) => {
        if (data && typeof data === 'object') adoptWizard(data as Wizard);
      },
      onReconnect: () => void pollWizard(),
      poll: () => undefined,
      onMode: (m) => {
        streamMode = m;
      },
    });
    stream.start();
    schedulePoll();
  }

  async function loadKey() {
    try {
      keyStatus = await getJSON<KeyStatus>(aiPaths.key);
    } catch (e) {
      if (handleSessionError(e)) return;
      keyError = messageOf(e);
    }
  }

  onMount(() => {
    setTitle(routeTitle('ai-new'));
    const role = session.user?.role;
    if (role !== 'admin' && role !== 'owner') {
      loadState = 'forbidden';
      return;
    }
    loadState = 'ready';
    loadKey();
    const resume = router.query.get('wizard');
    if (resume) {
      wizard = { id: resume, state: 'thinking' };
      void pollWizard().then(() => {
        if (wizardPending(wizard)) startWatch();
      });
    }
    return () => {
      alive = false;
      stopWatch();
    };
  });

  async function generate(event: SubmitEvent) {
    event.preventDefault();
    requestError = null;
    lostWizard = false;
    const built = wizardRequest(mode, brief, text);
    if (!built.ok) {
      fields = built.fields;
      await tick();
      document.querySelector<HTMLElement>('.wizard-form [aria-invalid="true"]')?.focus();
      return;
    }
    fields = {};
    sending = true;
    try {
      const answer = await sendJSON<{ wizard_id: string | number }>('POST', aiPaths.wizard, built.body);
      wizard = { id: String(answer.wizard_id), state: 'thinking' };
      navigate(`/admin/ai/new?wizard=${encodeURIComponent(String(answer.wizard_id))}`, { replace: true });
      startWatch();
      // The form goes away, so focus moves to what replaced it.
      await tick();
      document.getElementById('progress-heading')?.focus();
    } catch (e) {
      if (handleSessionError(e)) return;
      const raw = e instanceof ApiError ? e.fields : {};
      const next: Record<string, string> = {};
      for (const [k, v] of Object.entries(raw)) next[k.replace(/^brief\./, '')] = v;
      fields = next;
      requestError = {
        message: messageOf(e),
        problems: e instanceof ApiError ? e.problems : [],
        code: e instanceof ApiError ? e.code : 'unexpected',
      };
      if (e instanceof ApiError && e.code === 'no_key' && keyStatus) keyStatus = { ...keyStatus, active: false };
    } finally {
      sending = false;
    }
  }

  async function startAgain() {
    stopWatch();
    wizard = null;
    requestError = null;
    navigate('/admin/ai/new', { replace: true });
    await tick();
    document.getElementById(mode === 'brief' ? 'topic' : 'paste-text')?.focus();
  }

  function described(key: string, help = ''): string | undefined {
    const ids = [help, fields[key] ? `${key}-error` : ''].filter((s) => s !== '');
    return ids.length > 0 ? ids.join(' ') : undefined;
  }
</script>

<div class="wizard">
  <PageHeader title="New problem with AI" crumbs={[{ label: 'Problem bank', href: '/admin/problems' }, { label: 'New problem with AI' }]} />

  {#if loadState === 'forbidden'}
    <p class="lead">Only admins can use the AI assistant. Ask an owner if you need access.</p>
  {:else if loadState === 'ready'}
    <p class="key-state">
      {#if keyStatus && keyStatus.active}
        Using {modelLabel(keyStatus, keyStatus.provider, keyStatus.model)}.
        {keyLine(keyStatus)}{needsKey(keyStatus.provider) ? '.' : ''}
        <a href="/admin/ai">Change key</a>
      {:else if keyStatus}
        No key yet. <a href="/admin/ai">Add a key in AI assistant</a> before you generate.
      {:else if keyError !== ''}
        {keyError} <a href="/admin/ai">Go to AI assistant</a>
      {:else}
        <span class="muted">Checking the key</span>
      {/if}
    </p>

    <p class="lead">
      The model writes a draft: statement, subtasks, reference and brute force solutions, a validator and a test generator. Nothing reaches
      students until you have read it and it passes validation.
    </p>

    {#if lostWizard}
      <div class="banner banner-info lead" role="status">
        <p>That request is no longer on the server. It keeps a request only while it runs. Generate again.</p>
      </div>
    {/if}

    {#if wizard}
      <section class="progress" aria-labelledby="progress-heading">
        <h2 id="progress-heading" tabindex="-1">Progress</h2>
        <ol class="steps">
          {#each steps as step (step.text)}
            <li class="step step-{step.state}">
              <span class="mark" aria-hidden="true"></span>
              <span>{step.text}</span>
              <span class="visually-hidden">
                {step.state === 'done' ? ', done' : step.state === 'current' ? ', in progress' : step.state === 'failed' ? ', failed' : ', not started'}
              </span>
            </li>
          {/each}
        </ol>
        <p class="visually-hidden" role="status">{wizardStatusText(wizard)}</p>

        {#if pending}
          <p class="muted hint">This takes a minute or two. You can leave this page; the draft appears in the problem bank when it is ready.</p>
          {#if streamMode === 'polling'}
            <p class="muted hint">Live updates are down, so progress is checked every few seconds.</p>
          {/if}
        {:else if wizard.state === 'error'}
          <div class="banner banner-error problems-list" role="alert">
            <p>
              {wizard.error?.message ?? 'The model could not write this problem.'}
              {#if isKeyProblem(wizard.error?.code)}<a href="/admin/ai">Go to AI assistant</a>{/if}
            </p>
            {#if wizard.error?.reasons && wizard.error.reasons.length > 0}
              <ul>{#each wizard.error.reasons as reason, i (i)}<li>{reason}</li>{/each}</ul>
            {/if}
          </div>
          <p class="actions"><button type="button" class="btn btn-secondary" onclick={startAgain}>Change the request</button></p>
        {:else if wizard.problem_id}
          <p class="created">
            The draft is in the problem bank. Tests are being generated and validated; the editor shows each step.
          </p>
          <p class="actions">
            <a class="btn btn-primary" href="/admin/problems/{wizard.problem_id}">Open the draft in the editor</a>
            <button type="button" class="btn btn-quiet" onclick={startAgain}>Start another</button>
          </p>
        {/if}
      </section>
    {/if}

    {#if !wizard || wizard.state === 'error'}
      <form class="wizard-form" novalidate onsubmit={generate} aria-labelledby="request-heading">
        <h2 id="request-heading" class="visually-hidden">Request</h2>
        <fieldset class="modes">
          <legend>Start from</legend>
          <label class="mode" class:selected={mode === 'brief'}>
            <input type="radio" name="mode" value="brief" bind:group={mode} />
            <span class="mode-text">
              <span class="mode-name">From a brief</span>
              <span class="mode-help">Describe the topic and the model writes a new problem.</span>
            </span>
          </label>
          <label class="mode" class:selected={mode === 'paste'}>
            <input type="radio" name="mode" value="paste" bind:group={mode} />
            <span class="mode-text">
              <span class="mode-name">From pasted text</span>
              <span class="mode-help">Paste an existing problem and the model rewrites it.</span>
            </span>
          </label>
        </fieldset>

        {#if mode === 'brief'}
          <div class="field">
            <label for="topic">Topic</label>
            <input
              id="topic"
              class="input"
              type="text"
              autocomplete="off"
              bind:value={brief.topic}
              aria-invalid={fields.topic ? 'true' : undefined}
              aria-describedby={described('topic', 'topic-help')}
            />
            <p id="topic-help" class="field-help">What the problem is about, for example "count paths in a grid with blocked cells".</p>
            {#if fields.topic}<p id="topic-error" class="field-error">{fields.topic}</p>{/if}
          </div>
          <div class="field difficulty">
            <label for="difficulty">Difficulty</label>
            <select id="difficulty" class="select" bind:value={brief.difficulty}>
              {#each WIZARD_DIFFICULTIES as d (d.id)}<option value={d.id}>{d.label}</option>{/each}
            </select>
          </div>
          <div class="field">
            <label for="constraints">Constraints <span class="optional">(optional)</span></label>
            <textarea
              id="constraints"
              class="input"
              rows="3"
              bind:value={brief.constraints}
              aria-invalid={fields.constraints ? 'true' : undefined}
              aria-describedby={described('constraints', 'constraints-help')}
            ></textarea>
            <p id="constraints-help" class="field-help">Sizes and limits you want, for example n up to 200000 and values up to 10^9.</p>
            {#if fields.constraints}<p id="constraints-error" class="field-error">{fields.constraints}</p>{/if}
          </div>
          <div class="field">
            <label for="subtasks">Subtask ideas <span class="optional">(optional)</span></label>
            <textarea
              id="subtasks"
              class="input"
              rows="3"
              bind:value={brief.subtasks}
              aria-invalid={fields.subtasks ? 'true' : undefined}
              aria-describedby={described('subtasks', 'subtasks-help')}
            ></textarea>
            <p id="subtasks-help" class="field-help">For example: 30 points for n up to 1000, 70 points for the full range.</p>
            {#if fields.subtasks}<p id="subtasks-error" class="field-error">{fields.subtasks}</p>{/if}
          </div>
        {:else}
          <p id="paste-note" class="note">
            Problems on LeetCode and similar sites are copyrighted. The model rewrites the problem in new words with new samples and tests;
            read the result for anything copied, and do not redistribute packs made this way.
          </p>
          <div class="field">
            <label for="paste-text">Problem text</label>
            <textarea
              id="paste-text"
              class="input paste"
              rows="14"
              bind:value={text}
              aria-invalid={fields.text ? 'true' : undefined}
              aria-describedby="paste-note paste-count{fields.text ? ' text-error' : ''}"
            ></textarea>
            <p id="paste-count" class="field-help" class:over={textLength > MAX_TEXT}>
              <span class="mono">{textLength}</span> of {MAX_TEXT.toLocaleString('en-GB')} characters
            </p>
            {#if fields.text}<p id="text-error" class="field-error">{fields.text}</p>{/if}
          </div>
        {/if}

        <div class="actions">
          <button type="submit" class="btn btn-primary" disabled={sending || !keyActive} aria-describedby={keyActive ? undefined : 'generate-help'}>
            {sending ? 'Sending' : 'Generate'}
          </button>
          {#if !keyActive}<span id="generate-help" class="field-help">Add a key in AI assistant first.</span>{/if}
        </div>

        {#if requestError}
          <div class="banner banner-error problems-list" role="alert">
            <p>
              {requestError.message}
              {#if isKeyProblem(requestError.code)}<a href="/admin/ai">Go to AI assistant</a>{/if}
            </p>
            {#if requestError.problems.length > 0}
              <ul>{#each requestError.problems as line, i (i)}<li>{line}</li>{/each}</ul>
            {/if}
          </div>
        {/if}
      </form>
    {/if}
  {/if}
</div>

<style>
  .wizard {
    max-width: 48rem;
  }

  .key-state {
    margin-top: var(--space-3);
    color: var(--ink-muted);
  }

  .key-state a {
    font-weight: 500;
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 72ch;
  }

  h2 {
    font-size: var(--fs-2);
  }

  .progress {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .steps {
    display: grid;
    gap: var(--space-2);
    padding: 0;
    list-style: none;
  }

  .step {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--ink-muted);
  }

  .mark {
    flex: none;
    width: var(--space-3);
    height: var(--space-3);
    border: 2px solid var(--line-strong);
    border-radius: 50%;
  }

  .step-current {
    color: var(--ink);
    font-weight: 500;
  }

  .step-current .mark {
    border-color: var(--act);
    background: var(--act-tint);
  }

  .step-done .mark {
    border-color: var(--ok);
    background: var(--ok);
  }

  .step-failed {
    color: var(--bad);
    font-weight: 500;
  }

  .step-failed .mark {
    border-color: var(--bad);
    background: var(--bad);
  }

  .hint {
    font-size: var(--fs-0);
  }

  .created {
    font-weight: 500;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  .problems-list ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .wizard-form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .modes {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    min-width: 0;
    padding: 0;
    border: 0;
  }

  .modes legend {
    margin-bottom: var(--space-2);
    padding: 0;
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .mode {
    display: flex;
    flex: 1 1 16rem;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    cursor: pointer;
    transition: border-color var(--dur-fast) var(--ease);
  }

  .mode:hover {
    border-color: var(--ink-muted);
  }

  .mode.selected {
    border-color: var(--act);
    background: var(--act-tint);
  }

  .mode input {
    flex: none;
    width: var(--space-4);
    height: var(--space-4);
    margin: var(--space-1) 0 0;
    accent-color: var(--act);
  }

  .mode-text {
    display: grid;
    gap: var(--space-1);
  }

  .mode-name {
    font-weight: 500;
  }

  .mode-help {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .difficulty {
    max-width: 16rem;
  }

  .optional {
    color: var(--ink-muted);
    font-weight: 400;
  }

  .note {
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--part);
    background: var(--part-tint);
    color: var(--ink);
  }

  .paste {
    min-height: calc(14 * var(--lh-body) * var(--fs-1));
  }

  .over {
    color: var(--bad);
  }

  @media (max-width: 767px) {
    .actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
