<!--
  ValidationReport: the result of the latest validation of a problem, as a
  plain list with one line per check (lib/problems.ts reportLines), failing
  tests named with the program and verdict, compiler output and checker
  messages verbatim in mono, and every test result in a table that opens on
  request. While the judge has the job it shows the queue position or
  "Running". A result for an older version of the problem says so. State
  changes are announced through a polite live region. Props: `validation`
  (the Validation from the API, or null when the problem was never
  validated).
-->
<script lang="ts">
  import { formatDateTime } from '../lib/format';
  import { isPending, progressText, reportLines, roleName, verdictLabel, type Validation } from '../lib/problems';

  let { validation }: { validation: Validation | null } = $props();

  const lines = $derived(validation ? reportLines(validation) : []);

  const headline = $derived.by(() => {
    if (!validation) return { text: '', tone: '' };
    switch (validation.state) {
      case 'passed':
        return { text: 'Validation passed', tone: 'ok' };
      case 'failed':
        return { text: 'Validation failed', tone: 'bad' };
      case 'error':
        return { text: 'Validation could not finish', tone: 'bad' };
      default:
        return { text: progressText(validation), tone: 'wait' };
    }
  });

  // Only the state is announced, so the queue position counting down does
  // not interrupt.
  const announcement = $derived.by(() => {
    if (!validation) return '';
    if (validation.state === 'queued') return 'Waiting for the judge';
    if (validation.state === 'running') return 'Running';
    return headline.text;
  });
</script>

<p class="visually-hidden" aria-live="polite">{announcement}</p>

{#if !validation}
  <p class="muted">
    Not validated yet. Validate runs the reference solution, brute force and validator on every test.
  </p>
{:else}
  <div class="report">
    <p class="headline tone-{headline.tone}">{headline.text}</p>

    {#if !validation.current}
      <p class="stale">These results are for an earlier version. Validate again.</p>
    {/if}

    {#if validation.state === 'error'}
      <p class="error-text">{validation.error ?? 'The judge stopped before it finished.'}</p>
      <p class="muted">When that is fixed, validate again.</p>
    {/if}

    {#if lines.length > 0}
      <ul class="lines">
        {#each lines as line, i (i)}
          <li class="line tone-{line.tone}">
            <span>{line.text}</span>
            {#if line.detail}<pre class="detail">{line.detail}</pre>{/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#if !isPending(validation) && validation.finished_at !== null}
      <p class="when muted">Finished {formatDateTime(validation.finished_at)}</p>
    {:else if isPending(validation)}
      <p class="when muted">Asked for {formatDateTime(validation.queued_at)}</p>
    {/if}

    {#if validation.tests.length > 0}
      <details class="all">
        <summary>All test results</summary>
        <div class="table-wrap results">
          <table class="table">
            <thead>
              <tr>
                <th scope="col">Test</th>
                <th scope="col">Program</th>
                <th scope="col">Verdict</th>
                <th scope="col">Time</th>
                <th scope="col">Memory</th>
              </tr>
            </thead>
            <tbody>
              {#each validation.tests as t, i (i)}
                <tr>
                  <td class="num">{t.idx}</td>
                  <td>{roleName(t.role)}</td>
                  <td class:bad={t.verdict !== 'AC' && t.verdict !== 'SK'}>{verdictLabel(t.verdict)}</td>
                  <td class="num">{t.time_ms} ms</td>
                  <td class="num">{Math.round(t.memory_kb / 1024)} MB</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </details>
    {/if}
  </div>
{/if}

<style>
  .report {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }

  .headline {
    font-size: var(--fs-2);
    font-weight: 600;
  }

  .tone-ok {
    color: var(--ok);
  }

  .tone-bad,
  .bad {
    color: var(--bad);
  }

  .tone-wait,
  .tone-note {
    color: var(--ink-muted);
  }

  .stale {
    padding: var(--space-2) var(--space-3);
    border: var(--rule) solid var(--part);
    background: var(--part-tint);
    color: var(--ink);
  }

  .error-text {
    color: var(--bad);
  }

  .lines {
    display: grid;
    gap: var(--space-2);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .line {
    min-width: 0;
    padding-block: var(--space-2) 0;
  }

  .line.tone-ok {
    color: var(--ink);
  }

  .detail {
    max-height: calc(20 * var(--lh-body) * var(--fs-0));
    margin-top: var(--space-2);
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
    color: var(--ink);
    font-size: var(--fs-0);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .when {
    font-size: var(--fs-0);
  }

  .all summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    cursor: pointer;
  }

  .results {
    margin-top: var(--space-3);
  }

  .results td {
    white-space: nowrap;
  }
</style>
