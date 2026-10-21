/**
 * Pure helpers of the problem editor route, moved out of ProblemEditor.svelte
 * to keep the route near the P3 gate's 700 lines (council run 2, C2-053):
 * the one-line summary of an output generation and the jump links.
 */
import { isFailure, verdictLabel, type Draft, type Problem } from './problems';
import { formatMs, type Generation } from './workspace';

/** What the Tests section says about the last output generation, or null before any. */
export function generationSummary(g: Generation | null): { tone: string; text: string; lines: string[] } | null {
  if (!g) return null;
  if (g.state === 'queued' || g.state === 'running') return { tone: 'wait', text: 'Generating outputs', lines: [] };
  if (g.error) return { tone: 'bad', text: g.error, lines: [] };
  if (g.results.length === 0 && g.compile_output) {
    return {
      tone: 'bad',
      text: 'The reference solution did not compile, so no outputs were generated.',
      lines: g.compile_output.split('\n').filter((l) => l.trim() !== '').slice(0, 8),
    };
  }
  const problems = g.summary?.problems ?? [];
  if (problems.length > 0) return { tone: 'bad', text: 'Some outputs were not applied.', lines: problems };
  const failed = g.results.filter((r) => isFailure(r.verdict));
  if (failed.length > 0) {
    return {
      tone: 'bad',
      text: `The reference solution failed on ${failed.length} ${failed.length === 1 ? 'test' : 'tests'}, so those outputs are still missing.`,
      lines: failed.map((r) => `${verdictLabel(r.verdict)} on test ${r.idx}`),
    };
  }
  const slowest = g.results.reduce((m, r) => Math.max(m, r.time_ms), 0);
  const n = g.summary?.applied ?? g.results.length;
  return { tone: 'ok', text: `Generated ${n} ${n === 1 ? 'output' : 'outputs'}. Slowest run ${formatMs(slowest)}.`, lines: [] };
}

/** The editor's jump links, each with a short state (A3). */
export function editorJumps(problem: Problem | null, draft: Draft | null, hasGenerator: boolean): { id: string; label: string; state: string }[] {
  if (!problem || !draft) return [];
  const n = problem.tests.length;
  const noOutput = problem.tests.filter((t) => t.output_missing).length;
  const list = [
    { id: 'details-heading', label: 'Statement', state: '' },
    { id: 'solutions-heading', label: 'Solutions', state: draft.brute ? 'with brute force' : 'reference only' },
    { id: 'tests-heading', label: 'Tests', state: n === 0 ? 'none' : noOutput > 0 ? `${noOutput} without output` : String(n) },
  ];
  if (hasGenerator) list.push({ id: 'generator-heading', label: 'Generator', state: draft.generator ? '' : 'none' });
  list.push({ id: 'judging-heading', label: 'Judging', state: draft.validator ? '' : 'no validator' });
  list.push({ id: 'validation-heading', label: 'Validation', state: '' });
  list.push({ id: 'history-heading', label: 'History', state: '' });
  return list;
}
