<!--
  AiWizard: "New problem with AI" at /admin/ai/new. A line under the
  heading names the provider, the model and the key's state, or what is
  missing first; with no key or no model the key form sits inline above
  the brief, so nothing is filled in before the page says it cannot run.
  The organiser writes a brief or pastes text, and Generate keeps the
  request id in the address so a reload picks the progress up again.
  Progress comes from the `wizard` live event, with polling while the
  stream is down, and follows the draft through its tests and validation
  to "Validation passed. Open the draft". Under "Start from" the organiser
  chooses what students write, a whole program (the default) or one function
  between locked code with the Java list type; a function draft adds the step
  "Checking each language" and, once the round ends, a Languages block that
  says which languages are offered and why one is not. One status line is a
  live region; nothing else announces.

  With ?rewrite=<problem id> (the problem page's "Rewrite with AI") the page
  is the same wizard in paste mode for that problem: a note names it, the
  form is "Students write" (set to the problem's choice) and "What should
  change", and the server sends the AI the problem's statement, settings,
  reference solution and sample tests, never a hidden test. The run reports
  which problem it rewrote, and the page keeps that for the draft's banner.

  When a run fails, or its draft's tests could not be generated, a closed
  "What the AI sent back" disclosure shows the provider's text as plain text,
  read from the run's answer route. It is absent when there is no text.

  "Show me the AI's answer first" is ticked on each visit and not remembered.
  A run with it ends in state "review" before any problem exists: the page
  reads the answer route and shows AnswerReview, where the person edits the
  answer as fields or JSON. Create draft posts the answer, its kind and the
  Java lists choice to /api/admin/ai/drafts, which needs no key and no wizard
  slot, so it works after the run has expired; the returned problem id then
  continues the usual progress steps. A rewrite keeps its banner and link.
  Discard answer clears the page.
-->
<script lang="ts">
  import { organises } from '../lib/roles';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import KeyForm from '../components/ai/KeyForm.svelte';
  import AnswerReview from '../components/ai/AnswerReview.svelte';
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
    wizardInReview,
    wizardPending,
    wizardRequest,
    wizardStatusText,
    type Brief,
    type DraftRequest,
    type InputGeneration,
    type JavaLists,
    type KeyStatus,
    type StudentsWrite,
    type Wizard,
    type WizardAnswer,
  } from '../lib/ai';
  import { checkingLine, notOffered, notOfferedLine, offeredNames, proofPaths, referenceBanner, roundFinished, type ProofState } from '../lib/proof';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { problemPath, type Problem, type Validation } from '../lib/problems';
  import { draftStorage } from '../lib/drafts';
  import { rememberRewrite, rewriteHref, rewriteRequest } from '../lib/rewrite';
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
  let forbidden = $state(!organises(session.user?.role));
  /** Admins and owners are told when the site uses a custom wizard prompt. */
  const seesPromptNote = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  let keyStatus = $state<KeyStatus | null>(null);
  let keyError = $state('');
  /** The key form shown on request, when a key and a model are already set. */
  let keyFormOpen = $state(false);

  let mode = $state<'brief' | 'paste'>('brief');
  let brief = $state<Brief>({ topic: '', difficulty: 'medium', constraints: '', subtasks: '' });
  let text = $state('');
  // Rewrite with AI (stage W): the problem to write again, read from the address, and what should change.
  let rewriteId = $state<number | null>(null);
  let rewriteOld = $state<{ title: string } | null>(null);
  let rewriteError = $state('');
  let change = $state('');
  let studentsWrite = $state<StudentsWrite>('program');
  let javaLists = $state<JavaLists>('array');
  /** "Show me the AI's answer first": ticked for every new request, never remembered. */
  let review = $state(true);
  let fields = $state<Record<string, string>>({});
  let requestError = $state<{ message: string; problems: readonly string[]; code: string } | null>(null);
  let sending = $state(false);

  let wizard = $state<Wizard | null>(null);
  let lostWizard = $state(false);
  const rewriteTitle = $derived(rewriteOld?.title ?? wizard?.rewrite_of?.title ?? '');
  /** The page is for a rewrite: the address named a problem, or the run being followed is one. */
  const rewriting = $derived(rewriteId !== null || !!wizard?.rewrite_of);

  // Unknown until the status loads; then the wizard needs a key and a model.
  const blocker = $derived(wizardBlocker(keyStatus));
  const keyActive = $derived(blocker === '');
  const pending = $derived(wizardPending(wizard));
  // After the draft exists, its own jobs: inputs, outputs from the reference, validation.
  let chainInputs = $state<InputGeneration | null>(null);
  let chainOutputs = $state<Generation | null>(null);
  let chainValidation = $state<Validation | null>(null);
  // A function draft is also checked language by language. null until known: a resumed page learns it from the first answer.
  let functionRun = $state<boolean | null>(null);
  let proof = $state<ProofState | null>(null);
  let proofStopped = false;
  let proofError = $state(false);
  /** The ten minute limit passed with the chain unfinished. */
  let chainSlow = $state(false);
  const chain = $derived(wizard ? draftChainSteps(wizard, chainInputs, chainOutputs, chainValidation, proof, functionRun === true) : null);
  const steps = $derived(chain?.steps ?? []);
  const languageStep = $derived(steps.find((s) => s.text === 'Checking each language')?.state);
  const refBanner = $derived(referenceBanner(proof));
  const showLanguages = $derived(functionRun === true && proof !== null && roundFinished(proof) && refBanner === '');
  const chainStatus = $derived.by(() => {
    if (!wizard) return '';
    if (chain?.outcome === 'passed') return 'Validation passed';
    if (chain?.outcome === 'failed') return 'Validation failed';
    if (chain?.outcome === 'tests_failed') return 'The tests could not be generated';
    if (chain?.outcome === 'reference_failed') return "The AI's own solution failed its tests";
    if (chain?.outcome === 'reference_unchecked') return "The AI's own solution could not be checked";
    if (chain?.outcome === 'proof_stopped') return 'The language checks did not count';
    if (chainSlow) return 'This is taking longer than usual';
    if (languageStep === 'current') return proof ? checkingLine(proof) : 'Checking the languages.';
    if (wizard.problem_id) return chainValidation ? 'Validating the draft' : 'Generating the tests';
    return wizardStatusText(wizard);
  });
  const drafted = $derived(wizard ? draftedBy(wizard, keyStatus) : '');
  /** What the provider sent back for a run that failed or whose tests failed; asked for once per run. */
  let sentBack = $state<WizardAnswer | null>(null);
  let sentBackFor = '';
  $effect(() => {
    const id = wizard?.id;
    if (!id || sentBackFor === String(id) || (wizard?.state !== 'error' && chain?.outcome !== 'tests_failed')) return;
    sentBackFor = String(id);
    getJSON<WizardAnswer>(aiPaths.wizardAnswer(id))
      .then((a) => {
        if (alive && String(wizard?.id) === String(id)) sentBack = a;
      })
      .catch(() => {
        // The answer is extra help: without it the page still says what failed.
      });
  });
  /** The answer of a run waiting for review, read once per run; `reviewText` is what the person edits. */
  let reviewAnswer = $state<WizardAnswer | null>(null);
  let reviewText = $state('');
  let reviewFor = '';
  let reviewLost = $state(false);
  let reviewBusy = $state(false);
  let reviewRefused = $state<{ message: string; problems: readonly string[] } | null>(null);
  $effect(() => {
    const id = wizard?.id;
    if (!id || reviewFor === String(id) || !wizardInReview(wizard)) return;
    reviewFor = String(id);
    getJSON<WizardAnswer>(aiPaths.wizardAnswer(id))
      .then(async (a) => {
        if (!alive || String(wizard?.id) !== String(id)) return;
        reviewAnswer = a;
        reviewText = a.text;
        if (a.rewrite_of && wizard && !wizard.rewrite_of) wizard = { ...wizard, rewrite_of: a.rewrite_of };
        functionRun = a.kind === 'function';
        await tick();
        document.getElementById('review-heading')?.focus();
      })
      .catch((e) => {
        if (!handleAuth(e) && alive) reviewLost = true;
      });
  });
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
      // The draft of a rewrite remembers what it rewrites, for its banner.
      if (w.problem_id && w.rewrite_of) rememberRewrite(draftStorage(), w.problem_id, w.rewrite_of);
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
      if (!alive || wizard?.problem_id !== pid) return;
      chainInputs = i;
      chainOutputs = o;
      chainValidation = v;
    } catch (e) {
      if (handleAuth(e)) return;
      // A failed poll is tried again on the next tick.
    }
    await pollProof(pid);
    if (!alive || wizard?.problem_id !== pid) return;
    clearTimeout(chainTimer);
    if (!alive || proofError || chain?.done) return;
    if (Date.now() - chainStarted < CHAIN_MAX_MS) chainTimer = setTimeout(pollChain, POLL_DOWN_MS);
    else chainSlow = true;
  }

  /**
   * The language checks of a function draft. Asked for on each tick until the
   * round has ended; a whole program never asks, and an error stops the page
   * asking (the draft in the editor holds the same answer).
   */
  async function pollProof(pid: number) {
    if (functionRun === false || proofStopped || !alive) return;
    try {
      const p = await job<ProofState>(proofPaths.state(pid));
      if (!alive || wizard?.problem_id !== pid) return;
      if (p === null) {
        functionRun = false;
        proofStopped = true;
        return;
      }
      proof = p;
      functionRun = p.languages.length > 0;
      if (!functionRun || roundFinished(p)) proofStopped = true;
    } catch (e) {
      if (handleAuth(e) || wizard?.problem_id !== pid) return;
      proofStopped = true;
      proofError = true;
    }
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
    const rewrite = Number(router.query.get('rewrite'));
    if (resume) {
      wizard = { id: resume, state: 'thinking' };
      void pollWizard().then(() => {
        // Left before the first answer: the cleanup below already ran.
        if (alive && wizardPending(wizard)) startWatch();
      });
    } else if (Number.isInteger(rewrite) && rewrite > 0) {
      void loadRewrite(rewrite);
    }
    return () => {
      alive = false;
      stopWatch();
      clearTimeout(chainTimer);
    };
  });

  /** Reads the problem to rewrite: its title for the page and its "Students write" choice for the form. */
  async function loadRewrite(id: number) {
    rewriteId = id;
    rewriteOld = null;
    rewriteError = '';
    try {
      const p = await getJSON<Problem>(problemPath(id));
      if (!alive) return;
      if (p.archived) {
        rewriteError = `${p.title} is archived. Restore it first, then rewrite it.`;
        return;
      }
      rewriteOld = { title: p.title };
      studentsWrite = p.mode === 'function' ? 'function' : 'program';
    } catch (e) {
      if (!handleAuth(e)) rewriteError = errorText(e);
    }
  }

  async function generate(event: SubmitEvent) {
    event.preventDefault();
    requestError = null;
    lostWizard = false;
    const built = rewriteId !== null ? rewriteRequest(change, rewriteId, studentsWrite, javaLists, review) : wizardRequest(mode, brief, text, studentsWrite, javaLists, review);
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
      functionRun = studentsWrite === 'function';
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

  /** Create draft: the answer as it now stands becomes a draft, and the usual steps follow it. */
  async function createDraft() {
    const w = wizard;
    const a = reviewAnswer;
    if (!w || !a || reviewBusy) return;
    reviewBusy = true;
    reviewRefused = null;
    const body: DraftRequest = { answer: reviewText, kind: a.kind ?? 'program', java_lists: a.kind === 'function' && a.java_lists ? a.java_lists : 'array' };
    try {
      const made = await sendJSON<{ problem_id: number }>('POST', aiPaths.drafts, body);
      if (!alive || wizard?.id !== w.id) return;
      const link = w.rewrite_of ?? a.rewrite_of ?? null;
      reviewAnswer = null;
      reviewText = '';
      wizard = { ...w, state: 'draft_created', problem_id: made.problem_id, rewrite_of: link };
      if (link) rememberRewrite(draftStorage(), made.problem_id, link);
      chainStarted = Date.now();
      void pollChain();
      await tick();
      document.getElementById('progress-heading')?.focus();
    } catch (e) {
      if (handleAuth(e)) return;
      reviewRefused = { message: errorText(e), problems: e instanceof ApiError ? e.problems : [] };
    } finally {
      reviewBusy = false;
    }
  }

  async function startAgain() {
    review = true;
    reviewAnswer = null;
    reviewText = '';
    reviewFor = '';
    reviewLost = false;
    reviewRefused = null;
    stopWatch();
    clearTimeout(chainTimer);
    chainStarted = 0;
    chainInputs = null;
    chainOutputs = null;
    chainValidation = null;
    proof = null;
    proofStopped = false;
    proofError = false;
    chainSlow = false;
    functionRun = null;
    sentBack = null;
    sentBackFor = '';
    const again = rewriteId ?? wizard?.rewrite_of?.problem_id ?? null;
    wizard = null;
    requestError = null;
    navigate(again !== null ? rewriteHref(again) : '/admin/ai/new', { replace: true });
    if (again !== null && rewriteOld === null) await loadRewrite(again);
    await tick();
    document.getElementById(again !== null ? 'change-text' : mode === 'brief' ? 'topic' : 'paste-text')?.focus();
  }

  function described(key: string, help = ''): string | undefined {
    const ids = [help, fields[key] ? `${key}-error` : ''].filter((s) => s !== '');
    return ids.length > 0 ? ids.join(' ') : undefined;
  }
</script>

{#snippet sentBackBlock()}
  {#if sentBack && sentBack.text !== ''}
    <details class="sent-back">
      <summary>What the AI sent back</summary>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <pre class="compiler" tabindex="0" translate="no" aria-label="What the AI sent back">{sentBack.text}</pre>
      {#if sentBack.cut}<p class="muted hint">Only the first 200 KB is shown.</p>{/if}
    </details>
  {/if}
{/snippet}

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
    {#if keyStatus?.custom_prompt && seesPromptNote}
      <p class="muted custom-prompt">This site uses a custom prompt.</p>
    {/if}

    {#if rewriteTitle !== ''}
      <div class="banner banner-info lead">
        <p><strong>Rewriting: {rewriteTitle}</strong></p>
        <p>
          The AI writes this problem again from what you say should change. It sees the statement, settings, reference solution and sample tests,
          never the hidden tests. Nothing changes on {rewriteTitle} until you press Replace.
        </p>
      </div>
    {:else if rewriteError !== ''}
      <div class="banner banner-error lead" role="alert">
        <p>{rewriteError}</p>
      </div>
    {:else if rewriteId !== null && !wizard}
      <p class="lead muted" role="status">Loading problem</p>
    {:else}
      <p class="lead">
        The model writes a draft: statement, subtasks, solutions, a checker for the inputs and a test generator. Nothing reaches
        students until you have read it and it passes validation.
      </p>
      <p class="lead">Starting from a problem you already have? Use Edit with AI or Rewrite with AI on its page.</p>
    {/if}
    <p class="lead muted">Edit with AI changes part of this problem and keeps its tests. Rewrite with AI writes it again with new tests. New problem with AI starts from nothing.</p>

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
        {:else if wizardInReview(wizard)}
          {#if reviewLost}
            <div class="banner banner-info" role="status">
              <p>The server no longer holds this answer. It keeps an answer only for an hour and until a restart. Change the request and ask again.</p>
            </div>
            <p class="actions"><button type="button" class="btn btn-secondary" onclick={startAgain}>Change the request</button></p>
          {:else if !reviewAnswer}
            <p class="muted hint" role="status">Reading the answer</p>
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
          {@render sentBackBlock()}
          <p class="actions"><button type="button" class="btn btn-secondary" onclick={startAgain}>Change the request</button></p>
        {:else if wizard.problem_id}
          <p class="created">
            {[drafted ? `${drafted}.` : '', chain?.outcome === 'passed'
              ? 'Validation passed. Read the draft, then add it to a contest.'
              : chain?.outcome === 'failed'
                ? 'Validation failed. Open the draft to see what to fix.'
                : chain?.outcome === 'tests_failed'
                  ? 'The tests could not be generated. Open the draft to see why.'
                  : chain?.outcome === 'reference_failed' || chain?.outcome === 'reference_unchecked' || chain?.outcome === 'proof_stopped'
                    ? 'The draft is in the problem bank. It is not validated.'
                    : 'The draft is in the problem bank. Its tests are being generated and validated; this page follows each step.']
              .filter((x) => x !== '')
              .join(' ')}
          </p>
          {#if chain?.outcome === 'tests_failed'}{@render sentBackBlock()}{/if}
          {#if chainSlow && !chain?.done}
            <div class="banner banner-info">
              <p>This is taking longer than usual. Open the draft to see where it is.</p>
            </div>
          {/if}
          {#if proofError}
            <div class="banner banner-error" role="alert">
              <p>Could not read the language checks. Open the draft to see them.</p>
            </div>
          {:else if refBanner !== ''}
            <div class="banner banner-error" role="alert">
              <p>{refBanner}</p>
            </div>
          {:else if showLanguages && proof}
            <section class="languages" aria-labelledby="languages-heading">
              <h3 id="languages-heading">Languages</h3>
              {#if offeredNames(proof).length > 0}<p>Offered: {offeredNames(proof).join(', ')}.</p>{/if}
              {#each notOffered(proof) as l (l.language)}
                <div class="not-offered">
                  <p class="not-offered-line">
                    <span class="mark-icon failed" aria-hidden="true"><Icon name="circle-x" /></span>
                    <span>{notOfferedLine(l)}</span>
                  </p>
                  {#if l.reason === 'compile_error' && l.compiler.trim() !== ''}
                    <details>
                      <summary>Details</summary>
                      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                      <pre class="compiler" tabindex="0" translate="no" aria-label="Compiler output">{l.compiler}</pre>
                    </details>
                  {/if}
                </div>
              {/each}
            </section>
          {/if}
          <p class="actions">
            <a class="btn btn-primary" href="/admin/problems/{wizard.problem_id}">{chain?.outcome === 'passed' ? 'Open the draft' : 'Open the draft in the editor'}</a>
            <button type="button" class="btn btn-quiet" onclick={startAgain}>Start another</button>
          </p>
        {/if}
      </section>
    {/if}

    {#if wizard && wizardInReview(wizard) && reviewAnswer}
      <AnswerReview
        bind:text={reviewText}
        reasons={reviewAnswer.reasons}
        repairs={reviewAnswer.repairs ?? []}
        cut={reviewAnswer.cut}
        busy={reviewBusy}
        problems={reviewRefused?.problems ?? []}
        message={reviewRefused?.message ?? ''}
        oncreate={createDraft}
        ondiscard={startAgain}
      />
    {/if}

    {#if (!wizard || wizard.state === 'error') && !(rewriting && rewriteOld === null)}
      <form class="wizard-form" novalidate onsubmit={generate} aria-labelledby="request-heading">
        <h2 id="request-heading" class="visually-hidden">Request</h2>
        {#if !rewriting}
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
        {/if}

        <fieldset class="modes">
          <legend>Students write</legend>
          <label class="mode" class:selected={studentsWrite === 'program'}>
            <input type="radio" name="students-write" value="program" bind:group={studentsWrite} />
            <span class="mode-text">
              <span class="mode-name">A whole program</span>
              <span class="mode-help">Each student writes a full program that reads input and prints output.</span>
            </span>
          </label>
          <label class="mode" class:selected={studentsWrite === 'function'}>
            <input type="radio" name="students-write" value="function" bind:group={studentsWrite} />
            <span class="mode-text">
              <span class="mode-name">One function, between locked code</span>
              <span class="mode-help">
                The AI also writes the code students cannot change and the starting code they see, for each language. Podium runs each language against every test before it is
                offered.
              </span>
            </span>
          </label>
        </fieldset>
        {#if studentsWrite === 'function'}
          <fieldset class="lists">
            <legend>In Java, students may use</legend>
            <div class="list-choices">
              <label class="choice"><input type="radio" name="java-lists" value="array" bind:group={javaLists} /> int[] only</label>
              <label class="choice"><input type="radio" name="java-lists" value="arraylist" bind:group={javaLists} /> ArrayList only</label>
              <label class="choice"><input type="radio" name="java-lists" value="either" bind:group={javaLists} /> Either, the student picks</label>
            </div>
            <p class="field-help">C++, Python and JavaScript use their usual list whatever you choose.</p>
          </fieldset>
        {/if}

        {#if rewriting}
          <div class="field">
            <label for="change-text">What should change</label>
            <textarea
              id="change-text"
              aria-required="true"
              class="input paste"
              rows="8"
              bind:value={change}
              aria-invalid={fields.text ? 'true' : undefined}
              aria-describedby="change-help{fields.text ? ' text-error' : ''}"
            ></textarea>
            <p id="change-help" class="field-help">For example: the limits are wrong, the statement is unclear, or the tests miss a case.</p>
            {#if fields.text}<p id="text-error" class="field-error">{fields.text}</p>{/if}
          </div>
        {:else if mode === 'brief'}
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

        <label class="choice review-choice">
          <input type="checkbox" bind:checked={review} />
          Show me the AI's answer first
        </label>

        <div class="actions">
          <button type="submit" class="btn {keyActive ? 'btn-primary' : 'btn-secondary'}" disabled={sending || !keyActive} aria-describedby={keyActive ? undefined : 'generate-help'}>
            {sending ? 'Writing the draft' : rewriting ? 'Rewrite the problem' : 'Generate draft'}
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

  .lists {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }

  .lists legend {
    margin-bottom: var(--space-1);
    padding: 0;
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .list-choices {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-5);
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .choice input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .languages {
    display: grid;
    /* A long compiler line scrolls inside its box and must not widen the page. */
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }

  .languages h3 {
    font-size: var(--fs-1);
  }

  .not-offered {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }

  .not-offered-line {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    color: var(--bad);
    font-weight: 500;
  }

  .sent-back {
    margin-block: var(--space-3);
  }

  .compiler {
    max-height: 16rem;
    margin: var(--space-2) 0 0;
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
    color: var(--ink);
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
