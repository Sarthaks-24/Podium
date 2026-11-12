/**
 * Language checks of a function problem (v2.1.0 stage F): the data shapes of
 * GET /api/admin/problems/{id}/proof and of the two language routes, and the
 * words the wizard and the problem page use for them. The server decides what
 * a result is; this file only says it in plain words. Nothing here holds a
 * hidden test or a provider key.
 */
import { ApiError, errorText } from './api';
import { problemPath } from './problems';

/** One failed test of a language check: its number and verdict, nothing else. */
export interface ProofTest {
  idx: number;
  verdict: string;
}

/** What is known about one language of a function problem. */
export interface ProofLanguage {
  language: string;
  /** Live: the language has locked code now. */
  offered: boolean;
  result: 'passed' | 'failed' | 'unchecked' | 'none' | string;
  reason: string;
  failed: number;
  total: number;
  compiler: string;
  tests: ProofTest[];
  /** False when the result is from other content than the problem has now. */
  current: boolean;
  source: 'wizard' | 'fix' | 'check' | string;
}

/** The jobs of the open round. */
export interface ProofProgress {
  done: number;
  total: number;
  /** The language of a running job, or "". */
  current: string;
}

/** What the newest finished round did. */
export interface ProofApplied {
  kept?: 'reference_failed' | 'reference_unchecked' | 'not_added' | string;
  discarded?: 'gone' | 'changed' | string;
  removed?: string[];
  added?: string;
  stale?: string[];
  validating: boolean;
  error?: string;
  hash?: string;
}

/** GET /api/admin/problems/{id}/proof. */
export interface ProofState {
  checking: boolean;
  progress?: ProofProgress;
  /** The four languages in order, or empty for a whole-program problem. */
  languages: ProofLanguage[];
  applied?: ProofApplied;
  server_now?: number;
}

/** The one sentence for a problem whose languages are being checked. */
export const CHECKING_TEXT = 'Podium is checking the languages of this problem. This takes a few minutes.';

/** API paths of the language checks. */
export const proofPaths = {
  state: (id: number | string) => problemPath(id, '/proof'),
  check: (id: number | string, language: string) => problemPath(id, `/languages/${encodeURIComponent(language)}/check`),
  fix: (id: number | string, language: string) => problemPath(id, `/languages/${encodeURIComponent(language)}/ai-fix`),
};

const SHORT: Record<string, string> = { cpp17: 'C++', java21: 'Java', python3: 'Python', node22: 'JavaScript' };
const LONG: Record<string, string> = { cpp17: 'C++17', java21: 'Java 21', python3: 'Python 3', node22: 'JavaScript (Node 22)' };

/** A language's short name, as the page says it. */
export function languageName(id: string): string {
  return SHORT[id] ?? id;
}

/** The names of the offered languages, in the server's order. */
export function offeredNames(state: Pick<ProofState, 'languages'>): string[] {
  return state.languages.filter((l) => l.offered).map((l) => languageName(l.language));
}

/** The languages that are not offered. */
export function notOffered(state: Pick<ProofState, 'languages'>): ProofLanguage[] {
  return state.languages.filter((l) => !l.offered);
}

const IGNORES = "The locked code passes without the student's function.";

function testCount(failed: number, total: number): string {
  return `${failed} of ${total} ${total === 1 ? 'test' : 'tests'}`;
}

/** Why a language failed its check, in a sentence, or "" when it never ran. */
export function failureText(l: ProofLanguage): string {
  const name = languageName(l.language);
  if (l.result === 'unchecked') return 'It could not be checked.';
  if (l.result !== 'failed') return '';
  if (l.reason === 'ignores_function') return IGNORES;
  if (l.reason === 'compile_error') return `The AI's ${name} code did not compile.`;
  return l.total > 0 ? `The AI's ${name} code failed ${testCount(l.failed, l.total)}.` : `The AI's ${name} code failed its check.`;
}

/** The "Not offered: Java. ..." line of the wizard's result, with the way forward. */
export function notOfferedLine(l: ProofLanguage): string {
  const name = languageName(l.language);
  if (l.result === 'none') return `Not offered: ${name}. The AI did not write it.`;
  if (l.result === 'unchecked') return `Not offered: ${name}. ${failureText(l)} Open the draft and press Fix ${name} with AI.`;
  return `Not offered: ${name}. ${failureText(l)} Open the draft to fix it.`;
}

