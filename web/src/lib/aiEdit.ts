/**
 * AI edit of a problem (v2.0.2 stage E): the data shapes of the three routes
 * and the pure helpers behind the panel. The server keeps the draft in
 * memory; the page only shows it and sends Accept or Reject.
 */
import { lineDiff, type DiffLine } from './diff';

/** The most characters the server takes in an instruction. */
export const MAX_INSTRUCTION = 4000;

/** One file the draft changes or adds, with its text before and after. */
export interface EditFile {
  path: string;
  status: 'changed' | 'added';
  before: string;
  after: string;
}

/** The answer to POST /api/admin/problems/{id}/ai-edit. */
export interface EditDraft {
  draft_id: string;
  summary: string;
  files: EditFile[];
  provider: string;
  model: string;
}

/** The answer to POST .../ai-edit/{draft}/accept. */
export interface EditAccepted {
  problem_id: number;
  validation_queued: boolean;
  not_ready: string[];
}

/** API paths of one problem's AI edit. */
export const editPaths = {
  ask: (id: number) => `/api/admin/problems/${id}/ai-edit`,
  one: (id: number, draft: string) => `/api/admin/problems/${id}/ai-edit/${encodeURIComponent(draft)}`,
  accept: (id: number, draft: string) => `/api/admin/problems/${id}/ai-edit/${encodeURIComponent(draft)}/accept`,
};

/** What the panel checks before it sends: a sentence, or "" when the instruction can go. */
export function checkInstruction(text: string): string {
  if (text.trim() === '') return 'Say what to change.';
  if ([...text].length > MAX_INSTRUCTION) return `Keep the instruction under ${MAX_INSTRUCTION.toLocaleString('en-US')} characters.`;
  return '';
}

/** A file's line diff, or null when it is too long to compare in the browser. */
export function fileDiff(f: EditFile): DiffLine[] | null {
  return lineDiff(f.before, f.after);
}

/** "statement.md and solution.cpp changed", for the line above the diff. */
export function changeLine(files: readonly EditFile[]): string {
  const names = files.map((f) => f.path);
  if (names.length === 0) return 'The draft changes nothing.';
  const list = names.length === 1 ? names[0] : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
  return `${list} would change.`;
}
