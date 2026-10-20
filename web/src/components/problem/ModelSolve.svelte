<!--
  ModelSolve: the problem editor's "Can a model solve this?" section (M5,
  split out of ProblemEditor in v2 P3 F5.1). It reads the AI key's state,
  asks a model for a solution after a confirmation, follows the `wizard`
  and `submission` live events with polling as the fallback, and shows
  "A model solved this on its first try: N of 100" with what that means for
  graded contests and a link to the submission on the Try page. Props:
  `id` (the problem id from the address) and `onsessionerror`, the
  editor's handler for a signed-out or forbidden answer (true when it
  handled the error).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { aiPaths, isKeyProblem, modelLabel, solveLine, solveMeaning, wizardPending, type KeyStatus, type Wizard } from '../../lib/ai';
  import { ApiError, getJSON, sendJSON } from '../../lib/api';
  import { problemPath } from '../../lib/problems';
  import { EventStream, type StreamMode } from '../../lib/stream';
  import type { Submission } from '../../lib/workspace';

  interface Props {
    id: string;
    onsessionerror: (e: unknown) => boolean;
  }

  let { id, onsessionerror }: Props = $props();

  /** How often the solve request is asked for while live updates are down. */
  const POLL_MS = 2000;

  let aiStatus = $state<KeyStatus | null>(null);
  let solveOpen = $state(false);
  let solveBusy = $state(false);
  let solveWizard = $state<Wizard | null>(null);
  let solveSubmission = $state<Submission | null>(null);
  let solveError = $state<{ message: string; code: string; reasons: readonly string[] } | null>(null);
  let solveMode = $state<StreamMode>('connecting');

  const solveWaiting = $derived(
    solveWizard !== null &&
      solveWizard.state !== 'error' &&
      (wizardPending(solveWizard) || solveSubmission === null || solveSubmission.state === 'queued' || solveSubmission.state === 'running'),
  );
  const solveDone = $derived(solveSubmission !== null && solveSubmission.state === 'done');
  const solveStatus = $derived.by(() => {
    if (solveWizard === null) return '';
    if (solveWizard.state === 'error') return 'The model could not answer';
    if (wizardPending(solveWizard)) return 'Waiting for the model to answer';
    if (solveSubmission === null || solveSubmission.state === 'queued' || solveSubmission.state === 'running') return "Judging the model's solution";
    if (solveSubmission.state === 'error') return "The judge could not finish the model's submission";
    return solveLine(solveSubmission);
  });
  const solveTone = $derived.by(() => {
    const s = solveSubmission;
    if (!s || s.state !== 'done') return 'note';
    if (s.max_score > 0 && s.score >= s.max_score) return 'bad';
    return s.score > 0 ? 'part' : 'ok';
  });
  const keyMissing = $derived(aiStatus !== null && !aiStatus.active);

  let alive = true;
  let solveTimer: ReturnType<typeof setTimeout> | undefined;
  let solveStream: EventStream | null = null;

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  onMount(() => {
    getJSON<KeyStatus>(aiPaths.key)
      .then((s) => {
        if (alive) aiStatus = s;
      })
      .catch(() => {
        // Without the key's state the button still works; the server says when a key is missing.
        if (alive) aiStatus = null;
      });
    return () => {
      alive = false;
      stopSolveWatch();
    };
  });

  function stopSolveWatch() {
    solveStream?.stop();
    solveStream = null;
    clearTimeout(solveTimer);
  }

  function solveFinished(): boolean {
    if (!solveWizard) return true;
    if (solveWizard.state === 'error') return true;
    const s = solveSubmission;
    return s !== null && (s.state === 'done' || s.state === 'error');
  }

  /** Takes a newer copy of the solve request; true when it just gained its submission. */
  function adoptSolveWizard(w: Wizard): boolean {
    if (!solveWizard || String(w.id) !== String(solveWizard.id)) return false;
    const hadSubmission = solveWizard.submission_id ?? null;
    solveWizard = w;
    if (w.state === 'error') {
      solveError = { message: w.error?.message ?? 'The model could not answer.', code: w.error?.code ?? 'error', reasons: w.error?.reasons ?? [] };
      stopSolveWatch();
      return false;
    }
    return hadSubmission === null && !!w.submission_id;
  }

  function adoptSolveSubmission(s: Submission) {
    if (!solveWizard?.submission_id || s.id !== solveWizard.submission_id) return;
    solveSubmission = s;
    if (solveFinished()) stopSolveWatch();
  }

  function scheduleSolvePoll() {
    clearTimeout(solveTimer);
    if (!alive || solveFinished()) return;
    solveTimer = setTimeout(pollSolve, solveMode === 'live' ? 3 * POLL_MS : POLL_MS);
  }

  async function pollSolve() {
    const w = solveWizard;
    if (!w) return;
    try {
      if (!w.submission_id) {
        const next = await getJSON<Wizard>(aiPaths.wizardOne(w.id));
        if (!alive) return;
        adoptSolveWizard(next);
      }
      const sid = solveWizard?.submission_id;
      if (sid && !solveFinished()) {
        const s = await getJSON<Submission>(`/api/submissions/${encodeURIComponent(String(sid))}`);
        if (!alive) return;
        adoptSolveSubmission(s);
      }
    } catch (e) {
      if (onsessionerror(e)) return;
      // A failed poll is tried again on the next tick.
    }
    scheduleSolvePoll();
  }

  async function solve() {
    solveError = null;
    solveSubmission = null;
    solveBusy = true;
    try {
      const answer = await sendJSON<{ wizard_id: string | number }>('POST', problemPath(id, '/solve'));
      solveWizard = { id: String(answer.wizard_id), state: 'thinking' };
      solveOpen = false;
      stopSolveWatch();
      solveMode = 'connecting';
      solveStream = new EventStream({
        url: '/api/events',
        events: ['wizard', 'submission'],
        onEvent: (type, data) => {
          if (!data || typeof data !== 'object') return;
          // A submission that just appeared is fetched at once; later changes come as events.
          if (type === 'wizard' && adoptSolveWizard(data as Wizard)) void pollSolve();
          else if (type === 'submission') adoptSolveSubmission(data as Submission);
        },
        onReconnect: () => void pollSolve(),
        poll: () => undefined,
        onMode: (m) => {
          solveMode = m;
        },
      });
      solveStream.start();
      scheduleSolvePoll();
    } catch (e) {
      solveOpen = false;
      if (onsessionerror(e)) return;
      solveError = {
        message: messageOf(e),
        code: e instanceof ApiError ? e.code : 'unexpected',
        reasons: e instanceof ApiError ? e.problems : [],
      };
      if (e instanceof ApiError && e.code === 'no_key' && aiStatus) aiStatus = { ...aiStatus, active: false };
    } finally {
      solveBusy = false;
    }
  }
