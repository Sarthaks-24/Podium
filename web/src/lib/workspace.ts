/**
 * The student workspace's data shapes and the wording of its results.
 *
 * The shapes mirror the API: Workspace from GET /api/workspace/{problem},
 * Submission and Run from the submit and run routes and the event stream.
 * Paths are built from a base such as "/api/workspace/12", so a contest can point
 * the same screen at a contest's routes.
 *
 * The rest is wording: the headline a submission reads as ("Wrong answer on
 * test 5", "Partial, 40 of 100", "3rd in queue"), the sentence a screen
 * reader hears when it finishes, one line of guidance per failing verdict
 * with a language hint where one is safe, and times and memory as shown.
 */
import { LANGUAGES, factorWords, limitTime, ordinal, verdictLabel, type Language } from './problems';

export { LANGUAGES, type Language };

/** A sample test shown with the statement; text is cut at 4 KB. */
export interface Sample {
  idx: number;
  input: string;
  output: string;
  truncated: boolean;
}

/** The code last saved from the editor. updated_at is server Unix ms. */
export interface Draft {
  language: Language;
  source: string;
  updated_at: number;
}

/** GET /api/workspace/{problem}. */
export interface Workspace {
  problem_id: number;
  title: string;
  statement_md: string;
  time_limit_ms: number;
  memory_limit_mb: number;
  /** `time_factor` multiplies the time limit for that language; absent from older servers. */
  languages: { id: Language; name: string; time_factor?: number }[];
  samples: Sample[];
  draft: Draft | null;
  /** Own submissions, newest first, at most 20. */
  submissions: Submission[];
  server_now: number;
  /** Contest workspaces: why Run and Submit are off, or null. */
  refusal?: string | null;
  can_submit?: boolean;
  /** True when submissions now count as practice. */
  practice?: boolean;
}

/** "submitted" is a submission whose result the server hides from the viewer (its `hidden` flag). */
export type JudgeState = 'queued' | 'running' | 'done' | 'error' | 'submitted';

/** One judged test. verdict is "" while the test waits its turn. */
export interface TestResult {
  idx: number;
  verdict: string;
  time_ms: number;
  memory_kb: number;
  /** Checker comment; samples only. */
  message?: string;
}

/** One subtask of a submission: all or nothing. */
export interface SubtaskResult {
  idx: number;
  points: number;
  earned: number;
  /** "" until the subtask is finished. */
  verdict: string;
  tests: TestResult[];
}

/** A submission as the API returns it (hidden tests carry no output). */
export interface Submission {
  id: number;
  problem_id: number;
  language: Language;
  state: JudgeState;
  /** "" until done. */
  verdict: string;
  score: number;
  max_score: number;
  max_time_ms: number;
  max_memory_kb: number;
  created_at: number;
  judged_at: number | null;
  /** 1 for the next job the judge takes; null once running. */
  queue_position: number | null;
  compile_output: string;
  subtasks: SubtaskResult[];
  samples: TestResult[];
  server_now?: number;
  /** Exam mode before the end: everything but the time and language is withheld. */
  hidden?: boolean;
  /** The first failing hidden test, on a submission that never counts. */
  revealed?: SubmissionRevealed | null;
  /** The step the times were rounded to: 100 for a student in a graded contest, else 10. */
  time_step_ms?: number;
}

/** A hidden test a practice submission shows in full. Each text is at most 4 KB. */
export interface SubmissionRevealed {
  idx: number;
  subtask_idx: number | null;
  input: string;
  expected: string;
  output: string;
  /** True when any of the three was cut at 4 KB. */
  truncated: boolean;
}

/** The first failing sample of a Run, in full. */
export interface RunRevealed extends SubmissionRevealed {
  kind: 'sample';
  label: string;
  /** The end of the program's stderr, at most 4 KB. */
  stderr?: string;
}

