<!--
  LanguageProofPanel: the "Languages" list of a function problem, inside the
  editor's "What students write" fold. One block per language, in the server's
  order. An offered language says its last check ("Passes every test",
  "Not checked yet" for one that never ran, which is the only sign that a
  hand-edited problem was not proved) and has a fold with a paste box and
  "Check <language>": the pasted solution is used for that one check and is
  not saved with the problem. A language that is not offered shows why in an
  error banner, with the failed tests or the compiler's first lines behind
  Details, and "Fix <language> with AI", which asks the AI for new locked and
  starting code and offers the language only if every test passes. A refusal
  of either route is said next to the control that caused it, tied to it with
  aria-describedby; a missing key links to where the key is added. While an
  action runs its button says what is happening and has aria-busy, and each
  block has one status line for the result. With unsaved changes both
  actions are off and the panel says to save first, because the checks run
  on the saved problem.

  The editor owns the state: it reads GET .../proof, keeps asking while a
  round is open and passes the answer in. Props: `problemId`, `proof` (the
  latest answer), `dirty` (the editor has unsaved changes), `awaiting` (a round
  was started here and its end is not seen yet), `onstarted` (called when a
  route accepted a request, so the editor reads the state and watches until
  the round ends) and `onstale` (the panel was out of date: read the state
  and the problem again). Each language has one status line that is always in
  the page, empty and visually hidden until it has text: it says the start,
  the progress and the result.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import Icon from '../Icon.svelte';
  import { isKeyProblem } from '../../lib/ai';
  import { ApiError, sendJSON } from '../../lib/api';
  import { verdictLabel } from '../../lib/problems';
  import { checkResultLine, failureText, languageName, lastCheckLine, checkingLine, proofPaths, proofRefusal, type ProofLanguage, type ProofState } from '../../lib/proof';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    problemId: number;
    proof: ProofState;
    dirty: boolean;
    awaiting: boolean;
    onstarted: () => Promise<void>;
    onstale: () => Promise<void>;
  }

  let { problemId, proof, dirty, awaiting, onstarted, onstale }: Props = $props();

  /** The model can take minutes, as in Edit with AI. */
  const ASK_TIMEOUT_MS = 4 * 60 * 1000;
  const MAX_SOLUTION_BYTES = 64 * 1024;

  type Action = { language: string; kind: 'fix' | 'check'; stage: 'asking' | 'checking' };
  interface Refusal {
    control: 'fix' | 'check' | 'box';
    message: string;
    code: string;
    problems: readonly string[];
  }

  let working = $state<Action | null>(null);
  let solutions = $state<Record<string, string>>({});
  let refusals = $state<Record<string, Refusal | undefined>>({});
  let results = $state<Record<string, string>>({});

  const busy = $derived(working !== null || proof.checking || awaiting);

  const idOf = (language: string) => `lang-${language}`;

  function dirtyNote(): string {
    return dirty ? ' proof-dirty' : '';
  }

  function describedBy(language: string, help: string, control: Refusal['control']): string {
    const r = refusals[language];
    return `${help}${r && r.control === control ? ` ${idOf(language)}-error` : ''}${dirtyNote()}`;
  }

  /** The round this panel started has ended: say how it went and put focus where it is useful. */
  async function finish(w: Action) {
    working = null;
    const l = proof.languages.find((x) => x.language === w.language);
    const name = languageName(w.language);
    if (w.kind === 'fix') {
      if (l?.offered && l.result === 'passed') {
        results[w.language] = '';
        showToast(`${name} passes every test and is now offered.`);
        await tick();
        document.getElementById(idOf(w.language))?.focus();
        return;
      }
      const why = proof.applied?.discarded ? 'The problem changed during the check.' : l ? failureText(l) : '';
      results[w.language] = `${why === '' ? '' : `${why} `}Nothing was changed.`;
      await tick();
      document.getElementById(`${idOf(w.language)}-fix`)?.focus();
      return;
    }
    results[w.language] = l && l.source === 'check' ? checkResultLine(l) : `${name} could not be checked.`;
    await tick();
    document.getElementById(`${idOf(w.language)}-check`)?.focus();
  }

  $effect(() => {
    const w = working;
    if (w && w.stage === 'checking' && !proof.checking && !awaiting) void finish(w);
  });

  function refuse(language: string, control: Refusal['control'], e: unknown) {
    refusals[language] = { control, ...proofRefusal(e, language) };
    if (['checking', 'not_offered', 'already_offered'].includes(refusals[language]?.code ?? '')) void onstale();
  }

  async function run(language: string, kind: 'fix' | 'check') {
    if (busy) return;
    refusals[language] = undefined;
    results[language] = '';
    let body: Record<string, string> = {};
    if (kind === 'check') {
      const solution = solutions[language] ?? '';
      if (solution.trim() === '' || new TextEncoder().encode(solution).length > MAX_SOLUTION_BYTES) {
        refusals[language] = { control: 'box', message: 'Paste a solution of at most 64 KB.', code: 'bad_request', problems: [] };
        await tick();
        document.getElementById(`${idOf(language)}-solution`)?.focus();
        return;
      }
      body = { solution };
    }
    working = { language, kind, stage: 'asking' };
    try {
      await sendJSON('POST', kind === 'fix' ? proofPaths.fix(problemId, language) : proofPaths.check(problemId, language), body, { timeoutMs: ASK_TIMEOUT_MS });
    } catch (e) {
      working = null;
      refuse(language, kind, e);
      // A client timeout may still have queued the round: watch for it.
      if (e instanceof ApiError && e.code === 'timeout') void onstarted();
      await tick();
      document.getElementById(`${idOf(language)}-${kind}`)?.focus();
      return;
    }
    const started = onstarted();
    working = { language, kind, stage: 'checking' };
    await started;
  }

  const label = (l: ProofLanguage, kind: 'fix' | 'check') => {
    const name = languageName(l.language);
    if (working?.language !== l.language || working.kind !== kind) return kind === 'fix' ? `Fix ${name} with AI` : `Check ${name}`;
    return working.stage === 'asking' && kind === 'fix' ? 'Asking the AI' : `Checking ${name}`;
  };

  /** What the language's status line says now: the start, the progress or the result. */
  function liveText(language: string): string {
    if (working?.language === language) {
      if (working.stage === 'asking') return working.kind === 'fix' ? 'Asking the AI.' : `Checking ${languageName(language)}.`;
      return proof.checking ? checkingLine(proof) : `Checking ${languageName(language)}.`;
    }
    return results[language] ?? '';
  }

  /** The reason line of a language that is not offered. */
  function reason(l: ProofLanguage): string {
    return failureText(l) || 'The AI did not write it, or it has no locked code yet. Fix it with AI, or write its locked code and starting code in the editor and save.';
  }