</script>

<section class="block solve" aria-labelledby="solve-heading">
  <h2 id="solve-heading" tabindex="-1">Can a model solve this?</h2>
  <p class="solve-help">
    Sends only the statement to the AI model with your key and asks for a C++ solution, then judges the answer on this problem's
    tests as your submission. It shows what a student who pastes the statement into a model would get.
  </p>

  <p class="visually-hidden" role="status">{solveWizard ? solveStatus : ''}</p>

  {#if solveDone && solveSubmission}
    <div class="solve-result tone-box-{solveTone}">
      <p class="solve-line">{solveLine(solveSubmission)}</p>
      <p>{solveMeaning(solveSubmission)}</p>
      <p>
        <a href="/admin/problems/{id}/try">See the model's submission</a>
        <span class="muted">on the Try as a student page.</span>
      </p>
    </div>
  {:else if solveWaiting}
    <p class="solve-progress muted">{solveStatus}</p>
    {#if solveMode === 'polling'}
      <p class="field-help">Live updates are down, so the result is checked every few seconds.</p>
    {/if}
  {:else if solveSubmission && solveSubmission.state === 'error'}
    <div class="banner banner-error problems-list"><p>{solveStatus}. Ask again.</p></div>
  {/if}

  {#if solveError}
    <div class="banner banner-error problems-list" role="alert">
      <p>
        {solveError.message}
        {#if isKeyProblem(solveError.code)}<a href="/admin/ai">Go to AI assistant</a>{/if}
      </p>
      {#if solveError.reasons.length > 0}
        <ul>{#each solveError.reasons as line, i (i)}<li>{line}</li>{/each}</ul>
      {/if}
    </div>
  {/if}

  <div class="solve-actions">
    <button
      type="button"
      class="btn btn-secondary"
      disabled={keyMissing}
      aria-disabled={solveBusy || solveWaiting ? 'true' : undefined}
      aria-describedby={keyMissing ? 'solve-key-help' : undefined}
      onclick={() => {
        if (!solveBusy && !solveWaiting) solveOpen = true;
      }}
    >
      {solveWaiting ? 'Asking a model' : solveDone ? 'Ask a model again' : 'Ask a model to solve it'}
    </button>
    {#if keyMissing}
      <p id="solve-key-help" class="field-help">No AI key. <a href="/admin/ai">Add a key in AI assistant</a> to use this.</p>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={solveOpen}
  title="Ask a model to solve this problem?"
  body="The statement goes to {aiStatus?.active
    ? modelLabel(aiStatus, aiStatus.provider, aiStatus.model)
    : 'the AI model'} with your key, and its answer is judged on the tests as your submission. Nothing else is sent."
  confirmLabel="Ask a model"
  busy={solveBusy}
  onconfirm={solve}
  oncancel={() => (solveOpen = false)}
/>

<style>
  .solve {
    display: grid;
    gap: var(--space-3);
    max-width: 64rem;
  }

  .solve-help {
    max-width: 72ch;
    color: var(--ink-muted);
  }

  .solve-result {
    display: grid;
    gap: var(--space-2);
    max-width: 48rem;
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .tone-box-bad {
    border-color: var(--bad);
    background: var(--bad-tint);
  }

  .tone-box-part {
    border-color: var(--part);
    background: var(--part-tint);
  }

  .tone-box-ok {
    border-color: var(--ok);
    background: var(--ok-tint);
  }

  .solve-line {
    font-weight: 600;
  }

  .solve-progress {
    font-weight: 500;
  }

  .solve-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  @media (width < 768px) {
    .solve-actions .btn {
      flex: 1 1 auto;
    }
  }
</style>
