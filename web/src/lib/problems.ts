/**
 * The problem bank's data shapes and the pure helpers behind the admin
 * screens (docs/API.md).
 *
 * The editor works on a Draft: the editable part of a Problem with tags and
 * sample tests held as the text the person typed. toDraft() builds one from
 * a Problem and toEdit() turns it back into the ProblemEdit body for PUT,
 * reporting input it cannot read as field errors keyed like the server's.
 * The rest turns a Validation into the plain sentences the report shows.
 *
 * A problem may carry a generator and a plan of the tests it makes. A
 * server that sends no "generator" key is older: the draft then has no
 * generator part and toEdit() sends none, so older servers and fixtures
 * see the same body as before.
 */

/** Program languages, as the API names them. */
export type Language = 'cpp17' | 'java21' | 'python3' | 'node22';

/** Languages in the order the select lists them, with their labels. */
export const LANGUAGES: readonly { id: Language; label: string }[] = [
  { id: 'cpp17', label: 'C++17' },
  { id: 'java21', label: 'Java 21' },
  { id: 'python3', label: 'Python 3' },
  { id: 'node22', label: 'JavaScript (Node 22)' },
];

/** How a program's output is compared with the expected output. */
export type Checker = 'tokens' | 'exact' | 'float' | 'custom';

/** Checkers in the order the select lists them, with their labels. */
export const CHECKERS: readonly { id: Checker; label: string; help: string }[] = [
  {
    id: 'tokens',
    label: 'Word by word, ignoring extra spaces',
    help: 'The output matches when it has the same words and numbers as the expected output, whatever the spaces and line breaks between them.',
  },
  { id: 'exact', label: 'Exactly, character by character', help: 'The output must be the expected output exactly, spaces and line breaks included.' },
  { id: 'float', label: 'Numbers within an allowed error', help: 'Like word by word, but numbers match when they are close enough to the expected ones.' },
  { id: 'custom', label: 'Custom checker (testlib)', help: 'Your own program decides, for problems with more than one right answer.' },
];

export type Difficulty = '' | 'easy' | 'medium' | 'hard';

/** Difficulties in the order the select lists them, with their labels. */
export const DIFFICULTIES: readonly { id: Difficulty; label: string }[] = [
  { id: '', label: 'None' },
  { id: 'easy', label: 'Easy' },
  { id: 'medium', label: 'Medium' },
  { id: 'hard', label: 'Hard' },
];

/** One row of GET /api/admin/problems. Times are Unix milliseconds. */
export interface ProblemSummary {
  id: number;
  slug: string;
  title: string;
  difficulty: Difficulty;
  tags: string[];
  origin: 'manual' | 'import' | 'ai';
  archived: boolean;
  test_count: number;
  points: number;
  validated: boolean;
  validated_at: number | null;
  updated_at: number;
  /** A contest pinning it showed students its hidden tests (practice after the end). */
  shown_in_practice?: boolean;
}

/** A program's language and source. */
export interface Program {
  language: Language;
  source: string;
}

/** A subtask as the server returns it; `tests` is a range such as "3-5". */
export interface Subtask {
  idx: number;
  points: number;
  title: string;
  tests: string;
  /** Arguments the generator gets for this subtask's planned tests. */
  generator_args?: string[];
}

/** One test the generator makes: argv is the seed, then `args`. */
export interface PlannedTest {
  idx: number;
  subtask: number;
  seed: number;
  args: string[];
}

/** Normalises a plan from the server, accepting "test" for "idx". */
export function readPlan(raw: unknown): PlannedTest[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((r: Record<string, unknown>) => ({
    idx: Number(r.idx ?? r.test ?? 0),
    subtask: Number(r.subtask ?? 1),
    seed: Number(r.seed ?? 0),
    args: Array.isArray(r.args) ? r.args.map(String) : [],
  }));
}

/** One generator argument: letters, digits and _ . , : = + -, up to 64 characters. */
export const ARG_PATTERN = /^[A-Za-z0-9_.,:=+-]{1,64}$/;

/** At most this many arguments per test or subtask. */
export const MAX_ARGS = 16;

/** The largest seed a planned test may use. */
export const MAX_SEED = 1_000_000_000;

/** Splits typed arguments on spaces; an empty string is no arguments. */
export function splitArgs(text: string): string[] {
  return text.split(/\s+/).filter((a) => a !== '');
}