/** The last check of an offered language, one line, or "Not checked yet" with the way to check it. */
export function lastCheckLine(l: ProofLanguage): string {
  const name = languageName(l.language);
  if (l.result === 'none') return `Not checked yet. Press Check ${name} before a contest.`;
  if (l.result === 'unchecked') return `The last check could not be run. Press Check ${name} to try again.`;
  if (l.result === 'passed') return l.current ? 'Passes every test.' : `Passed before the last change. Press Check ${name} again.`;
  if (l.reason === 'ignores_function') return IGNORES;
  if (l.reason === 'compile_error') return 'Did not compile in the last check.';
  if (l.total === 0) return `The last check could not be run. Press Check ${name} to try again.`;
  if (!l.current) return `Failed before the last change. Press Check ${name} again.`;
  return `Failed ${testCount(l.failed, l.total)} in the last check.`;
}

/** The result line of a finished Check or Fix: "Java passes every test." */
export function checkResultLine(l: ProofLanguage): string {
  const name = languageName(l.language);
  if (l.result === 'passed') return `${name} passes every test.`;
  if (l.result === 'unchecked') return `${name} could not be checked.`;
  if (l.reason === 'compile_error') return `${name} does not compile.`;
  if (l.reason === 'ignores_function') return `${name}: ${IGNORES}`;
  if (l.total === 0) return `${name} could not be checked.`;
  return `${name} fails ${testCount(l.failed, l.total)}.`;
}

/** The status line while a round runs: "Checking Java, 2 of 4." */
export function checkingLine(state: Pick<ProofState, 'progress' | 'languages'>): string {
  const current = state.progress?.current ?? '';
  if (current === '') return 'Checking the languages.';
  const offered = state.languages.filter((l) => l.offered);
  const at = offered.findIndex((l) => l.language === current);
  return at < 0 ? `Checking ${languageName(current)}.` : `Checking ${languageName(current)}, ${at + 1} of ${offered.length}.`;
}

/** The banner for a draft whose round did not count (its own solution failed, it was changed or deleted, the save failed), or "". */
export function referenceBanner(state: Pick<ProofState, 'applied'> | null): string {
  const a = state?.applied;
  if (a?.discarded === 'changed') return 'The draft was changed while its languages were being checked, so the result was not used. Open the draft and press Check for each language.';
  if (a?.discarded === 'gone') return 'The draft was deleted while its languages were being checked.';
  if (a?.error) return `${a.error} Open the draft to see its languages.`;
  const kept = a?.kept;
  if (kept === 'reference_failed') return "The AI's own solution failed its tests, so this draft cannot be validated. Open it to look, or ask again with a simpler description.";
  if (kept === 'reference_unchecked') return "The AI's own solution could not be checked, so this draft cannot be validated. Open it and press Validate.";
  return '';
}

/** True when a round has finished (or at least has results), so the page can stop asking. */
export function roundFinished(state: ProofState): boolean {
  return !state.checking && (state.applied !== undefined || state.languages.some((l) => l.result !== 'none'));
}

/** The copy shown next to the control that caused a refusal. */
export function proofRefusal(e: unknown, language: string): { message: string; code: string; problems: readonly string[] } {
  if (!(e instanceof ApiError)) return { message: errorText(e), code: 'unexpected', problems: [] };
  const long = LONG[language] ?? language;
  const copy: Record<string, string> = {
    not_found: 'There is no such problem.',
    deleted: 'This problem was deleted and cannot be changed.',
    not_function: 'Only a function problem has languages to check.',
    not_offered: `${long} is not offered. Write its locked code and starting code and save, or use Fix ${long} with AI.`,
    already_offered: `${long} is already offered. Use Check ${long} to test it.`,
    checking: 'Podium is still checking the languages of this problem.',
    not_ready: 'The problem is not ready. Fix these first.',
    no_key: 'Add a key first.',
    busy: 'One AI request at a time. Wait for the current one to finish.',
    invalid_answer: `The AI's ${long} code could not be used, so nothing was changed. Press Fix again, or write the code yourself.`,
    changed: 'The problem changed while the AI was working. Press Fix again.',
  };
  return { message: copy[e.code] ?? e.message, code: e.code, problems: e.problems };
}
