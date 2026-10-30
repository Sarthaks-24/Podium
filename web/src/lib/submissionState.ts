/**
 * Turns what the event stream and the API say about runs and submissions
 * into the workspace's state.
 *
 * Every event carries the full object, so merging is replacing, with two
 * guards: events for another problem are ignored (the stream is per user,
 * not per problem), and an older picture never overwrites a newer one, so
 * an event that arrives late after a refetch cannot move a finished
 * submission back to "Judging".
 */
import type { Run, Submission } from './workspace';

/** What the workspace knows about the person's judging on this problem. */
export interface JudgingState {
  /** Own submissions, newest first. */
  submissions: Submission[];
  /** The latest run, if any. */
  run: Run | null;
  /** Server time from the latest event or response that carried one. */
  serverNow: number | null;
}

/** One message from GET /api/events, already parsed. */
export type StreamEvent =
  | { type: 'submission'; data: Submission }
  | { type: 'run'; data: Run }
  | { type: 'clock'; data: { server_now: number } };

const STAGE: Record<string, number> = { queued: 0, running: 1, done: 2, error: 2 };

function judged(s: Submission): number {
  let n = s.samples.filter((t) => t.verdict !== '').length;
  for (const st of s.subtasks) n += st.tests.filter((t) => t.verdict !== '').length;
  return n;
}

/** True when `next` is at least as far along as `prev`. */
export function isNewer(prev: Submission, next: Submission): boolean {
  const a = STAGE[prev.state] ?? 0;
  const b = STAGE[next.state] ?? 0;
  if (b !== a) return b > a;
  if (a === 1) return judged(next) >= judged(prev);
  return true;
}

/** Puts a submission into a newest-first list, keeping the newer picture. */
export function mergeSubmission(list: readonly Submission[], next: Submission): Submission[] {
  const i = list.findIndex((s) => s.id === next.id);
  if (i >= 0) {
    if (!isNewer(list[i], next)) return list as Submission[];
    const out = list.slice();
    out[i] = next;
    return out;
  }
  return [next, ...list].sort((a, b) => b.id - a.id);
}

/** Merges a fetched list (newest first) into the current one. */
export function mergeSubmissions(list: readonly Submission[], fetched: readonly Submission[]): Submission[] {
  let out = list as Submission[];
  for (const s of fetched) out = mergeSubmission(out, s);
  return out;
}

function runStage(r: Run): number {
  return STAGE[r.state] ?? 0;
}

/** Takes a run if it is the one shown (or newer) and not behind it. */
export function mergeRun(current: Run | null, next: Run): Run | null {
  if (!current || next.id > current.id) return next;
  if (next.id < current.id) return current;
  if (runStage(next) < runStage(current)) return current;
  if (runStage(next) === runStage(current) && next.results.length < current.results.length) return current;
  return next;
}

/** Applies one stream event for the problem `problemId`. */
export function reduceEvent(state: JudgingState, ev: StreamEvent, problemId: number): JudgingState {
  switch (ev.type) {
    case 'submission': {
      if (ev.data.problem_id !== problemId) return state;
      const submissions = mergeSubmission(state.submissions, ev.data);
      return submissions === state.submissions ? state : { ...state, submissions, serverNow: ev.data.server_now ?? state.serverNow };
    }
    case 'run': {
      // Runs carry no problem id, and one may come from another tab; only
      // the run this page started is shown.
      if (!state.run || ev.data.id !== state.run.id) return state;
      const run = mergeRun(state.run, ev.data);
      return run === state.run ? state : { ...state, run, serverNow: ev.data.server_now ?? state.serverNow };
    }
    case 'clock':
      return { ...state, serverNow: ev.data.server_now };
    default:
      return state;
  }
}
