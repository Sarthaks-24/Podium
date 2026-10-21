<!--
  Student: one student for organisers at /admin/students/{id} (v2 P4 F9.4;
  GET /api/admin/students/{id}). The shell loads it lazily. It answers how
  this student is doing against the class, from final contests only.

  The work column: "Score over time" (components/students/TrendLine: the
  student's percent beside the class median, from two graded contests,
  with "Show as table"), then "Contests", newest first: score and percent,
  the class median, the place (small and muted), problems solved of those
  tried, and accepted of submitted. Each contest opens its results.
  Beside it from 1280 px, first below that: links to the record page
  (standing, rating, cases and notes; every visit there is logged) and to
  the progress page as the student sees it. No integrity data is on this
  page. States: loading, error, forbidden (anyone but owners and admins),
  missing (an unknown or erased account), and no final contest yet.
  Props: `id` (the account).
-->
<script lang="ts">
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import TrendLine from '../components/students/TrendLine.svelte';
  import { onMount } from 'svelte';
  import { ApiError, errorText, getJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime, scoreParts } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { percentText, trendPoints, trendSummary, type StudentDetail } from '../lib/students';

  let { id }: { id: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let data = $state.raw<StudentDetail | null>(null);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const name = $derived(data ? data.student.name || data.student.email : 'Student');
  const points = $derived(data ? trendPoints(data.contests) : []);

  async function load() {
    loadState = 'loading';
    try {
      const d = await getJSON<StudentDetail>(`/api/admin/students/${encodeURIComponent(id)}`);
      const restore = keepHeadingFocus();
      data = d;
      setTitle(d.student.name || d.student.email, routeTitle('student'));
      loadState = 'ready';
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
        setTitle('Student not found');
        return;
      }
      loadError = errorText(e);
      loadState = 'error';
    }
  }

  onMount(() => {
    setTitle(routeTitle('student'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
  });
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Student" />
  <PageState kind="forbidden" message="Only organisers can see a student's page." />
{:else if loadState !== 'ready' || !data}
  <PageHeader
    title={loadState === 'missing' ? 'Student not found' : 'Student'}
    crumbs={[{ label: 'Students', href: '/admin/students' }, { label: loadState === 'missing' ? 'Student not found' : 'Student' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the student" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no student with that number, or their account was erased." parent={{ href: '/admin/students', label: 'Go to the students.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <PageHeader title={name} crumbs={[{ label: 'Students', href: '/admin/students' }, { label: name }]} lead={data.student.email} />

  <div class="page-split student-split">
    <aside class="page-split-aside" aria-label="Record and progress">
      <section class="panel" aria-labelledby="more-title">
        <h2 id="more-title">More about {name}</h2>
        <ul class="links">
          <li>
            <a class="btn btn-secondary" href="/admin/students/{data.student.id}/record">Open the record</a>
            <p class="muted small">Standing, rating, cases, private notes and appeals. Each visit is logged for the owner.</p>
          </li>
          <li>
            <a class="btn btn-secondary" href="/admin/users/{data.student.id}/progress">See their progress page</a>
            <p class="muted small">The page exactly as the student sees it.</p>
          </li>
        </ul>
      </section>
    </aside>

    <div class="page-split-main">
      {#if data.contests.length === 0}
        <PageState kind="empty" message="No final contest yet. This page fills in once a contest they took part in has final results." />
      {:else}
        <section class="panel" aria-labelledby="trend-title">
          <h2 id="trend-title">Score over time</h2>
          {#if points.length >= 2}
            <TrendLine {points} summary={trendSummary(name, points)} />
          {:else}
            <p class="muted small">{trendSummary(name, points)} The chart appears after two graded contests.</p>
          {/if}
        </section>

        <section class="panel" aria-labelledby="contests-title">
          <h2 id="contests-title">Contests</h2>
          <div class="table-wrap">
            <table class="table table-dense table-stack contests">
              <caption class="visually-hidden">Contests {name} took part in, newest first</caption>
              <thead>
                <tr>
                  <th scope="col">Contest</th>
                  <th scope="col" class="right">Score</th>
                  <th scope="col" class="right">Class median</th>
                  <th scope="col" class="right">Place</th>
                  <th scope="col" class="right">Problems solved</th>
                  <th scope="col" class="right">Accepted</th>
                </tr>
              </thead>
              <tbody>
                {#each data.contests as c (c.contest_id)}
                  <tr>
                    <th scope="row" class="cname">
                      <a href="/admin/contests/{c.contest_id}/results">{c.title}</a>
                      <span class="sub muted"><MonoDigits text={`Ended ${formatDateTime(c.ended_at)}`} />{c.graded ? '' : ', not graded'}</span>
                    </th>
                    <td class="right score">
                      <span class="cell-label">Score</span><MonoDigits parts={scoreParts(c.score, c.max_score)} /><span class="sub muted"><span class="mono">{percentText(c.percent)}</span>%</span>
                    </td>
                    <td class="right median"><span class="cell-label">Class median</span>{#if c.class_median_percent !== null}<span class="mono">{percentText(c.class_median_percent)}</span>%{:else}<span class="muted">-</span>{/if}</td>
                    <td class="right place muted"><span class="cell-label">Place</span><MonoDigits text={`${c.rank} of ${c.ranked_count}`} /></td>
                    <td class="right solved"><span class="cell-label">Problems solved</span><MonoDigits text={`${c.problems_full} of ${c.problems_attempted} tried`} /></td>
                    <td class="right accepted"><span class="cell-label">Accepted</span><MonoDigits text={`${c.accepted} of ${c.submissions}`} /></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </section>
      {/if}
    </div>
  </div>
{/if}

<style>
  .student-split {
    margin-top: var(--space-6);
  }

  /* The aside comes first in the DOM, so a phone reaches the record link
     before the tables; from 1280 px it sits on the right. */
  @media (min-width: 1280px) {
    .student-split {
      grid-template-columns: minmax(0, 1fr) minmax(0, var(--measure-aside));
    }
  }

  .panel h2 {
    font-size: var(--fs-sub);
  }

  .links {
    display: grid;
    gap: var(--space-4);
    padding: 0;
    list-style: none;
  }

  .links li {
    display: grid;
    gap: var(--space-1);
    justify-items: start;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .cname {
    font-weight: 500;
  }

  .sub {
    display: block;
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .score,
  .median,
  .place,
  .solved,
  .accepted {
    white-space: nowrap;
  }

  .cell-label {
    display: none;
  }

  @media (width < 768px) {
    .contests tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'cname score'
        'median median'
        'place place'
        'solved solved'
        'accepted accepted';
    }

    .cname {
      grid-area: cname;
    }

    .score {
      grid-area: score;
    }

    .median {
      grid-area: median;
    }

    .place {
      grid-area: place;
    }

    .solved {
      grid-area: solved;
    }

    .accepted {
      grid-area: accepted;
    }

    .contests .median,
    .contests .place,
    .contests .solved,
    .contests .accepted {
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: left;
    }

    .cell-label {
      display: inline;
      margin-inline-end: var(--space-1);
    }
  }
</style>