</script>

<section class="languages" aria-labelledby="languages-heading">
  <h3 id="languages-heading" class="sub">Languages</h3>
  {#if dirty}
    <p id="proof-dirty" class="muted">Save the problem first. The check runs on the saved version.</p>
  {/if}

  {#each proof.languages as l (l.language)}
    {@const name = languageName(l.language)}
    {@const id = idOf(l.language)}
    {@const refusal = refusals[l.language]}
    <div {id} class="lang" role="group" aria-labelledby="{id}-name" tabindex="-1">
      <p class="lang-head">
        <span id="{id}-name" class="lang-name">{name}</span>
        <span class="lang-state">{l.offered ? 'Offered' : 'Not offered'}</span>
      </p>

      {#if l.offered}
        <p class="muted">{lastCheckLine(l)}</p>
        <details class="check">
          <summary>Check {name} with a solution</summary>
          <div class="field">
            <label for="{id}-solution">Your solution in {name}</label>
            <textarea
              id="{id}-solution"
              class="input mono"
              rows="8"
              spellcheck="false"
              translate="no"
              bind:value={solutions[l.language]}
              aria-describedby={describedBy(l.language, `${id}-help`, 'box')}
              aria-invalid={refusal?.control === 'box' ? 'true' : undefined}
            ></textarea>
            <p id="{id}-help" class="field-help">The solution is used for this check and is not saved with the problem.</p>
          </div>
          <p>
            <button
              id="{id}-check"
              type="button"
              class="btn btn-secondary"
              disabled={busy || dirty}
              aria-busy={working?.language === l.language && working.kind === 'check' ? 'true' : undefined}
              aria-describedby={describedBy(l.language, `${id}-help`, 'check')}
              onclick={() => run(l.language, 'check')}>{label(l, 'check')}</button
            >
          </p>
        </details>
      {:else}
        <div class="banner banner-error fail">
          <p class="fail-line">
            <span class="fail-icon" aria-hidden="true"><Icon name="circle-x" /></span>
            <span>{reason(l)}</span>
          </p>
          {#if l.compiler.trim() !== '' || l.tests.length > 0}
            <details>
              <summary>Details</summary>
              {#if l.compiler.trim() !== ''}
                <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                <pre class="compiler" tabindex="0" translate="no" aria-label="Compiler output">{l.compiler}</pre>
              {/if}
              {#if l.tests.length > 0}
                <ul class="tests">{#each l.tests as t (t.idx)}<li>Test {t.idx}: {verdictLabel(t.verdict)}</li>{/each}</ul>
              {/if}
            </details>
          {/if}
        </div>
        <p id="{id}-fix-help" class="field-help">
          The AI writes new locked code and starting code for {name}. Your current {name} code is replaced only if the AI's version passes every test.
        </p>
        <p>
          <button
            id="{id}-fix"
            type="button"
            class="btn btn-secondary"
            disabled={busy || dirty}
            aria-busy={working?.language === l.language && working.kind === 'fix' ? 'true' : undefined}
            aria-describedby={describedBy(l.language, `${id}-fix-help`, 'fix')}
            onclick={() => run(l.language, 'fix')}>{label(l, 'fix')}</button
          >
        </p>
      {/if}

      {#if refusal}
        <div id="{id}-error" class="field-error refusal" role="alert">
          <p>{refusal.message}{#if isKeyProblem(refusal.code)} <a href="/admin/ai/new">Change key or model</a>{/if}</p>
          {#if refusal.problems.length > 0}
            <ul>{#each refusal.problems as line, i (i)}<li>{line}</li>{/each}</ul>
          {/if}
        </div>
      {/if}
      <p class="result" class:visually-hidden={liveText(l.language) === ''} role="status">{liveText(l.language)}</p>
    </div>
  {/each}
</section>

<style>
  .languages {
    display: grid;
    gap: var(--space-4);
  }

  .lang {
    display: grid;
    gap: var(--space-2);
    justify-items: start;
    min-width: 0;
    padding-top: var(--space-3);
    border-top: var(--rule) solid var(--line);
  }

  .lang > * {
    max-width: 100%;
  }

  .lang-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-3);
  }

  .lang-name {
    font-weight: 500;
  }

  .lang-state {
    color: var(--ink-muted);
  }

  .fail {
    display: grid;
    /* One track no wider than the banner, so a long compiler line scrolls in its own box. */
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    width: 100%;
    max-width: var(--measure-form);
  }

  .fail-line {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    font-weight: 500;
  }

  .fail-icon {
    display: inline-flex;
    flex: none;
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

  .tests {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .check {
    width: 100%;
    max-width: var(--measure-form);
  }

  .check > summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    font-weight: 500;
    cursor: pointer;
  }

  .check .field {
    margin-top: var(--space-3);
  }

  .check textarea {
    width: 100%;
    resize: vertical;
  }

  .check p:last-child {
    margin-top: var(--space-3);
  }

  .refusal ul {
    margin-top: var(--space-1);
    padding-left: var(--space-5);
  }

</style>
