/**
 * The AI wizard's data shapes and the pure helpers behind its screens
 * (cache/v1/m5-plan.md sections 1, 4 and 7).
 *
 * The provider key is the one secret the web app ever handles. It lives in
 * a password field until it is sent with PUT /api/admin/ai/key, and
 * saveKey() hands back a cleared form whatever the answer, so the key is in
 * no state the page keeps afterwards. It is never written to storage, a
 * URL, the console or a message: an error that somehow carries the key has
 * it replaced by redactKey() before anyone sees it.
 */
import { ApiError } from './api';
import { verdictLabel, type PlannedTest } from './problems';
import type { Submission } from './workspace';

/** One model a provider offers, as GET /api/admin/ai/key lists it. */
export interface ModelOption {
  id: string;
  name: string;
}

/** A provider the server can use; "fake" exists only in testing mode. */
export interface ProviderOption {
  id: string;
  name: string;
  models: ModelOption[];
}

/** GET, and the answer to PUT, /api/admin/ai/key. Never holds the key. */
export interface KeyStatus {
  active: boolean;
  provider: string;
  model: string;
  /** Unix ms when the server forgets the key, or null. */
  active_until: number | null;
  providers: ProviderOption[];
  server_now?: number;
}

/** The key form as the settings page holds it. */
export interface KeyForm {
  provider: string;
  model: string;
  key: string;
}

/** The model the plan makes the default (decision in m5-plan.md section 6). */
export const DEFAULT_MODEL = 'claude-opus-5-5';

/** The provider that needs no key. */
export const FAKE_PROVIDER = 'fake';

/** Pasted text and briefs are refused past this many characters. */
export const MAX_TEXT = 20_000;

/** How long the server keeps a key, in words, for the settings page. */
export const KEY_HOURS = 2;

/** True when the provider is the testing-mode fake, which needs no key. */
export function needsKey(provider: string): boolean {
  return provider !== FAKE_PROVIDER;
}

/** Accepts Unix seconds as well as milliseconds, in case a server sends seconds. */
function toMs(t: number): number {
  return t < 1e11 ? t * 1000 : t;
}

