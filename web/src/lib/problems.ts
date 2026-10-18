/**
 * The problem bank's data shapes and the pure helpers behind the admin
 * screens (cache/v1/m2-plan.md sections 4 and 8, docs/API.md).
 *
 * The editor works on a Draft: the editable part of a Problem with tags and
 * sample tests held as the text the person typed. toDraft() builds one from
 * a Problem and toEdit() turns it back into the ProblemEdit body for PUT,
 * reporting input it cannot read as field errors keyed like the server's.
 * The rest turns a Validation into the plain sentences the report shows.
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
export const CHECKERS: readonly { id: Checker; label: string }[] = [
  { id: 'tokens', label: 'Tokens (ignores whitespace)' },
  { id: 'exact', label: 'Exact' },
  { id: 'float', label: 'Floating point' },
  { id: 'custom', label: 'Custom (testlib)' },
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
}

/** GET /api/admin/problems/{id}. */
export interface Problem extends ProblemSummary {
  statement_md: string;
  time_limit_ms: number;
  memory_limit_mb: number;
  checker: Checker;
  epsilon: number;
  checker_source: string;
  reference: Program;
  brute: Program | null;
  validator: Program | null;
  samples: number[];
  subtasks: Subtask[];
  tests: TestCase[];
  content_hash: string;
  validated_hash: string | null;
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
  subtasks: { points: number; title: string; tests: string }[];
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
  subtasks: { points: number | null; title: string; tests: string }[];
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
    validator_passed: number;
    validator_total: number;
    max_time_ms: number;
    time_limit_ms: number;
    problems: string[];
    warnings: string[];
  } | null;
  error: string | null;
}

/** True while the judge has the validation queued or running. */
export function isPending(v: Pick<Validation, 'state'> | null): boolean {
  return v !== null && (v.state === 'queued' || v.state === 'running');
}

/**
 * Suggests a slug for a title: lower case ASCII letters, digits and single
 * hyphens, at most 64 characters.
 */
export function slugify(title: string): string {
  return title
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
    .replace(/-+$/g, '');
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

/** Builds the editor's working copy from a problem. */
export function toDraft(p: Problem): Draft {
  return {
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
    tags: p.tags.join(', '),
    difficulty: p.difficulty,
    samples: formatTestList(p.samples),
    subtasks: p.subtasks.map((s) => ({ points: s.points, title: s.title, tests: s.tests })),
  };
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
    else if (range.tests.length === 0) fields[`subtasks.${i}.tests`] = 'Enter the tests in this subtask, for example 3-5.';
    return { points, title: s.title.trim(), tests: s.tests.trim() };
  });

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

/** What happened to a role on a test, as the end of a sentence. */
function outcome(role: Role, verdict: string): string {
  switch (verdict) {
    case 'WA':
      return role === 'validator' ? 'rejected the input' : 'got wrong answer';
    case 'TLE':
      return 'exceeded the time limit';
    case 'MLE':
      return 'exceeded the memory limit';
    case 'RE':
      return 'had a runtime error';
    case 'OLE':
      return 'exceeded the output limit';
    case 'CE':
      return 'did not compile';
    case 'IE':
      return role === 'checker' ? 'failed' : 'hit an internal error in the judge';
    default:
      return `got ${verdictLabel(verdict).toLowerCase()}`;
  }
}

/** "Test 7: reference solution got wrong answer". */
export function failureSentence(t: { idx: number; role: Role; verdict: string }): string {
  return `Test ${t.idx}: ${roleName(t.role)} ${outcome(t.role, t.verdict)}`;
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

/** One line of the validation report. */
export interface ReportLine {
  text: string;
  /** ok and bad are drawn in the verdict colours; note is muted. */
  tone: 'ok' | 'bad' | 'note';
  /** Program output shown verbatim under the line, in mono. */
  detail?: string;
}

/**
 * The plain list a finished (or partly finished) validation reads as: one
 * line per compile failure, one per check in the summary, one per failing
 * test with its message, then the server's problems and warnings.
 */
export function reportLines(v: Validation): ReportLine[] {
  const lines: ReportLine[] = [];
  for (const c of v.compile) {
    if (c.verdict === 'CE') lines.push({ text: `${capitalise(roleName(c.role))} did not compile`, tone: 'bad', detail: c.output });
    else if (isFailure(c.verdict)) {
      lines.push({ text: `${capitalise(roleName(c.role))}: ${verdictLabel(c.verdict).toLowerCase()} while compiling`, tone: 'bad', detail: c.output || undefined });
    }
  }
  const s = v.summary;
  if (s) {
    if (s.reference_total > 0) {
      lines.push({
        text: `Reference solution passed ${s.reference_passed} of ${s.reference_total} tests`,
        tone: s.reference_passed === s.reference_total ? 'ok' : 'bad',
      });
    }
    if (s.brute_total > 0) {
      lines.push({
        text: `Brute force agreed on ${s.brute_agreed} of ${s.brute_total} tests`,
        tone: s.brute_agreed === s.brute_total ? 'ok' : 'bad',
      });
    }
    if (s.validator_total > 0) {
      lines.push({
        text: `Validator accepted ${s.validator_passed} of ${s.validator_total} inputs`,
        tone: s.validator_passed === s.validator_total ? 'ok' : 'bad',
      });
    }
    if (s.reference_total > 0) {
      lines.push({
        text: `Slowest reference run ${s.max_time_ms} ms of ${s.time_limit_ms} ms`,
        tone: s.max_time_ms * 2 <= s.time_limit_ms ? 'ok' : 'bad',
      });
    }
  }
  // While the run is going, list failures as they arrive; once the server
  // has summarised, its problems say the same thing, so show only those.
  if (!s) {
    for (const t of v.tests) {
      if (isFailure(t.verdict)) lines.push({ text: failureSentence(t), tone: 'bad', detail: t.message || undefined });
    }
  }
  if (s) {
    for (const p of s.problems) lines.push({ text: p, tone: 'bad' });
    for (const w of s.warnings) lines.push({ text: w, tone: 'note' });
  }
  return lines;
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
