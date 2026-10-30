/**
 * Field errors of the problem editor (split out of ProblemEditor in v2 P3
 * F5.1): which control a server or client error key belongs to, its label
 * in the error list, and the aria-describedby of a field. Keys are the
 * editor's own (lib/problems.ts fieldKey maps the server's onto them).
 */
import type { Draft } from './problems';

/** The label of each top-level field in the error list. */
export const FIELD_LABELS: Record<string, string> = {
  title: 'Title',
  slug: 'Slug',
  statement_md: 'Statement',
  time_limit_ms: 'Time limit',
  memory_limit_mb: 'Memory',
  checker: 'Checker',
  epsilon: 'Allowed error',
  checker_source: 'Checker source',
  tags: 'Tags',
  difficulty: 'Difficulty',
  samples: 'Examples',
  subtasks: 'Subtasks',
  reference: 'Reference solution',
  brute: 'Brute force',
  validator: 'Validator',
  generator: 'Generator',
  plan: 'Planned tests',
};

const PLAN_PARTS: Record<string, string> = { idx: 'test', subtask: 'subtask', seed: 'seed', args: 'arguments' };

/** One error with the id of its control (null when the control is not on the page) and its label. */
export interface FieldError {
  key: string;
  message: string;
  id: string | null;
  label: string;
}

/** The control and label of an error key, given the working copy as it stands. */
export function fieldTarget(key: string, draft: Draft | null, hasGenerator: boolean): { id: string | null; label: string } {
  const args = /^subtasks\.(\d+)\.generator_args$/.exec(key);
  if (args) {
    const i = Number(args[1]);
    const ok = draft !== null && i < draft.subtasks.length && hasGenerator;
    return { id: ok ? `subtask-${i}-args` : null, label: `Subtask ${i + 1} generator arguments` };
  }
  const planRow = /^plan\.(\d+)\.(idx|subtask|seed|args)$/.exec(key);
  if (planRow) {
    const i = Number(planRow[1]);
    const ok = draft?.plan !== undefined && i < draft.plan.length;
    return { id: ok ? `plan-${i}-${planRow[2]}` : null, label: `Planned test ${i + 1} ${PLAN_PARTS[planRow[2]]}` };
  }
  if (key === 'plan') return { id: hasGenerator ? 'add-plan-row' : null, label: FIELD_LABELS.plan };
  const generator = /^generator(?:\.(\w+))?$/.exec(key);
  if (generator) {
    const part = generator[1] === 'language' ? 'language' : 'source';
    return { id: draft?.generator ? `generator-${part}` : null, label: FIELD_LABELS.generator };
  }
  const row = /^subtasks\.(\d+)\.(points|title|tests)$/.exec(key);
  if (row) {
    const i = Number(row[1]);
    const ok = draft !== null && i < draft.subtasks.length;
    // A subtask's tests are chosen in the tests table's Role column.
    if (row[2] === 'tests') return { id: 'tests-heading', label: `Subtask ${i + 1} tests` };
    return { id: ok ? `subtask-${i}-${row[2]}` : null, label: `Subtask ${i + 1} ${row[2]}` };
  }
  if (key === 'samples') return { id: 'tests-heading', label: FIELD_LABELS.samples };
  if (key === 'slug') return { id: null, label: FIELD_LABELS.slug };
  const program = /^(reference|brute|validator)(?:\.(\w+))?$/.exec(key);
  if (program) {
    const role = program[1] as 'reference' | 'brute' | 'validator';
    const present = role === 'reference' || (draft !== null && draft[role] !== null);
    const part = program[2] === 'language' ? 'language' : 'source';
    return { id: present ? `${role}-${part}` : null, label: FIELD_LABELS[role] };
  }
  if (key === 'subtasks') return { id: 'add-subtask', label: FIELD_LABELS.subtasks };
  if (key === 'epsilon' && draft?.checker !== 'float') return { id: null, label: FIELD_LABELS.epsilon };
  if (key === 'checker_source' && draft?.checker !== 'custom') return { id: null, label: FIELD_LABELS.checker_source };
  if (FIELD_LABELS[key]) return { id: key, label: FIELD_LABELS[key] };
  return { id: null, label: '' };
}

/** Every error, in the order the server or the client gave them, with its control. */
export function fieldErrors(fields: Record<string, string>, draft: Draft | null, hasGenerator: boolean): FieldError[] {
  return Object.entries(fields).map(([key, message]) => ({ key, message, ...fieldTarget(key, draft, hasGenerator) }));
}

/** The error for a program, whichever of its keys the server used. */
export function programError(fields: Record<string, string>, role: 'reference' | 'brute' | 'validator' | 'generator'): string {
  const key = Object.keys(fields).find((k) => k === role || k.startsWith(`${role}.`));
  return key ? fields[key] : '';
}

/** The aria-describedby of a field: its help, then its error when it has one. */
export function describedBy(fields: Record<string, string>, key: string, help = ''): string | undefined {
  const ids = [help, fields[key] ? `${key.replace(/\./g, '-')}-error` : ''].filter((s) => s !== '');
  return ids.length > 0 ? ids.join(' ') : undefined;
}

/** The errors of one planned test row, in column order. */
export function planErrors(fields: Record<string, string>, i: number): { key: string; message: string }[] {
  return ['idx', 'subtask', 'seed', 'args']
    .map((part) => ({ key: `plan.${i}.${part}`, message: fields[`plan.${i}.${part}`] ?? '' }))
    .filter((e) => e.message !== '');
}
