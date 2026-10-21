<!--
  ClassView: how the class did on each problem of a final contest, for
  organisers on the contest results (v2 P4 F9.4; GET
  /api/admin/contests/{id}/class). Totals only: never a name or an id.

  One row per problem: how many of the participants tried it and how many
  reached full marks, the median score, then each subtask as a meter (the
  share of participants who earned all of its points, a --act bar on a
  --line track with the percentage beside it in ink, the dataviz meter
  form: one series, one hue) and the test that was the first failure in
  the most participants' best submissions. In exam mode the server leaves
  the subtasks and the test out, and the row says so. Every meter has its
  numbers in words beside it, so nothing depends on the bar. Under 768 px
  each problem stacks. States: loading, "opens when the results are
  final" (409 not_final), error. Props: `contestId`.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, errorText, getJSON } from '../../lib/api';
  import { scoreParts } from '../../lib/format';
  import { percentText, type ClassView } from '../../lib/students';

  let { contestId }: { contestId: string } = $props();

  let view = $state<'loading' | 'ready' | 'not-final' | 'error'>('loading');
  let message = $state('');
  let data = $state.raw<ClassView | null>(null);

  async function load() {
    view = 'loading';
    try {
      data = await getJSON<ClassView>(`/api/admin/contests/${encodeURIComponent(contestId)}/class`);
      view = 'ready';
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        message = e.message || 'The class view opens when the results are final.';
        view = 'not-final';
        return;
      }
      message = errorText(e);
      view = 'error';
    }
  }

  onMount(() => void load());
</script>

{#if view === 'loading'}
  <p class="muted small" role="status">Loading the class view</p>
{:else if view === 'not-final'}
  <p class="muted small">{message}</p>
{:else if view === 'error'}
  <p class="field-error">{message} <button type="button" class="btn btn-quiet btn-compact" onclick={load}>Try again</button></p>
{:else if data}
  <p class="muted small"><MonoDigits text={`${data.participants} ${data.participants === 1 ? 'participant' : 'participants'}. Totals only, no names.`} /></p>
  {#if data.problems.length === 0}
    <p class="muted small">This contest has no problems.</p>
  {:else}
    <div class="table-wrap">
      <table class="table table-dense table-stack klass">
        <caption class="visually-hidden">How the class did on each problem</caption>
        <thead>
          <tr>
            <th scope="col">Problem</th>
            <th scope="col" class="right">Tried</th>
            <th scope="col" class="right">Full marks</th>
            <th scope="col" class="right">Median score</th>
            <th scope="col">Subtasks solved</th>
          </tr>
        </thead>
        <tbody>
          {#each data.problems as p (p.label)}
            <tr>
              <th scope="row" class="pname"><span class="mono letter">{p.label}</span>{p.title}</th>
              <td class="right tried"><span class="cell-label">Tried</span><MonoDigits text={`${p.attempted} of ${data.participants}`} /></td>
              <td class="right full"><span class="cell-label">Full marks</span><span class="mono">{p.full}</span></td>
              <td class="right median"><span class="cell-label">Median score</span><MonoDigits parts={scoreParts(p.median_score, p.max_score)} /></td>
              <td class="subs">
                {#if p.subtasks && p.subtasks.length > 0}
                  <ul class="meters">
                    {#each p.subtasks as st (st.idx)}
                      <li>
                        <span class="meter-label">Subtask <span class="mono">{st.idx + 1}</span>, <span class="mono">{st.points}</span> pt</span>
                        <span class="track" aria-hidden="true"><span class="fill" style:width="{Math.min(100, Math.max(0, st.solve_rate))}%"></span></span>
                        <span class="meter-value"><span class="mono">{percentText(st.solve_rate)}</span>%<span class="visually-hidden"> of participants, {st.solved} students</span></span>
                      </li>
                    {/each}
                  </ul>
                  {#if p.most_failed_test}
                    <p class="muted small fail">
                      <MonoDigits
                        text={`Most often the first failure: test ${p.most_failed_test.test} of subtask ${p.most_failed_test.subtask_idx + 1}, for ${p.most_failed_test.failed} ${p.most_failed_test.failed === 1 ? 'student' : 'students'}.`}
                      />
                    </p>
                  {/if}
                {:else}
                  <span class="muted small">Totals only: an exam-mode contest shows no subtasks.</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
{/if}

<style>
  .small {
    font-size: var(--fs-0);
  }

  .right,
  .table .right {
    text-align: right;
  }

  .pname {
    font-weight: 500;
  }

  .letter {
    margin-right: var(--space-3);
    color: var(--ink-muted);
    font-weight: 400;
  }

  .tried,
  .full,
  .median {
    white-space: nowrap;
  }

  .subs {
    min-width: 18rem;
    padding-block: var(--space-2);
  }

  .meters {
    display: grid;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
    font-size: var(--fs-0);
  }

  .meters li {
    display: grid;
    grid-template-columns: 8.5rem minmax(4rem, 1fr) 3.5rem;
    align-items: center;
    gap: var(--space-2);
  }

  .meter-label {
    white-space: nowrap;
  }

  .track {
    display: block;
    height: var(--space-2);
    background: var(--line);
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--act);
  }

  .meter-value {
    text-align: right;
    white-space: nowrap;
  }

  .fail {
    margin-top: var(--space-2);
  }

  .cell-label {
    display: none;
  }

  @media (forced-colors: active) {
    .track {
      border: var(--rule) solid CanvasText;
      background: Canvas;
    }

    .fill {
      background: Highlight;
      forced-color-adjust: none;
    }
  }

  @media (width < 768px) {
    .klass tbody tr {
      grid-template-columns: repeat(3, minmax(0, auto));
      grid-template-areas:
        'pname pname pname'
        'tried full median'
        'subs subs subs';
      justify-content: start;
      gap: var(--space-2) var(--space-4);
    }

    .pname {
      grid-area: pname;
    }

    .tried {
      grid-area: tried;
    }

    .full {
      grid-area: full;
    }

    .median {
      grid-area: median;
    }

    .subs {
      grid-area: subs;
      min-width: 0;
    }

    .klass tbody :is(th, td) {
      text-align: left;
    }

    .meters li {
      grid-template-columns: 7.5rem minmax(3rem, 1fr) 3.5rem;
    }

    .cell-label {
      display: inline;
      margin-right: var(--space-1);
      color: var(--ink-muted);
      font-family: var(--font-ui);
    }
  }
</style>