/** One input of a run: a sample or the custom input. */
export interface RunResult {
  /** Missing from servers before v1.2.0, which ran samples and custom input only. */
  kind?: 'sample' | 'custom';
  label: string;
  /** "OK" when a custom input ran without error, else the verdict. */
  verdict: string;
  time_ms: number;
  memory_kb: number;
  output: string;
  output_truncated?: boolean;
  /** Expected output; samples only. */
  expected: string | null;
  message?: string;
  /** The end of the program's stderr, at most 4 KB. */
  stderr?: string;
}

/** A run on the samples or on a custom input. */
export interface Run {
  id: number;
  state: JudgeState;
  compile_output: string;
  queue_position?: number | null;
  results: RunResult[];
  /** The first failing sample; absent for a custom input and when all passed. */
  revealed?: RunRevealed | null;
  /** The step the times were rounded to. */
  time_step_ms?: number;
  server_now?: number;
}

/** POST /api/admin/problems/{id}/generate and GET .../generation. */
export interface Generation {
  id: number;
  state: string;
  results: { idx: number; verdict: string; time_ms: number }[];
  /** The reference solution's compiler output; with no results it means the compile failed. */
  compile_output?: string;
  /** Set when done: how many outputs were applied, why any were not, and whether a validation was queued next. */
  summary?: { applied: number; problems: string[]; validating?: boolean } | null;
  error: string | null;
}

/** API paths under a workspace base such as "/api/workspace/12". */
export function workspacePaths(base: string) {
  return {
    load: base,
    draft: `${base}/draft`,
    run: `${base}/run`,
    submit: `${base}/submit`,
    events: `${base}/events`,
  };
}

/** The workspace base for a problem outside a contest. */
export function problemWorkspace(id: number | string): string {
  return `/api/workspace/${encodeURIComponent(String(id))}`;
}

/** True while the judge has it. */
export function isWaiting(x: { state: JudgeState }): boolean {
  return x.state === 'queued' || x.state === 'running';
}

/** The label for a language id. */
export function languageLabel(id: string): string {
  return LANGUAGES.find((l) => l.id === id)?.label ?? id;
}

/**
 * Time as shown, rounded to the nearest `step` ms: the server's
 * `time_step_ms` (100 for a student in a graded contest, else 10), never
 * finer than 10. The server rounds to the nearest step, so a 0 reads
 * "under 50 ms" or "under 5 ms" rather than "0 ms", which reads as a mistake.
 */
export function formatMs(ms: number, step = 10): string {
  const s = Math.max(step, 10);
  const shown = Math.round(ms / s) * s;
  return shown === 0 ? `under ${s / 2} ms` : `${shown} ms`;
}

/** Memory as shown: "512 KB" or "3.1 MB". */
export function formatKb(kb: number): string {
  // Students never get a hidden test's memory: the server sends 0,
  // which no real run uses, so it shows as nothing rather than "0 KB".
  if (kb <= 0) return '';
  if (kb < 1024) return `${Math.round(kb)} KB`;
  return `${(kb / 1024).toFixed(1)} MB`;
}

/**
 * The limits line under the title: "Time limit 1 s, memory 256 MB". For a
 * language the judge gives more time (its `time_factor`), the limit is the
 * one that language gets, with the reason: "Time limit 2 s in Java 21
 * (twice the problem's 1 s, as it runs slower than C++), memory 256 MB".
 */
export function limitsText(timeMs: number, memoryMb: number, lang?: { label: string; factor?: number }): string {
  const f = lang?.factor;
  if (!lang || !f || f === 1) return `Time limit ${limitTime(timeMs)}, memory ${memoryMb} MB`;
  const own = limitTime(Math.round(timeMs * f));
  return `Time limit ${own} in ${lang.label} (${factorWords(f)} the problem's ${limitTime(timeMs)}, as it runs slower than C++), memory ${memoryMb} MB`;
}

/** True for a verdict that ends a test badly. Accepted, OK and skipped are not. */
export function isBad(verdict: string): boolean {
  return verdict !== '' && verdict !== 'AC' && verdict !== 'OK' && verdict !== 'SK';
}

