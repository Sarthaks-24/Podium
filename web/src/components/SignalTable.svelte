<!--
  SignalTable: integrity signals as a table (docs/INTEGRITY.md): the
  students named, the kind in words, the strength in words, for similar
  code with its reason under it ("Short programs, identical", never a bare
  "Weak 1.00", which read as proof), what was found, and for similar code the
  numbers behind it (fingerprints, tokens, score) and a link to the
  side-by-side view. Under a table with similar code, one line says what
  the strength weighs. With `selectable` each row starts with a checkbox for
  choosing signals to open a case from. Under 768 px each row reads as the
  students, then the kind and strength, then the details. Props: `signals`,
  `users` and `problems` (the run's maps), `contestId`, `caption`,
  `showProblem` (adds a problem column, for a case's evidence), `caseId`
  (the rows are a case's evidence: links open the case's own copy, as
  ?case={caseId}&evidence={row}), `selectable`, `selected` (chosen signal
  ids) and `ontoggle`.
-->
<script lang="ts">
  import {
    STRENGTH_MEANING,
    kindWord,
    problemName,
    signalDetail,
    signalStudents,
    similarNumbers,
    similarReason,
    strengthWord,
    type ReviewProblem,
    type ReviewUser,
    type Signal,
  } from '../lib/review';

  interface Props {
    signals: Signal[];
    users: Record<string, ReviewUser> | undefined;
    problems: Record<string, ReviewProblem> | undefined;
    contestId: string | number;
    caption: string;
    showProblem?: boolean;
    caseId?: number;
    selectable?: boolean;
    selected?: ReadonlySet<number>;
    ontoggle?: (id: number, on: boolean) => void;
  }

  let { signals, users, problems, contestId, caption, showProblem = false, caseId, selectable = false, selected = new Set(), ontoggle }: Props = $props();

  const base = $derived(`/admin/contests/${encodeURIComponent(String(contestId))}/integrity/signals`);
  const anySimilar = $derived(signals.some((s) => s.kind === 'similar'));

  function href(s: Signal, row: number): string {
    return caseId === undefined ? `${base}/${s.id}` : `${base}/${s.id}?case=${caseId}&evidence=${row}`;
  }
</script>

<div class="table-wrap signals" class:selectable>
  <table class="table table-dense table-stack">
    <caption class="visually-hidden">{caption}</caption>
    <thead>
      <tr>
        {#if selectable}<th scope="col" class="pick"><span class="visually-hidden">Choose</span></th>{/if}
        <th scope="col" class="c-who">Students</th>
        {#if showProblem}<th scope="col" class="c-prob">Problem</th>{/if}
        <th scope="col" class="c-kind">Kind</th>
        <th scope="col" class="c-strength">Strength</th>
        <th scope="col">What was found</th>
        <th scope="col"><span class="visually-hidden">Code</span></th>
      </tr>
    </thead>
    <tbody>
      {#each signals as s, row (s.id)}
        {@const who = signalStudents(s, users)}
        {@const size = s.kind === 'similar' ? similarNumbers(s.score, s.evidence) : ''}
        <tr class:chosen={selected.has(s.id)}>
          {#if selectable}
            <td class="pick">
              <label class="hit">
                <input
                  type="checkbox"
                  aria-label="Choose {kindWord(s.kind).toLowerCase()}, {who}"
                  checked={selected.has(s.id)}
                  onchange={(e) => ontoggle?.(s.id, e.currentTarget.checked)}
                />
              </label>
            </td>
          {/if}
          <th scope="row" class="who">{who}</th>
          {#if showProblem}<td class="prob">{problemName(problems, s.problem_id)}</td>{/if}
          <td class="kind">{kindWord(s.kind)}</td>
          <td class="strength"
            ><span class="word word-{s.strength}">{strengthWord(s.strength)}</span>{#if s.kind === 'similar'}<span class="reason"
                ><span class="visually-hidden">:</span> {similarReason(s.score, s.evidence)}</span
              >{/if}</td
          >
          <td class="detail">{signalDetail(s)}{#if size}<span class="size"><span class="visually-hidden">. </span>{size}</span>{/if}</td>
          <td class="link">
            {#if s.kind === 'similar'}
              <a href={href(s, row)}>Compare code<span class="visually-hidden"> of {who}</span></a>
            {:else}
              <a href={href(s, row)}>Timelines<span class="visually-hidden"> of {who}</span></a>
            {/if}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
{#if anySimilar}
  <p class="meaning">{STRENGTH_MEANING}</p>
{/if}

<style>
  .pick {
    width: var(--space-7);
  }

  .hit {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: var(--control-h);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .pick input {
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
    cursor: pointer;
  }

  .chosen > * {
    background: var(--act-tint);
  }

  .who {
    font-weight: 500;
  }

  .reason {
    display: block;
    color: var(--ink-muted);
  }

  .word-strong {
    font-weight: 600;
  }

  .word-weak {
    color: var(--ink-muted);
  }

  .link {
    white-space: nowrap;
  }

  .size {
    display: block;
    color: var(--ink-muted);
  }

  .meaning {
    margin-top: var(--space-2);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  /* Fixed shares, so the tables of every problem line up. */
  @media (min-width: 768px) {
    .c-who {
      width: 20%;
    }

    .c-prob {
      width: 16%;
    }

    .c-kind {
      width: 13%;
    }

    .c-strength {
      width: 20%;
    }
  }

  /* Phones: students first, then kind and strength, then what was found. */
  @media (width < 768px) {
    .signals tbody tr {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'pick who who'
        'pick kind strength'
        'pick prob prob'
        'pick detail detail'
        'pick link link';
    }

    .signals:not(.selectable) tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'who who'
        'kind strength'
        'prob prob'
        'detail detail'
        'link link';
    }

    .pick {
      grid-area: pick;
      width: auto;
    }

    .who {
      grid-area: who;
    }

    .kind {
      grid-area: kind;
    }

    .strength {
      grid-area: strength;
      text-align: right;
    }

    .prob {
      grid-area: prob;
    }

    .detail {
      grid-area: detail;
      color: var(--ink-muted);
    }

    .link {
      grid-area: link;
    }

    .link a {
      display: inline-flex;
      align-items: center;
      min-height: var(--control-h-touch);
    }
  }
</style>