/** "14:32" in local time. */
export function formatClock(t: number): string {
  const d = new Date(toMs(t));
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** A model's name without the "(default)" note the list carries. */
export function modelLabel(status: Pick<KeyStatus, 'providers'>, provider: string, model: string): string {
  const p = status.providers.find((x) => x.id === provider);
  const m = p?.models.find((x) => x.id === model);
  return (m?.name ?? model).replace(/\s*\(default\)\s*$/i, '');
}

/** A provider's name as the list gives it. */
export function providerLabel(status: Pick<KeyStatus, 'providers'>, provider: string): string {
  return status.providers.find((x) => x.id === provider)?.name ?? provider;
}

/** "Key active until 14:32", or what to do when there is no key. */
export function keyLine(status: Pick<KeyStatus, 'active' | 'active_until' | 'provider'>): string {
  if (!status.active) return 'No key yet';
  if (!needsKey(status.provider)) return 'Fake provider active, no key needed';
  return status.active_until !== null ? `Key active until ${formatClock(status.active_until)}` : 'Key active';
}

/**
 * The form a page starts with: the active provider and model, or the first
 * provider with its default model. The key is always empty.
 */
export function initialForm(status: KeyStatus): KeyForm {
  if (status.active && status.providers.some((p) => p.id === status.provider)) {
    return { provider: status.provider, model: status.model, key: '' };
  }
  const p = status.providers[0];
  return { provider: p?.id ?? '', model: defaultModel(p), key: '' };
}

/** The provider's default model: Opus 5.5 when it is offered, else the first. */
export function defaultModel(p: ProviderOption | undefined): string {
  if (!p) return '';
  return p.models.find((m) => m.id === DEFAULT_MODEL)?.id ?? p.models[0]?.id ?? '';
}

/** Replaces every copy of the key in a sentence with "[redacted]". */
export function redactKey(text: string, key: string): string {
  const k = key.trim();
  if (k.length < 4) return text;
  return text.split(k).join('[redacted]');
}

/** The result of saving a key: the new status or a sentence, and always an empty key. */
export type SaveKeyResult =
  | { ok: true; status: KeyStatus; form: KeyForm }
  | { ok: false; message: string; code: string; status: number; form: KeyForm };

/**
 * Sends the key form with `put` (PUT /api/admin/ai/key) and returns what
 * the page should keep. The key is cleared from the returned form whether
 * or not the server took it, and any error sentence is redacted.
 */
export async function saveKey(
  put: (body: { provider: string; model: string; key: string }) => Promise<KeyStatus>,
  form: KeyForm,
): Promise<SaveKeyResult> {
  const key = needsKey(form.provider) ? form.key.trim() : '';
  const cleared: KeyForm = { provider: form.provider, model: form.model, key: '' };
  try {
    const status = await put({ provider: form.provider, model: form.model, key });
    return { ok: true, status, form: cleared };
  } catch (e) {
    const message = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
    return {
      ok: false,
      message: redactKey(message, key),
      code: e instanceof ApiError ? e.code : 'unexpected',
      status: e instanceof ApiError ? e.status : 0,
      form: cleared,
    };
  }
}

/** Checks the key form before it is sent; returns field errors by name. */
export function checkKeyForm(form: KeyForm): Record<string, string> {
  const fields: Record<string, string> = {};
  if (form.provider === '') fields.provider = 'Choose a provider.';
  if (form.model === '') fields.model = 'Choose a model.';
  if (needsKey(form.provider) && form.key.trim() === '') fields.key = 'Paste your API key.';
  return fields;
}

export type Difficulty = 'easy' | 'medium' | 'hard';

export const WIZARD_DIFFICULTIES: readonly { id: Difficulty; label: string }[] = [
  { id: 'easy', label: 'Easy' },
  { id: 'medium', label: 'Medium' },
  { id: 'hard', label: 'Hard' },
];

/** The brief as the wizard form holds it. */
export interface Brief {
  topic: string;
  difficulty: Difficulty;
  constraints: string;
  subtasks: string;
}

/** The body of POST /api/admin/ai/wizard. */
export type WizardRequest = { mode: 'brief'; brief: Brief } | { mode: 'paste'; text: string };

export type WizardRequestResult = { ok: true; body: WizardRequest } | { ok: false; fields: Record<string, string> };

/** Builds the wizard request from the form, refusing what cannot be sent. */
export function wizardRequest(mode: 'brief' | 'paste', brief: Brief, text: string): WizardRequestResult {
  const fields: Record<string, string> = {};
  if (mode === 'paste') {
    const t = text.trim();
    if (t === '') fields.text = 'Paste the problem you want rewritten.';
    else if (t.length > MAX_TEXT) fields.text = `Paste at most ${MAX_TEXT.toLocaleString('en-GB')} characters. This text has ${t.length.toLocaleString('en-GB')}.`;
    return Object.keys(fields).length > 0 ? { ok: false, fields } : { ok: true, body: { mode: 'paste', text: t } };
  }
  const b: Brief = {
    topic: brief.topic.trim(),
    difficulty: brief.difficulty,
    constraints: brief.constraints.trim(),
    subtasks: brief.subtasks.trim(),
  };
  if (b.topic === '') fields.topic = 'Say what the problem is about, for example "shortest path with one free edge".';
  const total = b.topic.length + b.constraints.length + b.subtasks.length;
  if (total > MAX_TEXT) fields.constraints = `Keep the brief under ${MAX_TEXT.toLocaleString('en-GB')} characters in all.`;
  return Object.keys(fields).length > 0 ? { ok: false, fields } : { ok: true, body: { mode: 'brief', brief: b } };
}

export type WizardState = 'thinking' | 'draft_created' | 'error' | 'done';

/** GET /api/admin/ai/wizard/{id}, and the `wizard` live event. */
export interface Wizard {
  id: string;
  state: WizardState | string;
  error?: { code: string; message: string; reasons?: string[] } | null;
  problem_id?: number | null;
  /** Set on a solve request once the model's answer was submitted. */
  submission_id?: number | null;
}

/** Error codes that mean the key is missing or no longer works, so the page links to the settings. */
export function isKeyProblem(code: string | undefined): boolean {
  return code === 'no_key' || code === 'key_refused';
}

/** True while the model is still writing. */
export function wizardPending(w: Pick<Wizard, 'state' | 'submission_id'> | null): boolean {
  return w !== null && w.state === 'thinking' && (w.submission_id ?? null) === null;
}

/** One step of the wizard's progress list. */
export interface WizardStep {
  text: string;
  state: 'done' | 'current' | 'waiting' | 'failed';
}

/** The two steps the wizard page shows, from a wizard's state. */
export function wizardSteps(w: Pick<Wizard, 'state'>): WizardStep[] {
  const failed = w.state === 'error';
  const created = w.state === 'draft_created' || w.state === 'done';
  return [
    { text: 'Writing the problem', state: failed ? 'failed' : created ? 'done' : 'current' },
    { text: 'Draft created, generating tests', state: created ? 'current' : 'waiting' },
  ];
}

/** The live sentence for a wizard's state. */
export function wizardStatusText(w: Pick<Wizard, 'state'>): string {
  if (w.state === 'thinking') return 'Writing the problem';
  if (w.state === 'draft_created' || w.state === 'done') return 'Draft created, generating tests';
  if (w.state === 'error') return 'The model could not write this problem';
  return '';
}

/** API paths of the wizard. */
export const aiPaths = {
  key: '/api/admin/ai/key',
  wizard: '/api/admin/ai/wizard',
  wizardOne: (id: string | number) => `/api/admin/ai/wizard/${encodeURIComponent(String(id))}`,
};

/** A generate inputs run: POST .../generate-inputs and GET .../input-generation. */
export interface InputGeneration {
  id: number;
  state: 'queued' | 'running' | 'done' | 'error' | string;
  planned: number;
  results: { idx: number; verdict: string; message?: string }[];
  summary?: { applied: number; generating_outputs?: boolean; problems: string[] } | null;
  error?: string | null;
  /** The generator's compiler output; with no results it means the compile failed. */
  compile_output?: string;
}

/** True while the judge has the run. */
export function inputGenPending(g: Pick<InputGeneration, 'state'> | null): boolean {
  return g !== null && (g.state === 'queued' || g.state === 'running');
}

/** What an input generation reads as: a headline, its tone and named failures. */
export interface InputGenReport {
  tone: 'ok' | 'bad' | 'wait';
  text: string;
  lines: string[];
}

/**
 * Turns an input generation into the editor's lines. A failure names the
 * test, its subtask and seed from the plan: "The generator failed on test 7
 * (subtask 2, seed 3): Runtime error".
 */
export function inputGenReport(g: InputGeneration, plan: readonly PlannedTest[]): InputGenReport {
  if (inputGenPending(g)) {
    const done = g.results.filter((r) => r.verdict !== '').length;
    return {
      tone: 'wait',
      text: g.planned > 0 && done > 0 ? `Generating inputs, ${done} of ${g.planned}` : 'Generating inputs',
      lines: [],
    };
  }
  if (g.error) return { tone: 'bad', text: g.error, lines: [] };
  if (g.results.length === 0 && g.compile_output && g.compile_output.trim() !== '') {
    return {
      tone: 'bad',
      text: 'The generator did not compile, so no inputs were generated.',
      lines: g.compile_output.split('\n').filter((l) => l.trim() !== '').slice(0, 8),
    };
  }
  const failed = g.results.filter((r) => r.verdict !== '' && r.verdict !== 'OK' && r.verdict !== 'AC');
  const lines = failed.map((r) => {
    const t = plan.find((p) => p.idx === r.idx);
    const where = t ? ` (subtask ${t.subtask}, seed ${t.seed})` : '';
    const why = r.message && r.message.trim() !== '' ? r.message.trim() : verdictLabel(r.verdict);
    return `The generator failed on test ${r.idx}${where}: ${why}`;
  });
  const problems = g.summary?.problems ?? [];
  for (const p of problems) if (!lines.includes(p)) lines.push(p);
  if (lines.length > 0) {
    return {
      tone: 'bad',
      text: failed.length > 0 ? 'Some inputs could not be generated, so none were applied.' : 'The inputs were not applied.',
      lines,
    };
  }
  const n = g.summary?.applied ?? g.results.length;
  const next = g.summary?.generating_outputs ? ' Outputs are being generated from the reference solution, then the problem is validated.' : '';
  return { tone: 'ok', text: `Generated ${n} ${n === 1 ? 'input' : 'inputs'}.${next}`, lines: [] };
}

/** True when the problem carries the AI draft tag. */
export function isAIDraft(tags: readonly string[]): boolean {
  return tags.some((t) => t.trim().toLowerCase() === 'ai draft');
}

/** Tags without the AI draft tag, which the editor shows apart. */
export function withoutAIDraft(tags: readonly string[]): string[] {
  return tags.filter((t) => t.trim().toLowerCase() !== 'ai draft');
}

/** "A model solved this zero-shot: 100 of 100". */
export function solveLine(s: Pick<Submission, 'score' | 'max_score'>): string {
  return `A model solved this zero-shot: ${s.score} of ${s.max_score}`;
}

/** What a model's score means for a graded contest, in one or two sentences. */
export function solveMeaning(s: Pick<Submission, 'score' | 'max_score'>): string {
  if (s.max_score > 0 && s.score >= s.max_score) {
    return 'Anyone who pastes the statement into a model gets full marks. Keep this problem for practice, or change it before using it in a graded contest.';
  }
  if (s.score > 0) {
    return 'A model gets part of the marks without help. In a graded contest, expect some students to reach at least this score that way.';
  }
  return 'The model scored nothing, so pasting the statement into one does not give the answer today. Models improve, so check again before each graded contest.';
}