/** Every test in judging order: samples first, then each subtask's tests. */
export function testsInOrder(s: Pick<Submission, 'samples' | 'subtasks'>): (TestResult & { sample: boolean })[] {
  return [...s.samples.map((t) => ({ ...t, sample: true })), ...s.subtasks.flatMap((st) => st.tests.map((t) => ({ ...t, sample: false })))];
}

/** The first test that failed, in judging order, or null. */
export function firstFailure(s: Pick<Submission, 'samples' | 'subtasks'>): (TestResult & { sample: boolean }) | null {
  return testsInOrder(s).find((t) => isBad(t.verdict)) ?? null;
}

/** "Wrong answer on test 5". */
export function failureText(verdict: string, idx: number): string {
  return `${verdictLabel(verdict)} on test ${idx}`;
}

/** The tone a headline is drawn in. */
export type Tone = 'ok' | 'part' | 'bad' | 'wait';

/** What a submission reads as at the top of the verdict panel. */
export interface Headline {
  text: string;
  tone: Tone;
  /** A second line naming the first failure, for partial scores. */
  detail: string;
}

/** The waiting line: "3rd in queue", or "Judging" once the judge has it. */
export function waitingText(x: { state: JudgeState; queue_position?: number | null }): string {
  if (x.state === 'running') return 'Judging';
  if (x.queue_position !== null && x.queue_position !== undefined && x.queue_position > 0) {
    return `${ordinal(x.queue_position)} in queue`;
  }
  return 'Waiting for the judge';
}

/**
 * The waiting headline of a submission: the press is confirmed with the
 * button's word: "Submitted, 3rd in queue",
 * "Submitted, waiting for the judge", "Submitted, judging".
 */
export function submittedText(x: { state: JudgeState; queue_position?: number | null }): string {
  const rest = waitingText(x);
  return `Submitted, ${rest.charAt(0).toLowerCase()}${rest.slice(1)}`;
}

/** The headline for a submission in any state. */
export function headline(s: Submission): Headline {
  if (isWaiting(s)) return { text: submittedText(s), tone: 'wait', detail: '' };
  if (s.state === 'error')
    return {
      text: 'The judge could not finish this submission',
      tone: 'bad',
      detail: '',
    };
  if (s.verdict === 'CE') return { text: 'Compile error', tone: 'bad', detail: '' };
  if (s.verdict === 'AC') return { text: 'Accepted', tone: 'ok', detail: '' };
  const fail = firstFailure(s);
  const failLine = fail ? failureText(fail.verdict, fail.idx) : verdictLabel(s.verdict);
  if (s.score > 0 && s.score < s.max_score) {
    return {
      text: `Partial, ${s.score} of ${s.max_score}`,
      tone: 'part',
      detail: failLine,
    };
  }
  return { text: failLine, tone: 'bad', detail: '' };
}

/**
 * The sentence announced when a submission finishes, or changes state:
 * "Accepted. Score 100 of 100." The score is always a sentence, never a
 * bare number.
 */
export function announcement(s: Submission): string {
  if (s.state === 'queued') return 'Submitted. Waiting for the judge.';
  if (s.state === 'running') return 'Judging.';
  if (s.state === 'error') return 'The judge could not finish this submission. Submit again.';
  const h = headline(s);
  const lead = h.tone === 'part' ? `Partial score. ${h.detail}` : h.text;
  return `${lead}. Score ${s.score} of ${s.max_score}.`;
}

/**
 * The line under a finished submission that scored less than the problem
 * already has: "Your score on this problem stays 60 of 100."
 * Empty when the submission is not done or did not score lower.
 */
export function problemScoreLine(s: Pick<Submission, 'state' | 'score' | 'hidden'>, problem: { score: number | null; max: number } | null): string {
  if (!problem || problem.score === null || s.state !== 'done' || s.hidden) return '';
  return problem.score > s.score ? `Your score on this problem stays ${problem.score} of ${problem.max}.` : '';
}

