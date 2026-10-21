<!--
  Progress: "Your progress" at /progress, and the same page for an
  organiser at /admin/users/{id}/progress (v2 frontend PLAN.md 6.12,
  contract C8, decisions D92 to D96). The shell loads it lazily. It
  answers one question for a student: how am I doing across the
  semester, where do I lose points, and what should I practise next. A
  personal record, not a ranking: neutral words, no praise or warnings,
  no comparison with anyone (D94), and the place kept small and muted
  (D95).

  From GET /api/me/progress (or the organisers' route), top to bottom:
  the figures row (FiguresRow), "Score over time" (ScoreLine, from two
  contests), "Contests and problems" (ProgressGrid), then "By topic"
  (weakest first) and "By difficulty" (easy, medium, hard) as meter rows
  (MeterList). Beside them from 1280 px, below them on smaller screens:
  "Practise next" (PractiseList) and "Where points were lost" with one
  line of guidance under the largest row. Only final results appear: the
  server leaves out a contest still frozen or held for a reveal.

  States: loading and errors through PageState; no final contest yet
  reads "Your progress appears here after your first contest ends.";
  a student who hid the page (hide_progress) sees that it is hidden and
  "Show my progress"; an organiser view of an account they may not see
  is the forbidden state, an unknown account the missing state. The
  organiser view says whose progress it is in the title and that the
  student sees the same page. Props: `userId` (the account, for the
  organiser view; empty for the student's own page).
-->
<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import FiguresRow from '../components/progress/FiguresRow.svelte';
  import MeterList, { type MeterRow } from '../components/progress/MeterList.svelte';
  import PractiseList from '../components/progress/PractiseList.svelte';
  import ProgressGrid from '../components/progress/ProgressGrid.svelte';
  import ScoreLine from '../components/progress/ScoreLine.svelte';
  import { onMount } from 'svelte';
  import { saveHideProgress } from '../lib/account';
  import { ApiError, getJSON, errorText } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { countOf } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { lossGuidance, progressPath, verdictWords, weakestFirst, withDerived, type Progress } from '../lib/progress';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { userId = '' }: { userId?: string } = $props();

  const organiser = $derived(userId !== '');

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let data = $state.raw<ReturnType<typeof withDerived> | null>(null);
  let showing = $state(false);

  async function load() {
    loadState = 'loading';
    try {
      const p = await getJSON<Progress>(progressPath(organiser ? userId : undefined));
      const restore = keepHeadingFocus();
      data = withDerived(p);
      loadState = 'ready';
      if (organiser) setTitle(ownerTitle(data.user.name));
      void restore();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        session.user = undefined;
        session.status = 'signed-out';
        return;
      }
      if (e instanceof ApiError && e.status === 403) {
        loadState = 'forbidden';
        return;
      }
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
    }
  }

  async function showProgress() {
    showing = true;
    try {
      await saveHideProgress(false);
      await load();
    } catch (e) {
      showToast(e instanceof ApiError ? e.message : 'Your choice could not be saved. Try again in a moment.', 'error');
    } finally {
      showing = false;
    }
  }

  /** "Asha Rao's progress", or "Student progress" for an account with no name. */
  function ownerTitle(name: string): string {
    return name.trim() !== '' ? `${name.trim()}'s progress` : routeTitle('admin-progress');
  }

  const title = $derived(organiser ? (data ? ownerTitle(data.user.name) : routeTitle('admin-progress')) : 'Your progress');
  const titles = $derived(Object.fromEntries((data?.contests ?? []).map((c) => [c.slug, c.title])));

  const topicRows = $derived<MeterRow[]>(
    weakestFirst(data?.by_tag ?? []).map((t) => ({
      key: t.tag,
      label: t.tag,
      value: t.earned,
      max: t.available,
      valueText: `${t.earned} of ${t.available}`,
      note: t.problems === 1 ? '(1 problem)' : undefined,
      muted: t.problems === 1,
    })),
  );

  const difficultyRows = $derived<MeterRow[]>(
    (data?.by_difficulty ?? []).map((d) => ({
      key: d.difficulty,
      label: d.difficulty.charAt(0).toUpperCase() + d.difficulty.slice(1),
      value: d.earned,
      max: d.available,
      valueText: `${d.earned} of ${d.available}`,
      note: d.problems === 1 ? '(1 problem)' : undefined,
    })),
  );

  const lossTotal = $derived((data?.losses ?? []).reduce((n, l) => n + l.problems, 0));
  const lossRows = $derived<MeterRow[]>(
    (data?.losses ?? []).map((l) => {
      const words = verdictWords(l.verdict);
      return {
        key: l.verdict,
        label: words.charAt(0).toUpperCase() + words.slice(1),
        value: l.problems,
        max: lossTotal,
        valueText: countOf(l.problems, 'problem'),
      };
    }),
  );
  const topLoss = $derived(data && data.losses.length > 0 ? lossGuidance(data.losses[0].verdict) : '');

  const hidden = $derived(!organiser && data?.hide_progress === true);

  onMount(() => {
    setTitle(organiser ? routeTitle('admin-progress') : routeTitle('progress'));
    void load();
  });
</script>

<div class="progress-page">
  <PageHeader
    {title}
    back={organiser ? null : { href: '/', label: 'Your contests' }}
    crumbs={organiser
      ? [
          { label: 'Students', href: '/admin/students' },
          ...(data ? [{ label: data.user.name || 'Student', href: `/admin/students/${encodeURIComponent(userId)}` }] : []),
          { label: 'Progress' },
        ]
      : undefined}
    lead={organiser
      ? 'The page the student sees, with their final results. Only owners and admins can open it.'
      : 'Your final results across the contests you took part in. Only you and the organisers see this page.'}
  />

  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading progress" />
  {:else if loadState === 'error'}
    <PageState kind="error" message={loadError} onretry={load} />
  {:else if loadState === 'forbidden'}
    <PageState kind="forbidden" message="Only owners and admins can see a student's progress." />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no account with that number." parent={{ href: '/admin/students', label: 'Go to the students.' }} />
  {:else if data && hidden}
    <section class="panel hidden-note" aria-labelledby="hidden-title">
      <h2 id="hidden-title">Your progress is hidden</h2>
      <p>You chose to hide this page. Your results are still kept, and the organisers still see them.</p>
      <div class="action-row">
        <button type="button" class="btn btn-primary" disabled={showing} onclick={showProgress}>Show my progress</button>
      </div>
    </section>
  {:else if data && data.contests.length === 0}
    <PageState
      kind="empty"
      message={organiser ? 'No final results yet. Progress appears after the first contest this student took part in ends.' : 'Your progress appears here after your first contest ends.'}
    />
  {:else if data}
    <div class="page-split">
      <div class="page-split-main">
        <section aria-label="Totals">
          <FiguresRow totals={data.totals} />
        </section>

        {#if data.contests.length >= 2}
          <section class="block" aria-labelledby="line-title">
            <h2 id="line-title">Score over time</h2>
            <div class="panel">
              <ScoreLine contests={data.contests} />
            </div>
          </section>
        {/if}

        <section class="block" aria-labelledby="grid-title">
          <h2 id="grid-title">Contests and problems</h2>
          <ProgressGrid contests={data.contests} {organiser} />
        </section>

        {#if topicRows.length > 0}
          <section class="block" aria-labelledby="topic-title">
            <h2 id="topic-title">By topic</h2>
            <p class="muted small">Points earned of points available, weakest first.</p>
            <MeterList rows={topicRows} />
          </section>
        {/if}

        {#if difficultyRows.length > 0}
          <section class="block" aria-labelledby="difficulty-title">
            <h2 id="difficulty-title">By difficulty</h2>
            <MeterList rows={difficultyRows} />
          </section>
        {/if}
      </div>

      <aside class="page-split-aside" aria-label="What to work on">
        <section class="panel" aria-labelledby="practise-title">
          <h2 id="practise-title" class="panel-title">Practise next</h2>
          {#if data.retry.length > 0}
            <PractiseList rows={data.retry} {titles} {organiser} />
          {:else}
            <p class="muted">Every problem is at full score.</p>
          {/if}
        </section>

        {#if lossRows.length > 0}
          <section class="panel" aria-labelledby="loss-title">
            <h2 id="loss-title" class="panel-title">Where points were lost</h2>
            <p class="muted small">The result of the best attempt on each problem below full score.</p>
            <MeterList rows={lossRows} stacked />
            {#if topLoss}<p class="guidance">{topLoss}</p>{/if}
          </section>
        {/if}
      </aside>
    </div>
  {/if}
</div>

<style>
  .progress-page {
    display: grid;
    gap: var(--space-6);
    min-width: 0;
  }

  .block {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .page-split-main {
    gap: var(--section-gap);
  }

  .panel-title {
    font-size: var(--fs-sub);
  }

  .hidden-note {
    max-width: var(--measure-form);
  }

  .guidance {
    max-width: 60ch;
  }
</style>
