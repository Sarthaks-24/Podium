<!--
  IntegrityPair: one signal up close at
  /admin/contests/{id}/integrity/signals/{signal}. The signal comes from
  the latest check (GET .../integrity); for similar code the two programs
  come from GET .../signals/{signal}/pair and sit side by side from
  1024 px, stacked below, each with its student, language and time, line
  numbers and the shared passages tinted (CodeView), with the strength in
  words and its reason ("Short programs, identical"), the numbers behind it
  and what the score means. Below are the timelines of the
  students named (GET .../timeline/{user}), which are context and never a
  signal on their own, then one "Record a decision" button per student
  (DecisionDialog). A signal that a newer check replaced says so. The trail
  runs Contests, the contest, Similar code, this pair. The check loads
  through lib/loader, so an answer after the page has gone is dropped.
  Props: `id` (the contest), `signal`.
-->
<script lang="ts">
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import type { Crumb } from '../components/Breadcrumbs.svelte';
  import type { AdminContest } from '../lib/contests';
  import { onMount } from 'svelte';
  import CodeView from '../components/CodeView.svelte';
  import IntegrityNote from '../components/IntegrityNote.svelte';
  import DecisionDialog from '../components/DecisionDialog.svelte';
  import { errorText, getJSON, isSignedOut } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import { routeTitle } from '../lib/routing';
  import {
    SCORE_MEANING,
    kindWord,
    signalDetail,
    signalStudents,
    similarNumbers,
    similarReason,
    strengthWord,
    userName,
    decisionWord,
    type Case,
    type Moment,
    type Pair,
    type ReviewUser,
    type Run,
    type Signal,
  } from '../lib/review';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { languageLabel } from '../lib/workspace';

  let { id, signal }: { id: string; signal: string } = $props();

  let run = $state.raw<Run | null>(null);
  let sig = $state.raw<Signal | null>(null);
  let pair = $state.raw<Pair | null>(null);
  let pairError = $state('');
  let timelines = $state<Record<number, Moment[] | string>>({});
  let deciding = $state<{ id: number; name: string } | null>(null);
  let cases = $state.raw<Case[]>([]);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}/integrity`);
  const back = $derived(`/admin/contests/${encodeURIComponent(id)}/integrity`);
  let contestTitle = $state('');
  // Taken as the data arrives, just before the loading view gives way, so the heading keeps focus.
  let restoreFocus: () => Promise<void> = async () => undefined;

  // The check, and in it this signal; a check without it is "gone" (a newer check replaced it).
  const page = loader(
    () => getJSON<Run>(base),
    (r) => {
      restoreFocus = keepHeadingFocus();
      run = r;
      sig = (r.signals ?? []).find((s) => String(s.id) === signal) ?? null;
    },
  );
  const gone = $derived(page.state === 'ready' && !sig);

  /** Contests, the contest, Similar code, then this page. */
  function trail(here: string): Crumb[] {
    const contest = `/admin/contests/${encodeURIComponent(id)}`;
    const out: Crumb[] = [
      { label: 'Contests', href: '/admin/contests' },
      { label: contestTitle || 'Contest', href: contest },
      { label: 'Similar code', href: `${contest}/integrity` },
    ];
    return [...out, { label: here }];
  }
  const students = $derived(sig ? [sig.user_a, sig.user_b].filter((u): u is number => !!u) : []);
  const users = $derived<Record<string, ReviewUser>>(run?.users ?? {});
  const problems = $derived(run?.problems);
  const problem = $derived(sig && sig.problem_id ? problems?.[String(sig.problem_id)] : undefined);

  /** A side request that failed because the page is gone or signed out says nothing. */
  function quiet(e: unknown): boolean {
    return isSignedOut(e) || !page.live;
  }

  async function load() {
    await page.load();
    void restoreFocus();
    if (page.state === 'missing' || gone) setTitle('Signal not found');
    if (page.state !== 'ready' || !sig) return;
    show(sig);
  }

  function show(found: Signal) {
    setTitle(signalStudents(found, users), kindWord(found.kind));
    if (found.kind === 'similar') void loadPair();
    for (const u of [found.user_a, found.user_b]) if (u) void loadTimeline(u);
  }

  async function loadPair() {
    try {
      const p = await getJSON<Pair>(`${base}/signals/${encodeURIComponent(signal)}/pair`);
      if (page.live) pair = p;
    } catch (e) {
      if (!quiet(e)) pairError = errorText(e);
    }
  }

  async function loadTimeline(user: number) {
    const contest = encodeURIComponent(id);
    try {
      const data = await getJSON<{ moments: Moment[] }>(`/api/admin/contests/${contest}/integrity/timeline/${user}`);
      if (page.live) timelines = { ...timelines, [user]: data.moments ?? [] };
    } catch (e) {
      if (!quiet(e)) timelines = { ...timelines, [user]: errorText(e) };
    }
  }

  function problemLabel(problemId: number | undefined): string {
    if (!problemId) return '';
    return problems?.[String(problemId)]?.label ?? '';
  }

  async function loadCases() {
    try {
      const list = (await getJSON<{ cases: Case[] }>(`${base}/cases`)).cases ?? [];
      if (page.live) cases = list;
    } catch {
      // The decisions are added to the page when they load; without them each student reads "no decision recorded".
    }
  }

  function decided(c: Case) {
    deciding = null;
    showToast('Recorded the decision');
    cases = [c, ...cases.filter((x) => x.user_id !== c.user_id)];
  }

  const caseOf = (u: number) => cases.find((c) => c.user_id === u) ?? null;

  onMount(() => {
    setTitle(routeTitle('integrity-pair'));
    if (!isOrganiser) page.forbid();
    else {
      void load();
      void loadCases();
      // The contest title is only for the trail; the page reads without it.
      getJSON<AdminContest>(`/api/admin/contests/${encodeURIComponent(id)}`)
        .then((c) => {
          if (page.live) contestTitle = c.title;
        })
        .catch(() => {});
    }
    return () => page.stop();
  });
</script>

{#if page.state === 'forbidden'}
  <PageHeader title="Signal" />
  <PageState kind="forbidden" message="Only organisers can review similar code." />
{:else if page.state !== 'ready' || !sig}
  <PageHeader
    title={page.state === 'missing' || gone ? 'Signal not found' : 'Signal'}
    crumbs={trail(page.state === 'missing' || gone ? 'Signal not found' : 'Signal')}
  />
  {#if page.state === 'loading'}
    <PageState kind="loading" message="Loading the signal" />
  {:else if page.state === 'missing'}
    <PageState kind="missing" message="This contest has not been checked yet." parent={{ href: back, label: 'Go to similar code.' }} />
  {:else if gone}
    <PageState kind="missing" message="This signal is not in the latest check: a newer check replaced it." parent={{ href: back, label: 'See the latest signals.' }} />
  {:else}
    <PageState kind="error" message={page.error} onretry={load} />
  {/if}
{:else}
  <PageHeader title={signalStudents(sig, users)} crumbs={trail(signalStudents(sig, users))} />
  <p class="summary">
    {kindWord(sig.kind)} on {problem ? `problem ${problem.label}, ${problem.title}` : sig.problem_id ? 'this problem' : 'the whole contest'}. Strength:
    {strengthWord(sig.strength).toLowerCase()}{sig.kind === 'similar' ? `, ${similarReason(sig.score, sig.evidence).toLowerCase()}` : ''}. {signalDetail(sig)}.
  </p>
  {#if sig.kind === 'similar'}
    <p class="meaning muted">{similarNumbers(sig.score, sig.evidence)}. {SCORE_MEANING}</p>
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
              <p class="meta muted">{languageLabel(side.language)}, submitted <MonoDigits text={formatDateTime(side.created_at)} />, submission <span class="mono">{side.submission_id}</span></p>
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
                  <span class="when"><MonoDigits text={formatDateTime(m.at)} /></span>
                  <span class="what">{#if problemLabel(m.problem_id)}<span class="mono plabel"><span class="visually-hidden">Problem </span>{problemLabel(m.problem_id)}<span class="visually-hidden">: </span></span>{/if}{m.text}</span>
                </li>
              {/each}
            </ol>
          {/if}
        </div>
      {/each}
    </div>
  </section>

  <!-- After the code and the timelines it asks you to read first. -->
  <section class="section" aria-labelledby="decide-title">
    <h2 id="decide-title">Decisions</h2>
    <ul class="decide">
      {#each students as u (u)}
        {@const c = caseOf(u)}
        <li>
          <span>{userName(users, u)}: {c ? `${decisionWord(c.decision)}. ${c.reason}` : 'no decision recorded.'}</span>
          <button type="button" class="btn btn-secondary btn-compact" onclick={() => (deciding = { id: u, name: userName(users, u) })}
            >{c ? 'Change' : 'Record a decision'}<span class="visually-hidden"> about {userName(users, u)}</span></button
          >
        </li>
      {/each}
    </ul>
  </section>
  <DecisionDialog
    open={deciding !== null}
    contestId={id}
    student={deciding}
    current={deciding ? caseOf(deciding.id) : null}
    onclose={() => (deciding = null)}
    ondecided={decided}
  />
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-narrow);
  }

  .retry {
    margin-top: var(--space-4);
  }

  .summary {
    margin-top: var(--space-3);
    max-width: var(--measure-wide);
  }

  .actions {
    margin-top: var(--space-4);
  }

  .meaning,
  .decide {
    display: grid;
    gap: var(--space-2);
    padding: 0;
    list-style: none;
  }

  .decide li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .from-case {
    margin-top: var(--space-2);
    max-width: var(--measure-wide);
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

  @media (width < 768px) {
    .moment {
      grid-template-columns: minmax(0, 1fr);
      gap: 0;
    }
  }
</style>
