<!--
  Feed: the console's submissions feed, filtered by problem, student and
  verdict in pages of 50, with the time of the rows shown; after a failed
  refresh the last rows stay with a line saying so. Outcomes read as
  students read them and times are minutes from the start. When an exam
  option is on, a Recorded column gives what the editor counted before
  each submission. Each row offers "Record a decision" and "Judge again",
  handed to the page, which asks first; the page calls `load()` to
  refresh and `showErrors()` to show only judge errors.
-->
<script lang="ts">
  import Badge from '../Badge.svelte';
  import Icon from '../Icon.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, errorText, getJSON } from '../../lib/api';
  import { serverClock } from '../../lib/clock';
  import { anyExamOption, feedOutcome, recordedText, sinceStart, type ExamOptions, type FeedPage, type FeedRow } from '../../lib/contests';
  import { verdictLabel } from '../../lib/problems';
  import { ticker } from '../../lib/ticker.svelte';
  import { formatWhen, languageLabel } from '../../lib/workspace';

  interface Props {
    base: string;
    startsAt: number;
    labels: readonly string[];
    students: readonly { user_id: number; name: string }[];
    exam?: ExamOptions;
    autoRefresh: boolean;
    onrejudge: (row: FeedRow) => void;
    ondecide: (row: FeedRow) => void;
  }

  let { base, startsAt, labels, students, exam = {}, autoRefresh = $bindable(), onrejudge, ondecide }: Props = $props();

  const recorded = $derived(anyExamOption(exam));

  const VERDICTS = ['AC', 'WA', 'TLE', 'MLE', 'RE', 'CE'];

  let feed = $state<FeedRow[]>([]);
  let feedState = $state<'loading' | 'ready' | 'error'>('loading');
  let feedError = $state('');
  let hasMore = $state(false);
  let page = $state(1);
  let fProblem = $state('');
  let fUser = $state('');
  let fVerdict = $state('');
  /** When the shown rows arrived, and when a later refresh failed. */
  let feedAt = $state(0);
  let feedFailedAt = $state(0);

  // Only the newest request is shown, so a slow answer for an old filter cannot land under a new one.
  let seq = 0;

  /** "14:02:10" in local time. */
  function clock(ms: number): string {
    const d = new Date(ms);
    return [d.getHours(), d.getMinutes(), d.getSeconds()].map((n) => String(n).padStart(2, '0')).join(':');
  }

  export async function load() {
    const mine = ++seq;
    const params: [string, string][] = [];
    if (fProblem) params.push(['problem', fProblem]);
    if (fUser !== '') params.push(['user', fUser]);
    if (fVerdict) params.push(['verdict', fVerdict]);
    params.push(['page', String(page)]);
    const q = params.map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&');
    try {
      const data = await getJSON<FeedPage>(`${base}/submissions?${q}`);
      if (mine !== seq) return;
      feed = data.submissions ?? [];
      hasMore = data.has_more ?? false;
      feedState = 'ready';
      feedAt = serverClock.serverNow();
      feedFailedAt = 0;
    } catch (e) {
      if (mine !== seq) return;
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) return;
      // A failed refresh keeps the rows already shown.
      if (feedState === 'ready') {
        feedFailedAt = serverClock.serverNow();
        return;
      }
      feedError = errorText(e);
      feedState = 'error';
    }
  }

  /** Shows only the submissions that ended in a judge error (from the banner). */
  export function showErrors() {
    fVerdict = 'error';
    page = 1;
    void load();
    document.getElementById('submissions-title')?.scrollIntoView({ block: 'start' });
  }

  function applyFilters(event: SubmitEvent) {
    event.preventDefault();
    page = 1;
    void load();
  }
</script>