/** Checks typed arguments; returns a sentence about the first problem, or "". */
export function checkArgs(text: string): string {
  const args = splitArgs(text);
  if (args.length > MAX_ARGS) return `Use at most ${MAX_ARGS} arguments.`;
  const bad = args.find((a) => !ARG_PATTERN.test(a));
  if (bad !== undefined) {
    const shown = bad.length > 24 ? `${bad.slice(0, 24)}...` : bad;
    return `"${shown}" is not a valid argument. Use letters, digits and _ . , : = + -, separated by spaces.`;
  }
  return '';
}

/**
 * The row "Add planned test" appends: the next free test number after the
 * stored tests and the plan, the last row's subtask (or the last subtask),
 * and the next seed.
 */
export function nextPlanRow(
  plan: readonly { idx: number | null; subtask: number; seed: number | null }[],
  testIdxs: readonly number[],
  subtaskCount: number,
): { idx: number; subtask: number; seed: number; args: string } {
  const used = [...testIdxs, ...plan.map((r) => r.idx ?? 0)];
  const idx = Math.min(999, used.reduce((m, n) => Math.max(m, n), 0) + 1);
  const last = plan.at(-1);
  const subtask = last ? last.subtask : Math.max(1, subtaskCount);
  const seed = plan.reduce((m, r) => Math.max(m, r.seed ?? 0), 0) + 1;
  return { idx, subtask, seed, args: '' };
}

/** One stored test. Previews are the first 200 bytes of each file. */
export interface TestCase {
  idx: number;
  sample: boolean;
  /** The subtask whose range holds the test, or null (a sample, or unassigned). */
  subtask_idx: number | null;
  input_size: number;
  output_size: number;
  input_preview: string;
  output_preview: string;
  /** True when the test has no expected output yet (waiting for generation). */
  output_missing?: boolean;
}

/** A running contest that pins the problem: while there is one, a save needs a reason. */
export interface LiveContest {
  contest_id: number;
  slug: string;
  title: string;
  label: string;
}

/** GET /api/admin/problems/{id}. */
export interface Problem extends ProblemSummary {
  /** The revision the working copy is. */
  revision?: { id: number; rev: number };
  live_contests?: LiveContest[];
  statement_md: string;
  time_limit_ms: number;
  memory_limit_mb: number;
  checker: Checker;
  epsilon: number;
  checker_source: string;
  reference: Program;
  brute: Program | null;
  validator: Program | null;
  /** The input generator; absent from older servers. */
  generator?: Program | null;
  /** The tests the generator makes. */
  plan?: PlannedTest[];
  samples: number[];
  subtasks: Subtask[];
  tests: TestCase[];
  content_hash: string;
  validated_hash: string | null;
  /** What stops validation and judging (a test in no subtask, a missing output); older servers leave it out. */
  problems?: string[];
  warnings: string[];
}

/** The body of PUT /api/admin/problems/{id}. */
export interface ProblemEdit {
  title: string;
  slug: string;
  statement_md: string;
  time_limit_ms: number;
  memory_limit_mb: number;
  checker: Checker;
  epsilon: number;
  checker_source: string;
  reference: Program;
  brute: Program | null;
  validator: Program | null;
  tags: string[];
  difficulty: Difficulty;
  samples: number[];
  subtasks: { points: number; title: string; tests: string; generator_args?: string[] }[];
  generator?: Program | null;
  plan?: PlannedTest[];
}

/** One planned test as the editor's table holds it. */
export interface PlanRow {
  idx: number | null;
  subtask: number;
  seed: number | null;
  /** Arguments separated by spaces, as typed. */
  args: string;
}

/** The editor's working copy of a problem. */
export interface Draft {
  title: string;
  slug: string;
  statement_md: string;
  time_limit_ms: number | null;
  memory_limit_mb: number | null;
  checker: Checker;
  epsilon: number | null;
  checker_source: string;
  reference: Program;
  brute: Program | null;
  validator: Program | null;
  /** Comma-separated, as typed. */
  tags: string;
  difficulty: Difficulty;
  /** Test numbers and ranges, as typed, for example "1, 2". */
  samples: string;
  /** `args`: generator arguments separated by spaces, absent from older servers. */
  subtasks: { points: number | null; title: string; tests: string; args?: string }[];
  /** Present only when the server knows about generators. */
  generator?: Program | null;
  plan?: PlanRow[];
}

