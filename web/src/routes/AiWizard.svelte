<!--
  AiWizard: "New problem with AI" at /admin/ai/new. A line under the
  heading names the provider, the model and the key's state, or what is
  missing first; with no key or no model the key form sits inline above
  the brief, so nothing is filled in before the page says it cannot run.
  The organiser writes a brief or pastes text, and Generate keeps the
  request id in the address so a reload picks the progress up again.
  Progress comes from the `wizard` live event, with polling while the
  stream is down, and follows the draft through its tests and validation
  to "Validation passed. Open the draft". One status line is a live
  region; nothing else announces.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import KeyForm from '../components/ai/KeyForm.svelte';
  import { onMount, tick } from 'svelte';
  import { focusFirstInvalid } from '../lib/focus';
  import {
    aiPaths,
    draftChainSteps,
    isKeyProblem,
    keyLine,
    MAX_TEXT,
    modelLabel,
    draftedBy,
    providerLabel,
    wizardBlocker,
    needsKey,
    WIZARD_DIFFICULTIES,
    wizardPending,
    wizardRequest,
    wizardStatusText,
    type Brief,
    type InputGeneration,
    type KeyStatus,
    type Wizard,
  } from '../lib/ai';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { problemPath, type Validation } from '../lib/problems';
  import type { Generation } from '../lib/workspace';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { navigate, router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { LiveSubscription, type StreamMode } from '../lib/stream';

  /** How often the wizard is asked for while the live stream is down, and while it is up. */
  const POLL_DOWN_MS = 2000;
  const POLL_LIVE_MS = 6000;

  // The page loads nothing of its own: it is refused for students, or a request says so.
  let forbidden = $state(session.user?.role !== 'organiser' && session.user?.role !== 'owner');
  let keyStatus = $state<KeyStatus | null>(null);
  let keyError = $state('');
  /** The key form shown on request, when a key and a model are already set. */
  let keyFormOpen = $state(false);

  let mode = $state<'brief' | 'paste'>('brief');
  let brief = $state<Brief>({ topic: '', difficulty: 'medium', constraints: '', subtasks: '' });
  let text = $state('');
  let fields = $state<Record<string, string>>({});
  let requestError = $state<{ message: string; problems: readonly string[]; code: string } | null>(null);
  let sending = $state(false);

  let wizard = $state<Wizard | null>(null);
  let lostWizard = $state(false);

  // Unknown until the status loads; then the wizard needs a key and a model.
  const blocker = $derived(wizardBlocker(keyStatus));
  const keyActive = $derived(blocker === '');
  const pending = $derived(wizardPending(wizard));
  // After the draft exists, its own jobs: inputs, outputs from the reference, validation.
  let chainInputs = $state<InputGeneration | null>(null);
  let chainOutputs = $state<Generation | null>(null);
  let chainValidation = $state<Validation | null>(null);
  const chain = $derived(wizard ? draftChainSteps(wizard, chainInputs, chainOutputs, chainValidation) : null);
  const steps = $derived(chain?.steps ?? []);
  const chainStatus = $derived.by(() => {
    if (!wizard) return '';
    if (chain?.outcome === 'passed') return 'Validation passed';
    if (chain?.outcome === 'failed') return 'Validation failed';
    if (chain?.outcome === 'tests_failed') return 'The tests could not be generated';
    if (wizard.problem_id) return chainValidation ? 'Validating the draft' : 'Generating the tests';
    return wizardStatusText(wizard);
  });
  const drafted = $derived(wizard ? draftedBy(wizard, keyStatus) : '');
  const textLength = $derived(text.trim().length);

  /** Handles errors every request can hit; returns true when it did. */
  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) return true;
    if (e.status === 403 && e.code === 'forbidden') {
      forbidden = true;
      return true;
    }
    return false;
  }

  // Watching one wizard: the live stream, and polling as the fallback.
  let alive = true;
  let stream: LiveSubscription | null = null;
  let streamMode = $state<StreamMode>('connecting');
  let pollTimer: ReturnType<typeof setTimeout> | undefined;

  let chainTimer: ReturnType<typeof setTimeout> | undefined;
  let chainStarted = 0;
  /** The page stops following the draft after this long; the editor shows the rest. */
  const CHAIN_MAX_MS = 10 * 60_000;

  function stopWatch() {
    stream?.stop();
    stream = null;
    clearTimeout(pollTimer);
  }

  function adoptWizard(w: Wizard) {
    if (!wizard || String(w.id) !== String(wizard.id)) return;
    wizard = w;
    if (!wizardPending(w)) {
      stopWatch();
      if (w.problem_id && chainStarted === 0) {
        chainStarted = Date.now();
        void pollChain();
      }
    }
  }

  /** One job of the draft, or null when it never had one (404). */
  async function job<T>(path: string): Promise<T | null> {
    try {
      return await getJSON<T>(path);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return null;
      throw e;
    }
  }

  async function pollChain() {
    const pid = wizard?.problem_id;
    if (!pid || !alive) return;
    try {
      const [i, o, v] = await Promise.all([
        job<InputGeneration>(problemPath(pid, '/input-generation')),
        job<Generation>(problemPath(pid, '/generation')),
        job<Validation>(problemPath(pid, '/validation')),
      ]);
      if (!alive) return;
      chainInputs = i;
      chainOutputs = o;
      chainValidation = v;
    } catch (e) {
      if (handleAuth(e)) return;
      // A failed poll is tried again on the next tick.
    }
    clearTimeout(chainTimer);
    if (alive && !chain?.done && Date.now() - chainStarted < CHAIN_MAX_MS) chainTimer = setTimeout(pollChain, POLL_DOWN_MS);
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
      if (handleAuth(e)) return;
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
    stream = new LiveSubscription({
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

  /** Shows the key form inline and puts focus on its first field. */
  async function openKeyForm() {
    keyFormOpen = true;
    await tick();
    document.getElementById('wiz-provider')?.focus();
  }

  /** After a save that leaves nothing missing, the form folds and the brief takes focus. */
  async function keySaved(s: KeyStatus) {
    if (wizardBlocker(s) !== '') return;
    keyFormOpen = false;
    await tick();
    (document.getElementById(mode === 'brief' ? 'topic' : 'paste-text') ?? document.getElementById('key-change'))?.focus();
  }

  async function loadKey() {
    keyError = '';
    try {
      keyStatus = await getJSON<KeyStatus>(aiPaths.key);
    } catch (e) {
      if (handleAuth(e)) return;
      keyError = errorText(e);
    }
  }

  onMount(() => {
    setTitle(routeTitle('ai-new'));
    if (forbidden) return;
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
      clearTimeout(chainTimer);
    };
  });

  async function generate(event: SubmitEvent) {
    event.preventDefault();
    requestError = null;
    lostWizard = false;
    const built = wizardRequest(mode, brief, text);
    if (!built.ok) {
      fields = built.fields;
      await focusFirstInvalid('.wizard-form');
      return;
    }
    fields = {};
    sending = true;
    try {
      const answer = await sendJSON<{ wizard_id: string | number }>('POST', aiPaths.wizard, built.body);
      wizard = { id: String(answer.wizard_id), state: 'thinking' };
      if (!alive) return;
      navigate(`/admin/ai/new?wizard=${encodeURIComponent(String(answer.wizard_id))}`, { replace: true });
      startWatch();
      // The form goes away, so focus moves to what replaced it.
      await tick();
      document.getElementById('progress-heading')?.focus();
    } catch (e) {
      if (handleAuth(e)) return;
      const raw = e instanceof ApiError ? e.fields : {};
      const next: Record<string, string> = {};
      for (const [k, v] of Object.entries(raw)) next[k.replace(/^brief\./, '')] = v;
      fields = next;
      requestError = {
        message: errorText(e),
        problems: e instanceof ApiError ? e.problems : [],
        code: e instanceof ApiError ? e.code : 'unexpected',
      };
      if (Object.keys(next).length > 0) await focusFirstInvalid('.wizard-form');
      if (e instanceof ApiError && e.code === 'no_key' && keyStatus) keyStatus = { ...keyStatus, active: false };
      if (e instanceof ApiError && e.code === 'no_model' && keyStatus) keyStatus = { ...keyStatus, model: '' };
    } finally {
      sending = false;
    }
  }

  async function startAgain() {
    stopWatch();
    clearTimeout(chainTimer);
    chainStarted = 0;
    chainInputs = null;
    chainOutputs = null;
    chainValidation = null;
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
  <PageHeader title="New problem with AI" crumbs={[{ label: 'Problems', href: '/admin/problems' }, { label: 'New problem with AI' }]} />

  {#if forbidden}
    <PageState kind="forbidden" message="Only organisers can use the AI assistant." />
  {:else}
    <p class="key-state">
      {#if keyStatus && keyStatus.active && blocker !== ''}
        {blocker}
      {:else if keyStatus && keyStatus.active}
        Using {providerLabel(keyStatus, keyStatus.provider)}{needsKey(keyStatus.provider) ? `, ${modelLabel(keyStatus, keyStatus.provider, keyStatus.model)}` : ''}.
        {`${keyLine(keyStatus)}.`}
        <button id="key-change" type="button" class="btn btn-link btn-compact" aria-expanded={keyFormOpen} onclick={() => (keyFormOpen ? (keyFormOpen = false) : void openKeyForm())}>Change key or model</button>
      {:else if keyStatus}
        No key yet. Add one below to generate; it stays in the server's memory only.
      {:else if keyError !== ''}
        {keyError} <button type="button" class="btn btn-link btn-compact" onclick={loadKey}>Check again</button>
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

    {#if keyStatus && ((blocker !== '' && !wizard) || keyFormOpen)}
      <section class="panel inline-key" aria-labelledby="wiz-form-heading">
        <KeyForm
          bind:status={keyStatus}
          id="wiz"
          heading={keyFormOpen && blocker === '' ? undefined : keyStatus.active ? 'Choose a model to start' : 'Add a key to start'}
          onsessionerror={handleAuth}
          onsaved={(s) => void keySaved(s)}
        />
      </section>
    {/if}

    {#if wizard}
      <section class="progress" aria-labelledby="progress-heading">
        <h2 id="progress-heading" tabindex="-1">Progress</h2>
        <ol class="steps">
          {#each steps as step (step.text)}
            <li class="step step-{step.state}">
              {#if step.state === 'done'}
                <span class="mark-icon done" aria-hidden="true"><Icon name="circle-check" /></span>
              {:else if step.state === 'failed'}
                <span class="mark-icon failed" aria-hidden="true"><Icon name="circle-x" /></span>
              {:else}
                <span class="mark" aria-hidden="true"></span>
              {/if}
              <span>{step.text}</span>
              <span class="visually-hidden">
                {step.state === 'done' ? ', done' : step.state === 'current' ? ', in progress' : step.state === 'failed' ? ', failed' : ', not started'}
              </span>
            </li>
          {/each}
        </ol>
        <p class="visually-hidden" role="status">{chainStatus}</p>

        {#if pending}
          <p class="muted hint">This takes a minute or two. You can leave this page; the draft appears in the problem bank when it is ready.</p>
          {#if streamMode === 'polling'}
            <p class="muted hint">Live updates are down, so progress is checked every few seconds.</p>
          {/if}
        {:else if wizard.state === 'error'}
          <div class="banner banner-error problems-list" role="alert">
            <p>
              {wizard.error?.message ?? 'The model could not write this problem.'}
              {#if isKeyProblem(wizard.error?.code)}<button type="button" class="btn btn-link btn-compact" onclick={openKeyForm}>Change key or model</button>{/if}
            </p>
            {#if wizard.error?.reasons && wizard.error.reasons.length > 0}
              <ul>{#each wizard.error.reasons as reason, i (i)}<li>{reason}</li>{/each}</ul>
            {/if}
          </div>
          <p class="actions"><button type="button" class="btn btn-secondary" onclick={startAgain}>Change the request</button></p>
        {:else if wizard.problem_id}
          <p class="created">
            {[drafted ? `${drafted}.` : '', chain?.outcome === 'passed'
              ? 'Validation passed. Read the draft, then add it to a contest.'
              : chain?.outcome === 'failed'
                ? 'Validation failed. Open the draft to see what to fix.'
                : chain?.outcome === 'tests_failed'
                  ? 'The tests could not be generated. Open the draft to see why.'
                  : 'The draft is in the problem bank. Its tests are being generated and validated; this page follows each step.']
              .filter((x) => x !== '')
              .join(' ')}
          </p>
          <p class="actions">
            <a class="btn btn-primary" href="/admin/problems/{wizard.problem_id}">{chain?.outcome === 'passed' ? 'Open the draft' : 'Open the draft in the editor'}</a>
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
              aria-required="true"
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
              aria-required="true"
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
          <button type="submit" class="btn {keyActive ? 'btn-primary' : 'btn-secondary'}" disabled={sending || !keyActive} aria-describedby={keyActive ? undefined : 'generate-help'}>
            {sending ? 'Sending' : 'Generate draft'}
          </button>
          {#if !keyActive}<span id="generate-help" class="field-help">{keyStatus ? (keyStatus.active ? 'Choose a model in the form above first.' : 'Add a key in the form above first.') : blocker}</span>{/if}
        </div>

        {#if requestError}
          <div class="banner banner-error problems-list" role="alert">
            <p>
              {requestError.message}
              {#if isKeyProblem(requestError.code)}<button type="button" class="btn btn-link btn-compact" onclick={openKeyForm}>Change key or model</button>{/if}
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
  .inline-key {
    max-width: var(--measure-form);
    margin-top: var(--space-5);
  }

  .wizard {
    max-width: var(--measure-form);
  }

  .key-state {
    margin-top: var(--space-3);
    color: var(--ink-muted);
  }

  .key-state .btn-link {
    font-weight: 500;
  }

  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-prose);
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
    border: calc(2 * var(--rule)) solid var(--line-strong);
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

  /* Done and failed are shapes (a tick, a cross), not only colours. */
  .mark-icon {
    display: inline-flex;
    flex: none;
  }

  .mark-icon.done {
    color: var(--ok);
  }

  .mark-icon.failed {
    color: var(--bad);
  }

  .step-failed {
    color: var(--bad);
    font-weight: 500;
  }

  @media (forced-colors: active) {
    .mark {
      border-color: CanvasText;
    }

    .step-current .mark {
      background: Highlight;
    }

    .mark-icon {
      color: CanvasText;
    }
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

  @media (width < 768px) {
    .actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