/** "3 of 50 submissions used" beside Submit. */
export function usedText(count: number, max: number): string {
  return `${count} of ${max} submissions used`;
}

/** True when a finished run stopped at the compiler: no input ran, and there is compiler output. */
export function runCompileFailed(run: Run): boolean {
  return run.state === 'done' && run.compile_output !== '' && run.results.every((r) => r.verdict === '');
}

/** True for the row of a custom input. Older servers send no kind: a row without an expected output was one. */
export function isCustomResult(r: Pick<RunResult, 'kind' | 'expected'>): boolean {
  return r.kind ? r.kind === 'custom' : r.expected === null;
}

function passedCount(list: readonly RunResult[]): number {
  return list.filter((r) => r.verdict === 'AC' || r.verdict === 'OK').length;
}

/**
 * The headline and tone of a run (RunResults shows it; the announcer
 * speaks it), in the words of design/student.md: "All 3 examples passed"
 * or "2 of 3 examples failed".
 */
export function runHeadline(run: Run): { text: string; tone: Tone } {
  if (isWaiting(run)) return { text: run.state === 'running' ? 'Running' : waitingText(run), tone: 'wait' };
  if (run.state === 'error') return { text: 'The run could not finish. Try again in a moment.', tone: 'bad' };
  if (run.results.every((r) => r.verdict === '') && run.compile_output) return { text: 'Compile error', tone: 'bad' };
  const tests = run.results.filter((r) => !isCustomResult(r));
  if (tests.length > 0) {
    const failed = tests.length - passedCount(tests);
    if (tests.length === 1) return failed ? { text: 'The example failed', tone: 'bad' } : { text: 'The example passed', tone: 'ok' };
    if (failed === 0) return { text: `All ${tests.length} examples passed`, tone: 'ok' };
    return { text: `${failed} of ${tests.length} examples failed`, tone: 'bad' };
  }
  const r = run.results[0];
  return r ? { text: runVerdictLabel(r.verdict), tone: isBad(r.verdict) ? 'bad' : 'ok' } : { text: 'Finished', tone: 'ok' };
}

/** The heading of a submission's revealed test: "Test 5, in subtask 2". */
export function revealedTitle(r: Pick<SubmissionRevealed, 'idx' | 'subtask_idx'>): string {
  return r.subtask_idx === null ? `Test ${r.idx}` : `Test ${r.idx}, in subtask ${r.subtask_idx}`;
}

/**
 * The sentence announced for a run: "Running on samples.",
 * "Passed 2 of 3 samples.", "Compile error.", or "" while it waits in the
 * queue, so a queue position never repeats.
 */
export function runAnnouncement(run: Run): string {
  if (run.state === 'queued') return '';
  const custom = run.results.some(isCustomResult);
  if (run.state === 'running') return custom ? 'Running on your input.' : 'Running on the examples.';
  return `${runHeadline(run).text}.`;
}

const GUIDANCE: Record<string, string> = {
  WA: 'Your output differs from the expected answer. Check edge cases such as the smallest and largest inputs, and the output format in the examples.',
  TLE: 'Your program ran past the time limit. Look for an algorithm that fits the largest constraints.',
  MLE: 'Your program used more memory than allowed. Avoid storing what you can compute as you go.',
  RE: 'Your program crashed or exited with an error. Check array bounds, division by zero, very deep recursion, and numbers too big for their type.',
  OLE: 'Your program printed far more than expected. Remove debugging output.',
  CE: 'Fix the first error below; later errors often follow from it.',
  IE: 'Something went wrong in the judge, not in your code. Submit again, and tell an organiser if it repeats.',
};