export type ValidationState = 'queued' | 'running' | 'passed' | 'failed' | 'error';

/** Who ran in a validation: the four programs a problem can have. */
export type Role = 'reference' | 'brute' | 'validator' | 'checker';

/** GET /api/admin/problems/{id}/validation. */
export interface Validation {
  id: number;
  state: ValidationState;
  content_hash: string;
  /** True when the problem has not changed since this validation was asked for. */
  current: boolean;
  queued_at: number;
  finished_at: number | null;
  /** 1 for the next job the judge takes; null once running. */
  queue_position: number | null;
  compile: { role: Role; verdict: string; output: string }[];
  tests: { idx: number; role: Role; verdict: string; time_ms: number; memory_kb: number; message: string }[];
  summary: {
    reference_passed: number;
    reference_total: number;
    brute_agreed: number;
    brute_total: number;
    /** Tests the brute force did not finish (time limit or skipped): nothing cross-checked them. */
    brute_unchecked?: number;
    validator_passed: number;
    validator_total: number;
    max_time_ms: number;
    /** The problem's limit times the reference language's factor. */
    time_limit_ms: number;
    /** The reference's language and its time factor; absent on summaries stored before. */
    reference_language?: string;
    time_factor?: number;
    problems: string[];
    warnings: string[];
  } | null;
  error: string | null;
}

/** True while the judge has the validation queued or running. */
export function isPending(v: Pick<Validation, 'state'> | null): boolean {
  return v !== null && (v.state === 'queued' || v.state === 'running');
}

/** The result of reading a list of test numbers. */
export type TestListResult = { ok: true; tests: number[] } | { ok: false; error: string };

/**
 * Reads test numbers and ranges separated by commas or spaces, such as
 * "1, 2" or "3-5 8". Numbers run from 1 to 999. The answer is sorted
 * without repeats. An empty string is an empty list.
 */
export function parseTestList(text: string): TestListResult {
  const out = new Set<number>();
  const parts = text.split(/[\s,]+/).filter((p) => p !== '');
  for (const part of parts) {
    const m = /^(\d+)(?:-(\d+))?$/.exec(part);
    if (!m) return { ok: false, error: `"${part}" is not a test number or range. Write numbers like 1, 2 or a range like 3-5.` };
    const from = Number(m[1]);
    const to = m[2] === undefined ? from : Number(m[2]);
    if (from < 1 || to > 999) return { ok: false, error: 'Test numbers run from 1 to 999.' };
    if (to < from) return { ok: false, error: `The range ${part} runs backwards. Write the smaller number first.` };
    for (let i = from; i <= to; i++) out.add(i);
  }
  return { ok: true, tests: [...out].sort((a, b) => a - b) };
}

/** Writes test numbers compactly, joining runs into ranges: "1, 2, 5-7". */
export function formatTestList(tests: readonly number[]): string {
  const sorted = [...new Set(tests)].sort((a, b) => a - b);
  const parts: string[] = [];
  for (let i = 0; i < sorted.length; i++) {
    let j = i;
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++;
    if (j - i >= 2) parts.push(`${sorted[i]}-${sorted[j]}`);
    else for (let k = i; k <= j; k++) parts.push(String(sorted[k]));
    i = j;
  }
  return parts.join(', ');
}

/** A subtask's test list in the one form the editor writes ("3-5, 8"), so a role moved and moved back leaves no change. */
export function rangesOf(text: string): string {
  const r = parseTestList(text);
  return r.ok ? rangeText(r.tests) : text;
}

/** Builds the editor's working copy from a problem. */
export function toDraft(p: Problem): Draft {
  const gen = p.generator !== undefined;
  const draft: Draft = {
    title: p.title,
    slug: p.slug,
    statement_md: p.statement_md,
    time_limit_ms: p.time_limit_ms,
    memory_limit_mb: p.memory_limit_mb,
    checker: p.checker,
    epsilon: p.epsilon,
    checker_source: p.checker_source,
    reference: { ...p.reference },
    brute: p.brute ? { ...p.brute } : null,
    validator: p.validator ? { ...p.validator } : null,
    // The AI draft tag goes when a person saves, so it is not edited here.
    tags: p.tags.filter((t) => t.trim().toLowerCase() !== 'ai draft').join(', '),
    difficulty: p.difficulty,
    samples: formatTestList(p.samples),
    subtasks: p.subtasks.map((s) =>
      gen
        ? { points: s.points, title: s.title, tests: rangesOf(s.tests), args: (s.generator_args ?? []).join(' ') }
        : { points: s.points, title: s.title, tests: rangesOf(s.tests) },
    ),
  };
  if (gen) {
    draft.generator = p.generator ? { ...p.generator } : null;
    draft.plan = readPlan(p.plan).map((t) => ({ idx: t.idx, subtask: t.subtask, seed: t.seed, args: t.args.join(' ') }));
  }
  return draft;
}

