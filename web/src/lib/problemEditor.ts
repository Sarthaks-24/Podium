/**
 * Pure helpers of the problem editor route, kept out of ProblemEditor.svelte
 * so the route stays readable: the one status line of the title row, the
 * one-line summary of an output generation, and the tests table's roles.
 *
 * A test's role (Example, Subtask 1, Subtask 2, ..., Not in a subtask) is
 * chosen per row, but the server keeps the old shape: a list of example
 * (sample) test numbers and a range per subtask, such as "3-5, 8". The
 * draft holds those lists as text, so setRole() rewrites them and a save
 * sends the same body as before. A subtask's tests need not be contiguous:
 * the server reads any list of numbers and ranges.
 */
import { formatTestList, isFailure, isPending, ordinal, parseTestList, rangeText, verdictLabel, type Draft, type Problem, type Validation } from './problems';
import { formatMs, type Generation } from './workspace';

/** What the Tests tab says about the last output generation, or null before any. */
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

/**
 * The title row's one status line: "Validated, 7 tests, slowest 286 ms",
 * "Not validated, 5 tests", "Validating, 2nd in queue", "Making outputs,
 * then validating", "Validation failed on test 7" or "Archived".
 */
export function statusLine(
  p: Pick<Problem, 'archived' | 'validated' | 'content_hash' | 'tests'>,
  v: Validation | null,
  makingOutputs: boolean,
): { tone: 'ok' | 'bad' | 'wait' | 'note'; text: string } {
  if (p.archived) return { tone: 'note', text: 'Archived' };
  if (makingOutputs) return { tone: 'wait', text: 'Making outputs, then validating' };
  if (v && isPending(v)) {
    const pos = v.state === 'queued' && v.queue_position !== null && v.queue_position > 0 ? v.queue_position : 0;
    return { tone: 'wait', text: pos > 0 ? `Validating, ${ordinal(pos)} in queue` : 'Validating' };
  }
  const n = p.tests.length;
  const count = n === 0 ? 'no tests' : `${n} ${n === 1 ? 'test' : 'tests'}`;
  const current = v !== null && v.content_hash === p.content_hash;
  if (current && (v.state === 'failed' || v.state === 'error')) {
    if (v.state === 'error') return { tone: 'bad', text: 'Validation could not finish' };
    const first = v.tests.filter((t) => isFailure(t.verdict)).sort((a, b) => a.idx - b.idx)[0];
    if (first) return { tone: 'bad', text: `Validation failed on test ${first.idx}` };
    if (v.compile.some((c) => c.verdict === 'CE')) return { tone: 'bad', text: 'Validation failed: a program did not compile' };
    return { tone: 'bad', text: 'Validation failed' };
  }
  if (p.validated) {
    const slowest = current && v.state === 'passed' && v.summary ? `, slowest ${v.summary.max_time_ms} ms` : '';
    return { tone: 'ok', text: `Validated, ${count}${slowest}` };
  }
  return { tone: 'note', text: `Not validated, ${count}` };
}

/** A test's role: an example, a subtask counted from 1, or none. */
export type TestRole = 'example' | number | 'none';

/** The role a test has in the draft as it stands. */
export function roleOf(idx: number, d: Pick<Draft, 'samples' | 'subtasks'>): TestRole {
  const samples = parseTestList(d.samples);
  if (samples.ok && samples.tests.includes(idx)) return 'example';
  const i = d.subtasks.findIndex((s) => {
    const r = parseTestList(s.tests);
    return r.ok && r.tests.includes(idx);
  });
  return i >= 0 ? i + 1 : 'none';
}

/**
 * Gives a test one role: it leaves the examples and every subtask, then
 * joins the one chosen. The lists are written as toDraft writes them
 * (examples "1, 2", subtasks "3-5, 8"), so moving a test back is no change.
 */
export function setRole(d: Pick<Draft, 'samples' | 'subtasks'>, idx: number, role: TestRole): void {
  const without = (text: string) => {
    const r = parseTestList(text);
    return r.ok ? r.tests.filter((n) => n !== idx) : [];
  };
  const samples = without(d.samples);
  if (role === 'example') samples.push(idx);
  d.samples = formatTestList(samples);
  d.subtasks.forEach((s, i) => {
    const tests = without(s.tests);
    if (role === i + 1) tests.push(idx);
    s.tests = rangeText(tests);
  });
}

/** The value of a role select, and back: "example", "none" or the subtask number. */
export function roleValue(role: TestRole): string {
  return String(role);
}

export function parseRole(value: string): TestRole {
  if (value === 'example' || value === 'none') return value;
  return Number(value);
}

/** The tests of each subtask counted from the draft, for the subtask list. */
export function subtaskCounts(tests: readonly { idx: number }[], d: Pick<Draft, 'samples' | 'subtasks'>): number[] {
  const counts = d.subtasks.map(() => 0);
  for (const t of tests) {
    const r = roleOf(t.idx, d);
    if (typeof r === 'number' && r <= counts.length) counts[r - 1]++;
  }
  return counts;
}