const HINTS: Partial<Record<string, Partial<Record<Language, string>>>> = {
  TLE: {
    cpp17: 'Use fast input: call ios::sync_with_stdio(false) and cin.tie(nullptr) at the start of main.',
    java21: 'Read input with a BufferedReader instead of Scanner, and print with a PrintWriter.',
    python3: 'Read input with sys.stdin, which is much faster than input().',
    node22: 'Read all input at once with fs.readFileSync(0) instead of line by line.',
  },
  CE: {
    java21: 'If the error names a class or a .java file, call your public class Main: the file is Main.java.',
  },
  RE: {
    java21: 'Use long for numbers past 2147483647 (a NumberFormatException means one did not fit), and read input that may span several lines.',
    python3: 'Read input with sys.stdin and split it, so extra spaces and blank lines do not break parsing.',
  },
};

/**
 * One line of guidance for a failing verdict, plus a language hint where
 * one is safe. Accepted, skipped and unknown verdicts get none.
 */
export function guidance(verdict: string, language: string, opts: { sample?: boolean } = {}): string[] {
  const line = GUIDANCE[verdict];
  if (!line) return [];
  const out = [line];
  if (verdict === 'WA' && opts.sample) out[0] = 'This test is an example. Run to see your output next to the expected one.';
  const hint = HINTS[verdict]?.[language as Language];
  if (hint) out.push(hint);
  return out;
}

/**
 * The line under a run's example: the judge's own comparison in plain
 * words, or a problem checker's comment marked as the checker's, so it is
 * never read as the program's output. The judge's sentences come from
 * server/internal/judge/checker.go (they count whitespace-separated
 * "tokens", said here as words); its notes about a checker
 * (custom_checker.go) already start with "The checker" or "The judge".
 */
export function resultNote(message: string): string {
  const m = message.trim();
  if (m === '') return '';
  let x = /^Token (\d+) differs: (.*)$/s.exec(m);
  if (x) return `The first difference is at word ${x[1]}: ${x[2]}`;
  x = /^Token (\d+) is longer than (.*)$/s.exec(m);
  if (x) return `Word ${x[1]} is longer than ${x[2]}`;
  x = /^Output ended early: (.*) at token (\d+)\.$/s.exec(m);
  if (x) return `Output ended early: ${x[1]} at word ${x[2]}.`;
  x = /^Extra output from token (\d+): (.*)$/s.exec(m);
  if (x) return `Extra output from word ${x[1]}: ${x[2]}`;
  if (/^Output differs from line \d+\.$/.test(m) || /^The (checker|judge)\b/.test(m)) return m;
  return `The checker said: ${m}`;
}

/** The word for a run result's verdict; a custom input that ran is "Finished". */
export function runVerdictLabel(verdict: string): string {
  if (verdict === 'OK') return 'Finished';
  return verdictLabel(verdict);
}

/**
 * A fresh idempotency key for one press of Submit. crypto.randomUUID needs
 * a secure context, so plain http on a LAN address falls back to random
 * bytes laid out the same way.
 */
export function newKey(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === 'function') {
    try {
      return c.randomUUID();
    } catch {
      // Not a secure context; fall through.
    }
  }
  const bytes = new Uint8Array(16);
  if (c && typeof c.getRandomValues === 'function') c.getRandomValues(bytes);
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** "19:42" for today, "16 Oct 19:42" for another day, in local time. */
export function formatWhen(ms: number, now: number = Date.now()): string {
  const d = new Date(ms);
  const n = new Date(now);
  const time = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  const sameDay = d.getFullYear() === n.getFullYear() && d.getMonth() === n.getMonth() && d.getDate() === n.getDate();
  return sameDay ? time : `${d.getDate()} ${MONTHS[d.getMonth()]} ${time}`;
}

/** The best score among finished submissions, or null when none finished. */
export function bestScore(list: readonly Submission[]): { score: number; max: number } | null {
  let best: { score: number; max: number } | null = null;
  for (const s of list) {
    if (s.state !== 'done') continue;
    if (!best || s.score > best.score) best = { score: s.score, max: s.max_score };
  }
  return best;
}