<section class="panel" aria-labelledby="submissions-title">
  <div class="panel-head">
    <h2 id="submissions-title">Submissions</h2>
    <label class="choice"><input type="checkbox" bind:checked={autoRefresh} /> Refresh every 10 s</label>
  </div>
  {#if feedAt > 0}
    {#if feedFailedAt > 0}
      <p class="small refresh-failed" role="status"><MonoDigits text={`Could not refresh at ${clock(feedFailedAt)}. Showing ${clock(feedAt)}. Retrying.`} /></p>
    {:else}
      <p class="small muted"><MonoDigits text={`Updated ${clock(feedAt)}`} /></p>
    {/if}
  {/if}
  <form class="row filters" novalidate onsubmit={applyFilters}>
    <div class="field">
      <label for="f-problem">Problem</label>
      <select id="f-problem" class="select" bind:value={fProblem}>
        <option value="">All</option>
        {#each labels as l (l)}<option value={l}>{l}</option>{/each}
      </select>
    </div>
    <div class="field grow">
      <label for="f-user">Student</label>
      <select id="f-user" class="select" bind:value={fUser}>
        <option value="">Everyone</option>
        {#each students as s (s.user_id)}<option value={String(s.user_id)}>{s.name}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="f-verdict">Verdict</label>
      <select id="f-verdict" class="select" bind:value={fVerdict}>
        <option value="">All</option>
        <option value="pending">Waiting or judging</option>
        <option value="error">Judge error</option>
        {#each VERDICTS as v (v)}<option value={v}>{verdictLabel(v)}</option>{/each}
      </select>
    </div>
    <button type="submit" class="btn btn-secondary">Filter</button>
    <button type="button" class="btn btn-quiet" onclick={() => load()}><Icon name="refresh-cw" /> Refresh</button>
  </form>

  {#if feedState === 'loading'}
    <p class="muted" role="status">Loading submissions</p>
  {:else if feedState === 'error'}
    <div class="banner banner-error" role="alert"><p>{feedError}</p></div>
  {:else if feed.length === 0}
    <p class="muted">No submissions match. They appear here as students submit.</p>
  {:else}
    <div class="table-wrap feed">
      <table class="table table-dense table-stack">
        <thead>
          <tr>
            <th scope="col">Time</th>
            <th scope="col">Student</th>
            <th scope="col">Problem</th>
            <th scope="col">Language</th>
            <th scope="col">Outcome</th>
            {#if recorded}<th scope="col">Recorded</th>{/if}
            <th scope="col"><span class="visually-hidden">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each feed as row (row.id)}
            {@const out = feedOutcome(row)}
            {@const since = sinceStart(row.created_at, startsAt)}
            <tr>
              <td class="when" title={formatWhen(row.created_at, ticker.now)}><MonoDigits text={since ?? formatWhen(row.created_at, ticker.now)} /></td>
              <th scope="row" class="who">
                {row.name}{#if row.practice}<span class="muted"> (practice)</span>{/if}
              </th>
              <td class="mono plabel">{row.problem_label}</td>
              <td class="lang">{languageLabel(row.language)}</td>
              <td class="verdict"><Badge tone={out.tone}><MonoDigits text={out.text} /></Badge></td>
              {#if recorded}<td class="recorded small"><MonoDigits text={recordedText(row, exam)} /></td>{/if}
              <td class="act">
                <button type="button" class="btn btn-quiet btn-compact again" aria-label="Record a decision about {row.name}" onclick={() => ondecide(row)}
                  >Record a decision</button
                >
                <button
                  type="button"
                  class="btn btn-quiet btn-compact again"
                  aria-label="Judge again: {row.name}, problem {row.problem_label}, {formatWhen(row.created_at, ticker.now)}"
                  onclick={() => onrejudge(row)}>Judge again</button
                >
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <div class="pager">
      <button
        type="button"
        class="btn btn-secondary"
        disabled={page <= 1}
        title={page <= 1 ? 'This is the newest page.' : undefined}
        onclick={() => {
          page -= 1;
          void load();
        }}>Newer</button
      >
      <span class="muted small">Page <span class="mono">{page}</span></span>
      <button
        type="button"
        class="btn btn-secondary"
        disabled={!hasMore}
        title={hasMore ? undefined : 'This is the oldest page.'}
        onclick={() => {
          page += 1;
          void load();
        }}>Older</button
      >
    </div>
  {/if}
</section>

<style>
  .panel h2 {
    font-size: var(--fs-section);
  }

  .refresh-failed {
    color: var(--part);
    font-weight: 600;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
  }

  .grow {
    flex: 1 1 12rem;
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-size: var(--fs-0);
    cursor: pointer;
  }

  .choice input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .who {
    font-weight: 500;
    white-space: nowrap;
  }

  .act,
  .lang,
  .when,
  .verdict {
    white-space: nowrap;
  }

  /* The row actions are quiet in every row and full strength only on the row a pointer or focus is on. */
  .again {
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .feed tbody tr:hover .again,
  .again:focus-visible {
    color: var(--ink);
  }

  .recorded {
    color: var(--ink-muted);
  }

  .pager {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  /* Phones: each row is time, student and problem, then the outcome and the actions. */
  @media (width < 768px) {
    .feed tbody tr {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'when who plabel'
        'verdict verdict verdict'
        'recorded recorded recorded'
        'act act act';
      align-items: center;
      padding: var(--space-2) var(--space-3);
    }

    .feed .when {
      grid-area: when;
    }

    .feed .who {
      grid-area: who;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .feed .plabel {
      grid-area: plabel;
      text-align: right;
    }

    .feed .lang {
      display: none;
    }

    .feed .verdict {
      grid-area: verdict;
    }

    .feed .recorded {
      grid-area: recorded;
    }

    .feed .act {
      grid-area: act;
      justify-self: end;
    }
  }
</style>
