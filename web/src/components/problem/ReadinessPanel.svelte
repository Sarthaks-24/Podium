<!--
  ReadinessPanel: what the problem needs before students can have it (v2
  P3 F5.2, A1; audit 2026-10-20 O2). It sits in the editor's sticky
  context column from 1280 px and above the form below that. In order:
  the status as a badge; whether the validation covers this version, or
  which unsaved judged changes will need a new one (lib/problems
  judgedFieldsChanged); the chain after Generate inputs while it runs; the
  last result in one line with "See the report", which moves focus to the
  report; what stops validation (the server's `problems`); the warnings
  ("Before you validate"), with a line for statement images that have no
  description; and once validated, "Choose a contest" ("Try as a student"
  is in the page header). A plain list, not tiles.

  Props: `problem`, `status` (lib/problems editorStatus), `judged` (the
  judged fields changed and not saved), `dirty`, `pipeline` (the chain
  line, "" when idle), `result` (validationResultLine), `altMissing` (how
  many statement images have no description) and `onreport`, which shows
  the full report.
-->
<script lang="ts">
  import Badge from '../Badge.svelte';
  import type { BadgeTone } from '../../lib/badges';
  import { wordList, type Problem } from '../../lib/problems';

  interface Props {
    problem: Pick<Problem, 'id' | 'validated' | 'archived' | 'warnings' | 'problems'>;
    status: { tone: 'ok' | 'bad' | 'wait' | 'note'; text: string } | null;
    judged: string[];
    dirty: boolean;
    pipeline: string;
    result: { tone: 'ok' | 'bad'; text: string } | null;
    altMissing: number;
    onreport: (event: MouseEvent) => void;
  }

  let { problem, status, judged, dirty, pipeline, result, altMissing, onreport }: Props = $props();

  const TONES: Record<string, BadgeTone> = { ok: 'ok', bad: 'bad', wait: 'wait', note: 'part' };

  const blockers = $derived(problem.problems ?? []);
  const warnings = $derived(
    altMissing > 0
      ? [
          ...problem.warnings,
          `${altMissing === 1 ? 'An image' : `${altMissing} images`} in the statement ${altMissing === 1 ? 'has' : 'have'} no description. Write one between the square brackets, as in ![A graph of the input](graph.png), so screen readers can read ${altMissing === 1 ? 'it' : 'them'}.`,
        ]
      : problem.warnings,
  );
  const ready = $derived(problem.validated && judged.length === 0 && !problem.archived);

  const version = $derived.by(() => {
    if (judged.length > 0) {
      return {
        tone: 'part',
        text: problem.validated
          ? `Saving needs a new validation: you changed the ${wordList(judged)}.`
          : `Your changes to the ${wordList(judged)} are not saved yet.`,
      };
    }
    if (problem.validated) return { tone: 'ok', text: dirty ? 'Validated on this version. Your unsaved changes do not affect it.' : 'Validated on this version.' };
    return null;
  });
</script>

<section class="panel readiness" aria-labelledby="readiness-heading">
  <div class="panel-head">
    <h2 id="readiness-heading">Readiness</h2>
    {#if status}<Badge tone={TONES[status.tone]} text={status.text} />{/if}
  </div>

  {#if version}<p class="line tone-{version.tone}">{version.text}</p>{/if}
  {#if pipeline}<p class="line pipeline">{pipeline}</p>{/if}
  {#if result}
    <p class="line">
      <span class="tone-{result.tone} result">{result.text}</span>
      <a href="#validation-heading" onclick={onreport}>See the report</a>
    </p>
  {/if}

  {#if blockers.length > 0}
    <div class="group" role="group" aria-labelledby="blockers-title">
      <h3 id="blockers-title" class="group-title tone-bad">Fix {blockers.length === 1 ? 'this' : 'these'} before you validate</h3>
      <ul>{#each blockers as line, i (i)}<li>{line}</li>{/each}</ul>
    </div>
  {/if}

  {#if warnings.length > 0}
    <div class="group" role="note" aria-labelledby="warnings-title">
      <h3 id="warnings-title" class="group-title">Before you validate</h3>
      <ul>{#each warnings as warning, i (i)}<li>{warning}</li>{/each}</ul>
    </div>
  {/if}

  {#if ready}
    <div class="action-row">
      <a class="btn btn-secondary" href="/admin/contests">Choose a contest</a>
    </div>
  {:else if !problem.validated && blockers.length === 0 && !pipeline && !result}
    <p class="line muted">Validate runs the reference solution, brute force and validator on every test. A contest takes the problem once it passes.</p>
  {/if}
</section>

<style>
  .readiness h2 {
    font-size: var(--fs-sub);
  }

  .line {
    max-width: var(--measure-prose);
  }

  .result {
    font-weight: 500;
  }

  .tone-ok {
    color: var(--ok);
  }

  .tone-part {
    color: var(--part);
    font-weight: 500;
  }

  .tone-bad {
    color: var(--bad);
  }

  .pipeline {
    color: var(--ink-muted);
  }

  .group {
    display: grid;
    gap: var(--space-1);
  }

  .group-title {
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .group ul {
    padding-left: var(--space-5);
  }
</style>