/** The result of turning a draft into a request body. */
export type EditResult = { ok: true; edit: ProblemEdit } | { ok: false; fields: Record<string, string> };

/**
 * Turns a draft into the PUT body. Only input that cannot be sent at all is
 * refused here (empty numbers, unreadable test lists); the server checks
 * everything else. Field keys match the server's: "samples",
 * "subtasks.0.tests" and so on, with subtasks counted from 0.
 */
export function toEdit(d: Draft): EditResult {
  const fields: Record<string, string> = {};
  const need = (key: string, value: number | null, what: string): number => {
    if (value === null || !Number.isFinite(value)) {
      fields[key] = `Enter ${what}.`;
      return 0;
    }
    return value;
  };
  const time = need('time_limit_ms', d.time_limit_ms, 'a time limit in milliseconds');
  const memory = need('memory_limit_mb', d.memory_limit_mb, 'a memory limit in megabytes');
  const epsilon = d.checker === 'float' ? need('epsilon', d.epsilon, 'the allowed error, for example 0.000001') : (d.epsilon ?? 1e-6);

  const samples = parseTestList(d.samples);
  if (!samples.ok) fields.samples = samples.error;

  const subtasks = d.subtasks.map((s, i) => {
    const points = need(`subtasks.${i}.points`, s.points, 'the points for this subtask');
    const range = parseTestList(s.tests);
    if (!range.ok) fields[`subtasks.${i}.tests`] = range.error;
    else if (range.tests.length === 0) fields[`subtasks.${i}.tests`] = `Give subtask ${i + 1} at least one test: choose Subtask ${i + 1} as a test's role.`;
    if (s.args === undefined) return { points, title: s.title.trim(), tests: s.tests.trim() };
    const argError = checkArgs(s.args);
    if (argError !== '') fields[`subtasks.${i}.generator_args`] = argError;
    return { points, title: s.title.trim(), tests: s.tests.trim(), generator_args: splitArgs(s.args) };
  });

  let plan: PlannedTest[] | undefined;
  if (d.plan !== undefined) {
    const seen = new Set<number>();
    plan = d.plan.map((row, i) => {
      if (row.idx === null || !Number.isInteger(row.idx) || row.idx < 1 || row.idx > 999) {
        fields[`plan.${i}.idx`] = 'Enter a test number from 1 to 999.';
      } else if (seen.has(row.idx)) {
        fields[`plan.${i}.idx`] = `Test ${row.idx} is planned twice.`;
      } else {
        seen.add(row.idx);
      }
      if (!Number.isInteger(row.subtask) || row.subtask < 1 || row.subtask > d.subtasks.length) {
        fields[`plan.${i}.subtask`] = 'Choose one of the subtasks.';
      }
      if (row.seed === null || !Number.isInteger(row.seed) || row.seed < 0 || row.seed > MAX_SEED) {
        fields[`plan.${i}.seed`] = `Enter a seed from 0 to ${MAX_SEED}.`;
      }
      const argError = checkArgs(row.args);
      if (argError !== '') fields[`plan.${i}.args`] = argError;
      return { idx: row.idx ?? 0, subtask: row.subtask, seed: row.seed ?? 0, args: splitArgs(row.args) };
    });
    if (plan.length > 0 && !d.generator) fields.plan = 'Planned tests need a generator. Add one, or remove the planned tests.';
  }

  if (Object.keys(fields).length > 0) return { ok: false, fields };
  return {
    ok: true,
    edit: {
      title: d.title.trim(),
      slug: d.slug.trim(),
      statement_md: d.statement_md,
      time_limit_ms: time,
      memory_limit_mb: memory,
      checker: d.checker,
      epsilon,
      checker_source: d.checker === 'custom' ? d.checker_source : '',
      reference: { ...d.reference },
      brute: d.brute ? { ...d.brute } : null,
      validator: d.validator ? { ...d.validator } : null,
      tags: d.tags
        .split(',')
        .map((t) => t.trim())
        .filter((t) => t !== ''),
      difficulty: d.difficulty,
      samples: samples.ok ? samples.tests : [],
      subtasks,
      ...(d.generator !== undefined ? { generator: d.generator ? { ...d.generator } : null } : {}),
      ...(plan !== undefined ? { plan } : {}),
    },
  };
}

