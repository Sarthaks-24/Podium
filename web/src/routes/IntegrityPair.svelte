<!--
  IntegrityPair: one signal up close at
  /admin/contests/{id}/integrity/signals/{signal} (integrity-plan.md
  section 5). The signal comes from the latest check
  (GET .../integrity); for similar code the two programs come from
  GET .../signals/{signal}/pair and sit side by side from 1024 px, stacked
  below, each with its student, language and time, line numbers and the
  shared passages tinted (CodeView), with how much code is behind the
  score and what the score means. Below are the timelines of the students
  named (GET .../timeline/{user}), which are context and never a signal on
  their own. "Open a case" opens OpenCaseDialog with this signal. A signal
  that a newer check replaced says so.

  With ?case={case}&evidence={index} it shows a case's evidence instead:
  the signal as copied into the case (GET /api/admin/integrity/cases/{case})
  and its programs from GET .../cases/{case}/evidence/{index}/pair, so a
  later check can never put other students' code under the case. It links
  back to the case and offers no "Open a case". Props: `id` (the contest),
  `signal`.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import CodeView from '../components/CodeView.svelte';
  import IntegrityNote from '../components/IntegrityNote.svelte';
  import OpenCaseDialog from '../components/OpenCaseDialog.svelte';
  import { ApiError, getJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    SCORE_MEANING,
    evidenceSize,
    formatScore,
    kindWord,
    signalDetail,
    signalStudents,
    strengthWord,
    userName,
    type Case,
    type Moment,
    type Pair,
    type ReviewUser,
    type Run,
    type Signal,
  } from '../lib/review';
  import { navigate, router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { languageLabel } from '../lib/workspace';

  let { id, signal }: { id: string; signal: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'gone' | 'error'>('loading');
  let loadError = $state('');
  let run = $state.raw<Run | null>(null);
  let kase = $state.raw<Case | null>(null);
  let sig = $state.raw<Signal | null>(null);
  let pair = $state.raw<Pair | null>(null);
  let pairError = $state('');
  let timelines = $state<Record<number, Moment[] | string>>({});
  let dialogOpen = $state(false);

  // Read once: the page reloads when the address changes.
  const caseId = router.query.get('case') ?? '';
  const evidence = router.query.get('evidence') ?? '';
  const fromCase = /^\d+$/.test(caseId) && /^\d+$/.test(evidence);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}/integrity`);
  const back = $derived(fromCase ? `/admin/integrity/cases/${caseId}` : `/admin/contests/${encodeURIComponent(id)}/integrity`);
  const backLabel = $derived(fromCase ? (kase ? kase.title : 'Case') : 'Integrity');
  const students = $derived(sig ? [sig.user_a, sig.user_b].filter((u): u is number => !!u) : []);
  // Names: the case's own students first, then the check's map.
  const users = $derived.by(() => {
    const out: Record<string, ReviewUser> = { ...(run?.users ?? {}) };
    for (const u of kase?.users ?? []) out[String(u.user_id)] = { name: u.name, email: out[String(u.user_id)]?.email ?? '' };
    return out;
  });
  const problems = $derived(run?.problems);
  const problem = $derived(sig && sig.problem_id ? problems?.[String(sig.problem_id)] : undefined);
  const size = $derived(sig?.kind === 'similar' ? evidenceSize(sig.evidence) : '');

  function handleAuth(e: unknown): boolean {
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

  function show(found: Signal) {
    setTitle(signalStudents(found, users), kindWord(found.kind));
    loadState = 'ready';
    if (found.kind === 'similar') void loadPair();
    for (const u of [found.user_a, found.user_b]) if (u) void loadTimeline(u);
  }

  async function load() {
    loadState = 'loading';
    if (fromCase) return loadFromCase();
    let r: Run;
    try {
      r = await getJSON<Run>(base);
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Signal not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
      return;
    }
    const found = (r.signals ?? []).find((s) => String(s.id) === signal) ?? null;
    const restore = keepHeadingFocus();
    run = r;
    sig = found;
    if (!found) {
      loadState = 'gone';
      setTitle('Signal not found');
      void restore();
      return;
    }
    show(found);
    void restore();
  }

  // A case's evidence: the case's copy of the signal, names from the case
  // (and from the latest check when there is one).
  async function loadFromCase() {
    let c: Case;
    try {
      c = await getJSON<Case>(`/api/admin/integrity/cases/${caseId}`);
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Evidence not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
      return;
    }
    const found = c.evidence[Number(evidence)] ?? null;
    const restore = keepHeadingFocus();
    kase = c;
    sig = found;
    if (!found) {
      loadState = 'missing';
      setTitle('Evidence not found');
      void restore();
      return;
    }
    try {
      run = await getJSON<Run>(`/api/admin/contests/${c.contest_id}/integrity`);
    } catch {
      // Names and labels are a convenience; the case reads without them.
    }
    show(found);
    void restore();
  }

  async function loadPair() {
    const url = fromCase ? `/api/admin/integrity/cases/${caseId}/evidence/${evidence}/pair` : `${base}/signals/${encodeURIComponent(signal)}/pair`;
    try {
      pair = await getJSON<Pair>(url);
    } catch (e) {
      if (!handleAuth(e)) pairError = messageOf(e);
    }
  }

  async function loadTimeline(user: number) {
    const contest = kase ? String(kase.contest_id) : encodeURIComponent(id);
    try {
      const data = await getJSON<{ moments: Moment[] }>(`/api/admin/contests/${contest}/integrity/timeline/${user}`);
      timelines = { ...timelines, [user]: data.moments ?? [] };
    } catch (e) {
      if (!handleAuth(e)) timelines = { ...timelines, [user]: messageOf(e) };
    }
  }

  function problemLabel(problemId: number | undefined): string {
    if (!problemId) return '';
    return problems?.[String(problemId)]?.label ?? '';
  }

  function created(c: Case) {
    dialogOpen = false;
    showToast('Opened the case');
    navigate(`/admin/integrity/cases/${c.id}`);
  }

  onMount(() => {
    setTitle(routeTitle('integrity-pair'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
  });
</script>

{#if loadState === 'forbidden'}
  <h1 tabindex="-1">Signal</h1>
  <p class="lead">Only owners and admins can review integrity. Ask an owner if you need access.</p>
{:else if loadState !== 'ready' || !sig}
  <a class="back" href={back}>{backLabel}</a>
  <h1 tabindex="-1">{loadState === 'missing' || loadState === 'gone' ? (fromCase ? 'Evidence not found' : 'Signal not found') : 'Signal'}</h1>
  {#if loadState === 'loading'}
    <p class="lead muted" role="status">Loading the signal</p>
  {:else if loadState === 'missing' && fromCase}
    <p class="lead">There is no such evidence in this case, or you cannot review it. <a href="/admin/contests">Go to the contest list.</a></p>
  {:else if loadState === 'missing'}
    <p class="lead">This contest has not been checked yet. <a href={back}>Go to the integrity check.</a></p>
  {:else if loadState === 'gone'}
    <p class="lead">This signal is not in the latest check: a newer check replaced it. Cases keep their own copy. <a href={back}>See the latest signals.</a></p>
  {:else}
    <div class="banner banner-error lead" role="alert"><p>{loadError}</p></div>
    <p class="retry"><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
  {/if}
{:else}
  <a class="back" href={back}>{backLabel}</a>
  <h1 tabindex="-1">{signalStudents(sig, users)}</h1>
  <p class="summary">
    {kindWord(sig.kind)} on {problem ? `problem ${problem.label}, ${problem.title}` : sig.problem_id ? 'this problem' : 'the whole contest'}. Strength:
    {strengthWord(sig.strength).toLowerCase()} <span class="mono">({formatScore(sig.score, sig.kind)})</span>. {signalDetail(sig)}.
  </p>
  {#if sig.kind === 'similar'}
    <p class="meaning muted">{size ? `${size}. ` : ''}{SCORE_MEANING}</p>
  {/if}
  {#if fromCase}
    <p class="from-case muted">Evidence as it was when the case opened; a later check does not change it.</p>
  {:else}
    <p class="actions">
      <button type="button" class="btn btn-primary" onclick={() => (dialogOpen = true)}>Open a case</button>
    </p>
  {/if}

  <div class="note"><IntegrityNote /></div>

  {#if sig.kind === 'similar'}
    <section class="section" aria-labelledby="code-title">
      <h2 id="code-title">Code</h2>
      <p class="legend muted"><span class="swatch" aria-hidden="true"></span>Tinted passages appear in both programs; a bar in the margin marks their lines.</p>
      {#if pairError}
        <div class="banner banner-error" role="alert"><p>{pairError}</p></div>
      {:else if !pair}
        <p class="muted" role="status">Loading the code</p>
      {:else}
        <div class="sides">
          {#each [pair.a, pair.b] as side (side.submission_id)}
            {@const name = userName(users, side.user_id)}
            <div class="side">
              <h3>{name}</h3>
              <p class="meta muted">{languageLabel(side.language)}, submitted <span class="mono">{formatDateTime(side.created_at)}</span>, submission <span class="mono">{side.submission_id}</span></p>
              <CodeView source={side.source} spans={side.spans ?? []} label="{name}'s program" />
            </div>
          {/each}
        </div>
      {/if}
    </section>
  {/if}

  <section class="section" aria-labelledby="timeline-title">
    <h2 id="timeline-title">{students.length === 1 ? 'Timeline' : 'Timelines'}</h2>
    <p class="muted small">Context, never a signal on its own: lab machines are shared, and pasting your own template is allowed.</p>
    <div class="timelines" class:two={students.length > 1}>
      {#each students as u (u)}
        {@const t = timelines[u]}
        <div class="timeline">
          <h3 id="timeline-{u}">{userName(users, u)}</h3>
          {#if t === undefined}
            <p class="muted" role="status">Loading the timeline</p>
          {:else if typeof t === 'string'}
            <p class="field-error">{t}</p>
          {:else if t.length === 0}
            <p class="muted">Nothing recorded.</p>
          {:else}
            <ol class="moments" aria-labelledby="timeline-{u}">
              {#each t as m, i (i)}
                <li class="moment moment-{m.kind}">
                  <span class="mono when">{formatDateTime(m.at)}</span>
                  <span class="what">{#if problemLabel(m.problem_id)}<span class="mono plabel"><span class="visually-hidden">Problem </span>{problemLabel(m.problem_id)}<span class="visually-hidden">: </span></span>{/if}{m.text}</span>
                </li>
              {/each}
            </ol>
          {/if}
        </div>
      {/each}
    </div>
  </section>

  {#if !fromCase}
    <OpenCaseDialog
      open={dialogOpen}
      contestId={id}
      signals={[sig]}
      {users}
      {problems}
      onclose={() => (dialogOpen = false)}
      oncreated={created}
    />
  {/if}
{/if}

<style>
  .back {
    display: inline-block;
    margin-bottom: var(--space-2);
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  h1 {
    font-size: var(--fs-4);
    overflow-wrap: anywhere;
  }

  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .retry {
    margin-top: var(--space-4);
  }

  .summary {
    margin-top: var(--space-3);
    max-width: 56rem;
  }

  .actions {
    margin-top: var(--space-4);
  }

  .meaning,
  .from-case {
    margin-top: var(--space-2);
    max-width: 56rem;
    font-size: var(--fs-0);
  }

  .note {
    margin-top: var(--space-5);
  }

  .section {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-6);
    min-width: 0;
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .small {
    font-size: var(--fs-0);
  }

  .legend {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-0);
  }

  .swatch {
    flex: none;
    width: var(--space-5);
    height: var(--space-4);
    border-left: calc(3 * var(--rule)) solid var(--code-match-edge);
    background: var(--code-match);
  }

  .sides {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-5);
  }

  .side {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    align-content: start;
  }

  .side h3,
  .timeline h3 {
    font-size: var(--fs-2);
  }

  .meta {
    font-size: var(--fs-0);
  }

  .timelines {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-5);
  }

  .timeline {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    align-content: start;
  }

  .moments {
    display: grid;
    padding: 0;
    border: var(--rule) solid var(--line);
    background: var(--surface);
    list-style: none;
    font-size: var(--fs-0);
  }

  .moment {
    position: relative;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .moment:last-child {
    border-bottom: 0;
  }

  .when {
    color: var(--ink-muted);
    white-space: nowrap;
  }

  .plabel {
    margin-right: var(--space-2);
    font-weight: 600;
  }

  .moment-submission .what {
    font-weight: 500;
  }

  @media (min-width: 768px) {
    .timelines.two {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (min-width: 1024px) {
    .sides {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (max-width: 767px) {
    .moment {
      grid-template-columns: minmax(0, 1fr);
      gap: 0;
    }
  }
</style>
