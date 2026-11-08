/**
 * The AI prompt editor's data shapes (GET /api/admin/ai/prompts) and the
 * small pure helpers behind it. The server owns every rule; these only
 * word what it sent.
 */
import { formatDateTime } from './format';

export type PromptKind = 'wizard_rules' | 'pack_rules' | 'preferences';

/** One saved row. `by` is the saver's email, "" when unknown. */
export interface PromptVersion {
  id: number;
  kind: PromptKind;
  body: string;
  is_reset: boolean;
  default_sha: string;
  by: string;
  at: number;
  default_changed?: boolean;
}

/** The text a kind uses now and where it comes from. */
export interface ActivePrompt {
  text: string;
  source: 'database' | 'file' | 'built-in';
  version?: PromptVersion;
  file_sha?: string;
}

/** One kind as the editor shows it. */
export interface PromptKindData {
  kind: PromptKind;
  label: string;
  can_edit: boolean;
  active: ActivePrompt;
  default: string;
  default_changed: boolean;
  /** The part of the prompt nobody can edit, shown read-only. */
  fixed: string;
  history: PromptVersion[];
  /** Longest body: bytes for rules, characters for preferences. */
  limit: number;
}

export interface PromptsData {
  kinds: PromptKindData[];
  server_now?: number;
}

/** A full prompt a dry run returns. */
export interface PromptPreview {
  label: string;
  text: string;
}

export const STALE_CODE = 'stale_version';
export const DEFAULT_CHANGED_WARNING = 'The default changed since this version was saved.';
export const PREFERENCES_EXAMPLE = 'Use short variable names. Give every problem a story of one sentence. Prefer C++ solutions that avoid macros.';

/** "Last changed by priya@college.edu on 8 Nov 2026, 14:32", or the no-change line. */
export function lastChanged(v: PromptVersion | undefined): string {
  if (!v) return 'Never changed.';
  return `${v.is_reset ? 'Reset' : 'Last changed'} by ${v.by || 'someone whose account was erased'} on ${formatDateTime(v.at)}`;
}

/** Where the active text comes from, in one line. */
export function sourceLine(a: ActivePrompt): string {
  if (a.source === 'database' && a.version) return `Saved in the database as version ${a.version.id} by ${a.version.by || 'an erased account'} on ${formatDateTime(a.version.at)}.`;
  if (a.source === 'file') return `From the rules file on the server (sha256 ${(a.file_sha ?? '').slice(0, 12)}).`;
  return 'The built-in text.';
}

/** The counter under the Preferences box. */
export function counter(text: string, limit: number): string {
  return `${[...text].length} of ${limit.toLocaleString('en-US')} characters`;
}