/** Normalises a server field key: "subtasks[1].tests" becomes "subtasks.1.tests". */
export function fieldKey(key: string): string {
  return key.replace(/\[(\d+)\]/g, '.$1').replace(/^\./, '');
}

/** The bank's status word for a problem. */
export function statusWord(p: Pick<ProblemSummary, 'archived' | 'validated'>): 'Archived' | 'Validated' | 'Not validated' {
  if (p.archived) return 'Archived';
  return p.validated ? 'Validated' : 'Not validated';
}

const VERDICTS: Record<string, string> = {
  AC: 'Accepted',
  WA: 'Wrong answer',
  TLE: 'Time limit exceeded',
  MLE: 'Memory limit exceeded',
  RE: 'Runtime error',
  OLE: 'Output limit exceeded',
  CE: 'Compile error',
  IE: 'Internal error',
  SK: 'Skipped',
};

/** The sentence-case word for a judge verdict code, such as "Wrong answer" for WA. */
export function verdictLabel(code: string): string {
  return VERDICTS[code] ?? code;
}

const ROLES: Record<Role, string> = {
  reference: 'reference solution',
  brute: 'brute force',
  validator: 'validator',
  checker: 'checker',
};

/** The lower-case name of a role, as used inside a sentence. */
export function roleName(role: Role): string {
  return ROLES[role] ?? role;
}

