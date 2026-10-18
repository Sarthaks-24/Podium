<!--
  ValidationReport: the result of the latest validation of a problem
  (lib/problems.ts buildReport). A failed run leads with one sentence
  naming what to fix first. Then each program that has something to say
  gets its own group with a link that moves focus to its source: the
  summary line, a compile failure with the compiler's output verbatim, and
  each failing test in verdict wording ("Wrong answer on test 7") with the
  checker's message and one line of guidance. Only failing lines are red.
  The server's problems and warnings follow, and every test result sits in
  a table that opens on request. While the judge has the job it shows the
  queue position or "Running". A result for an older version of the
  problem says so. State changes are announced through a polite live
  region. Props: `validation` (the Validation from the API, or null when
  the problem was never validated).
-->
<script lang="ts">
  import { formatDateTime } from '../lib/format';
  import { buildReport, isPending, programFieldId, progressText, roleName, verdictLabel, type Role, type Validation } from '../lib/problems';

  let { validation }: { validation: Validation | null } = $props();

  const report = $derived(validation ? buildReport(validation) : null);

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

  function goTo(event: MouseEvent, role: Role) {
    const el = document.getElementById(programFieldId(role));
    if (!el) return;
    event.preventDefault();
    el.scrollIntoView({ block: 'center' });
    el.focus({ preventScroll: true });
  }
</script>

<p class="visually-hidden" aria-live="polite">{announcement}</p>

{#if !validation || !report}
  <p class="muted">
    Not validated yet. Validate runs the reference solution, brute force and validator on every test.
  </p>
{:else}
  <div class="report">
    <p class="headline tone-{headline.tone}">{headline.text}</p>
    {#if report.lead}<p class="lead">{report.lead}</p>{/if}

    {#if !validation.current}
      <p class="stale">These results are for an earlier version. Validate again.</p>
    {/if}

    {#if validation.state === 'error'}
      <p class="error-text">{validation.error ?? 'The judge stopped before it finished.'}</p>
      <p class="muted">When that is fixed, validate again.</p>
    {/if}

    {#each report.sections as section (section.role)}
      <section class="program" aria-label={section.title}>
        <div class="program-head">
          <h3 class="program-title">{section.title}</h3>
          <a class="go" href="#{programFieldId(section.role)}" onclick={(e) => goTo(e, section.role)}>Go to {roleName(section.role)}</a>
        </div>
        <ul class="lines">
          {#each section.lines as line, i (i)}
            <li class="line tone-{line.tone}">
              <span>{line.text}</span>
              {#if line.detail}<pre class="detail">{line.detail}</pre>{/if}
              {#if line.guidance}<p class="guidance">{line.guidance}</p>{/if}
            </li>
          {/each}
        </ul>
      </section>
    {/each}

    {#if report.notes.length > 0}
      <ul class="lines notes">
        {#each report.notes as line, i (i)}
          <li class="line tone-{line.tone}">{line.text}</li>
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
    gap: var(--space-4);
    min-width: 0;
  }

  .headline {
    font-size: var(--fs-2);
    font-weight: 600;
  }

  .lead {
    max-width: 60ch;
    font-weight: 500;
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

  .program {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }

  .program-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-4);
  }

  .program-title {
    font-size: var(--fs-1);
  }

  .go {
    border-radius: var(--radius);
    font-size: var(--fs-0);
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

  /* Passing lines are plain ink; only failures are red. */
  .line.tone-ok {
    color: var(--ink);
  }

  .guidance {
    margin-top: var(--space-1);
    color: var(--ink-muted);
    font-size: var(--fs-0);
    max-width: 72ch;
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