function capitalise(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** True when a verdict counts as a failure in the report. Skipped is not one. */
export function isFailure(verdict: string): boolean {
  return verdict !== 'AC' && verdict !== 'SK' && verdict !== 'OK';
}

/** English ordinal: 1st, 2nd, 3rd, 4th, 11th, 22nd. */
export function ordinal(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

/** The waiting line: "Waiting for the judge (2nd in queue)" or "Running". */
export function progressText(v: Pick<Validation, 'state' | 'queue_position'>): string {
  if (v.state === 'running') return 'Running';
  if (v.queue_position !== null && v.queue_position > 0) return `Waiting for the judge (${ordinal(v.queue_position)} in queue)`;
  return 'Waiting for the judge';
}

/** "twice", "3 times", "1.5 times": a language's time factor in words. */
export function factorWords(factor: number): string {
  return factor === 2 ? 'twice' : `${factor} times`;
}

/** "1 s" for whole seconds, else "1500 ms". */
export function limitTime(ms: number): string {
  return ms % 1000 === 0 ? `${ms / 1000} s` : `${ms} ms`;
}

/**
 * The reference's slowest run against its limit. When the reference's
 * language gets more than the problem's limit, the line says so: "of 2000
 * ms" on a 1000 ms problem read as a mistake.
 */
export function slowestReferenceText(s: Pick<NonNullable<Validation['summary']>, 'max_time_ms' | 'time_limit_ms' | 'reference_language' | 'time_factor'>): string {
  const head = `Slowest reference run ${s.max_time_ms} ms of`;
  const f = s.time_factor;
  if (!f || f === 1 || !s.reference_language) return `${head} ${s.time_limit_ms} ms`;
  const label = LANGUAGES.find((l) => l.id === s.reference_language)?.label ?? s.reference_language;
  return `${head} the ${s.time_limit_ms} ms ${label} gets (${factorWords(f)} the problem's ${Math.round(s.time_limit_ms / f)} ms)`;
}

/** One line of the validation report. */
/**
 * The tests the brute force did not finish, so nothing cross-checked their
 * expected output. Fewer than half cross-checked fails validation, so the
 * line is then drawn as a failure.
 */
export function uncheckedLine(s: NonNullable<Validation['summary']>): ReportLine | null {
  const n = s.brute_unchecked ?? 0;
  if (n === 0) return null;
  const checked = s.reference_total - n;
  return {
    text: `${n} of ${s.reference_total} tests not cross-checked: the brute force did not finish them`,
    tone: checked * 2 < s.reference_total ? 'bad' : 'note',
  };
}

export interface ReportLine {
  text: string;
  /** ok and bad are drawn in the verdict colours; note is muted. */
  tone: 'ok' | 'bad' | 'note';
  /** Program output shown verbatim under the line, in mono. */
  detail?: string;
}

/** One program's part of the validation report. */
export interface ReportSection {
  role: Role;
  /** "Reference solution", "Brute force", "Validator" or "Checker". */
  title: string;
  lines: (ReportLine & { guidance?: string })[];
}

/** The validation report: what to fix first, then each program, then notes. */
export interface Report {
  /** One sentence naming the first thing to fix; empty when nothing is wrong. */
  lead: string;
  sections: ReportSection[];
  /** The server's problems and warnings, and any judge error. */
  notes: ReportLine[];
}

/** Programs in the order the report lists them. */
export const ROLE_ORDER: readonly Role[] = ['reference', 'checker', 'validator', 'brute'];

/** A failing test as a line: "Wrong answer on test 7", or the role's own wording. */
export function testFailureText(t: { idx: number; role: Role; verdict: string }): string {
  if (t.role === 'validator' && t.verdict === 'WA') return `Rejected the input of test ${t.idx}`;
  if (t.role === 'brute' && t.verdict === 'WA') return `Disagreed with the reference on test ${t.idx}`;
  if (t.role === 'checker') return `Failed on test ${t.idx}`;
  return `${verdictLabel(t.verdict)} on test ${t.idx}`;
}

/** One line of guidance for a setter, by program and verdict. */
export function setterGuidance(role: Role, verdict: string): string {
  if (role === 'validator') return 'Fix this input so it meets the constraints, or fix the validator.';
  if (role === 'checker') return 'Check that the checker accepts every valid output and exits with 0, 1 or 2.';
  if (role === 'brute') {
    if (verdict === 'WA') return 'The brute force and the reference disagree here, so one of them is wrong. Compare them on this input.';
    if (verdict === 'TLE') return 'A slow brute force is expected; give it smaller tests or leave it out of the large ones.';
    return 'Run the brute force on this input to see what goes wrong.';
  }
  switch (verdict) {
    case 'WA':
      return "The reference must match every expected output. Check this test's output file, or the solution.";
    case 'TLE':
      return 'The reference must finish within half the time limit. Speed it up or raise the limit.';
    case 'MLE':
      return 'Reduce the memory the reference uses, or raise the memory limit.';
    case 'RE':
      return "Run the reference on this test's input; check bounds and recursion depth.";
    default:
      return 'Run the reference on this input to see what goes wrong.';
  }
}

/**
 * Builds the validation report grouped by program: a lead sentence naming the first thing to fix, then per program
 * its summary line, compile failure and failing tests with one line of
 * guidance each, then the server's problems and warnings.
 */
export function buildReport(v: Validation): Report {
  const sections = new Map<Role, ReportSection>();
  const section = (role: Role) => {
    let s = sections.get(role);
    if (!s) {
      s = { role, title: capitalise(roleName(role)), lines: [] };
      sections.set(role, s);
    }
    return s;
  };
  let lead = '';
  const firstFix = (text: string) => {
    if (lead === '') lead = text;
  };

  // Compile failures come first, in program order.
  for (const role of ROLE_ORDER) {
    for (const c of v.compile.filter((x) => x.role === role)) {
      if (c.verdict === 'CE') {
        section(role).lines.push({ text: `${capitalise(roleName(role))} did not compile`, tone: 'bad', detail: c.output });
        firstFix(`Fix the ${roleName(role)}: it did not compile.`);
      } else if (isFailure(c.verdict)) {
        section(role).lines.push({
          text: `${capitalise(roleName(role))}: ${verdictLabel(c.verdict).toLowerCase()} while compiling`,
          tone: 'bad',
          detail: c.output || undefined,
        });
        firstFix(`Fix the ${roleName(role)}: it failed while compiling.`);
      }
    }
  }

  const s = v.summary;
  if (s) {
    if (s.reference_total > 0) {
      section('reference').lines.unshift(
        {
          text: `Reference solution passed ${s.reference_passed} of ${s.reference_total} tests`,
          tone: s.reference_passed === s.reference_total ? 'ok' : 'bad',
        },
        {
          text: slowestReferenceText(s),
          tone: s.max_time_ms * 2 <= s.time_limit_ms ? 'ok' : 'bad',
        },
      );
      if (s.max_time_ms * 2 > s.time_limit_ms) {
        section('reference').lines[1] = {
          ...section('reference').lines[1],
          guidance: 'The reference must finish within half the time limit. Speed it up or raise the limit.',
        };
      }
    }
    if (s.validator_total > 0) {
      section('validator').lines.unshift({
        text: `Validator accepted ${s.validator_passed} of ${s.validator_total} inputs`,
        tone: s.validator_passed === s.validator_total ? 'ok' : 'bad',
      });
    }
    const unchecked = uncheckedLine(s);
    if (unchecked) section('brute').lines.unshift(unchecked);
    if (s.brute_total > 0) {
      section('brute').lines.unshift({
        text: `Brute force agreed on ${s.brute_agreed} of ${s.brute_total} tests`,
        tone: s.brute_agreed === s.brute_total ? 'ok' : 'bad',
      });
    }
  }

  for (const role of ROLE_ORDER) {
    for (const t of v.tests.filter((x) => x.role === role && isFailure(x.verdict))) {
      const text = testFailureText(t);
      section(role).lines.push({ text, tone: 'bad', detail: t.message || undefined, guidance: setterGuidance(role, t.verdict) });
      const verb = role === 'brute' ? 'Check' : 'Fix';
      firstFix(`${verb} the ${roleName(role)}: ${text.charAt(0).toLowerCase()}${text.slice(1)}.`);
    }
  }

  const notes: ReportLine[] = [];
  if (s) {
    for (const p of s.problems) notes.push({ text: p, tone: 'bad' });
    for (const w of s.warnings) notes.push({ text: w, tone: 'note' });
  }
  if (v.state === 'failed' && s && s.max_time_ms * 2 > s.time_limit_ms) {
    firstFix('Fix the reference solution: it is slower than half the time limit.');
  }
  if (v.state !== 'failed' && v.state !== 'error') lead = '';
  else if (lead === '') lead = s?.problems[0] ?? v.error ?? '';

  const ordered = ROLE_ORDER.map((r) => sections.get(r)).filter((x): x is ReportSection => x !== undefined);
  return { lead, sections: ordered, notes };
}

/** The element id of a program's source field in the editor. */
export function programFieldId(role: Role): string {
  return role === 'checker' ? 'checker_source' : `${role}-source`;
}

/** Formats a byte count: "512 B", "12.4 KB", "1.0 MB". */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** API paths for one problem. */
export function problemPath(id: number | string, rest = ''): string {
  return `/api/admin/problems/${encodeURIComponent(String(id))}${rest}`;
}

/** A program as the content hash sees it: one with empty source counts as none. */
function programKey(p: Program | null | undefined): string {
  return p && p.source.trim() !== '' ? `${p.language}|${p.source}` : '';
}

/**
 * The judged fields that differ between the saved copy and the working copy,
 * in words, mirroring server/internal/problems/hash.go (ContentHash): limits,
 * checker and its allowed error, the custom checker's source, the reference,
 * brute force and validator, examples and subtask points and ranges. Title,
 * slug, statement, tags, difficulty, subtask titles and the generator are
 * not judged. An AI draft also loses its validation when a person changes
 * its statement, reference or brute force (store.go Update), so for one the
 * statement counts too. Empty when a save keeps the validation.
 */
export function judgedFieldsChanged(saved: Draft, now: Draft, aiDraft = false): string[] {
  const out: string[] = [];
  if (aiDraft && saved.statement_md !== now.statement_md) out.push('statement');
  if (saved.time_limit_ms !== now.time_limit_ms || saved.memory_limit_mb !== now.memory_limit_mb) out.push('limits');
  if (saved.checker !== now.checker || saved.epsilon !== now.epsilon) out.push('checker');
  else if (now.checker === 'custom' && saved.checker_source !== now.checker_source) out.push('checker');
  if (programKey(saved.reference) !== programKey(now.reference)) out.push('reference solution');
  if (programKey(saved.brute) !== programKey(now.brute)) out.push('brute force');
  if (programKey(saved.validator) !== programKey(now.validator)) out.push('validator');
  const list = (text: string) => {
    const r = parseTestList(text);
    return r.ok ? r.tests.join(',') : text;
  };
  if (list(saved.samples) !== list(now.samples)) out.push('examples');
  const subtasks = (d: Draft) =>
    d.subtasks
      .map((s) => {
        const r = parseTestList(s.tests);
        return `${s.points ?? ''}:${r.ok ? r.tests.join(',') : s.tests}`;
      })
      .join('|');
  if (subtasks(saved) !== subtasks(now)) out.push('subtasks');
  return out;
}

/** Joins words as "a, b and c". */
export function wordList(words: readonly string[]): string {
  if (words.length <= 1) return words.join('');
  return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
}

/**
 * The validation's result in one line, for the sticky bar and the readiness
 * panel: "Validation passed: 6 of 6 tests, slowest 11 ms." or "Validation
 * failed on test 5." Null while the judge has it, or with no validation.
 */
export function validationResultLine(v: Validation | null): { tone: 'ok' | 'bad'; text: string } | null {
  if (!v || isPending(v)) return null;
  if (v.state === 'passed') {
    const s = v.summary;
    if (!s) return { tone: 'ok', text: 'Validation passed.' };
    const n = s.reference_total;
    return { tone: 'ok', text: `Validation passed: ${s.reference_passed} of ${n} ${n === 1 ? 'test' : 'tests'}, slowest ${s.max_time_ms} ms.` };
  }
  if (v.state === 'error') return { tone: 'bad', text: 'Validation could not finish.' };
  const first = v.tests.filter((t) => isFailure(t.verdict)).sort((a, b) => a.idx - b.idx)[0];
  if (first) return { tone: 'bad', text: `Validation failed on test ${first.idx}.` };
  if (v.compile.some((c) => c.verdict === 'CE')) return { tone: 'bad', text: 'Validation failed: a program did not compile.' };
  return { tone: 'bad', text: 'Validation failed.' };
}

/** How many images in a statement have no description (alt text). */
export function imagesWithoutAlt(markdown: string): number {
  return (markdown.match(/!\[\s*\]\([^)]*\)/g) ?? []).length;
}

type JobState = { state: string } | null;

/**
 * The chain after the AI wizard or Generate inputs in one line, while its
 * inputs or outputs are being made: "Inputs generated, outputs generating,
 * validation waiting". Empty otherwise, a validation on its own included.
 */
export function pipelineLine(inputs: JobState, outputs: JobState, validation: JobState): string {
  const busy = (j: JobState) => j !== null && (j.state === 'queued' || j.state === 'running');
  // A validation on its own is said by the status; the line is for the chain.
  if (!busy(inputs) && !busy(outputs)) return '';
  const word = (j: JobState, done: string, running: string) =>
    j === null ? 'waiting' : j.state === 'queued' ? 'waiting' : j.state === 'running' ? running : done;
  const parts: string[] = [];
  if (inputs !== null) parts.push(`Inputs ${word(inputs, 'generated', 'generating')}`);
  const outputsWord = busy(inputs) ? 'waiting' : word(outputs, 'generated', 'generating');
  parts.push(`${parts.length === 0 ? 'Outputs' : 'outputs'} ${outputsWord}`);
  const validationWord = busy(inputs) || busy(outputs) ? 'waiting' : word(validation, 'done', 'running');
  parts.push(`validation ${validationWord}`);
  return parts.join(', ');
}

/**
 * The samples the editor's preview shows, as the samples field is typed
 * (falling back to the saved sample flags while it cannot be read): each
 * with the stored preview of its input and output, the first 200 bytes.
 * `cut` is true when any file is longer than its preview.
 */
export function previewSamples(tests: readonly TestCase[], samplesText: string): { samples: { idx: number; input: string; output: string; truncated: boolean }[]; cut: boolean } {
  const typed = parseTestList(samplesText);
  const chosen = tests.filter((t) => (typed.ok ? typed.tests.includes(t.idx) : t.sample));
  return {
    samples: chosen.map((t) => ({ idx: t.idx, input: t.input_preview, output: t.output_preview, truncated: false })),
    cut: chosen.some((t) => t.input_size > t.input_preview.length || t.output_size > t.output_preview.length),
  };
}

/** Sorted test numbers in range form, as subtask ranges are written: "1-3, 5". */
export function rangeText(nums: readonly number[]): string {
  const sorted = [...nums].sort((a, b) => a - b);
  const parts: string[] = [];
  for (let i = 0; i < sorted.length; ) {
    let j = i;
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++;
    parts.push(j === i ? String(sorted[i]) : `${sorted[i]}-${sorted[j]}`);
    i = j + 1;
  }
  return parts.join(', ');
}
