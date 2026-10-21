/**
 * The tone of a badge (styles/controls.css "Badges", audit 2026-10-20
 * change 3). A badge carries a verdict or a state word, never anything
 * else; these helpers pick its tint so every screen colours the same word
 * the same way. Contest states are drawn by `.state-word` with
 * data-state (styles/controls.css), not by a Badge tone, so there is one
 * system for them (council C1-030).
 */
import type { Tone as VerdictTone } from './workspace';

export type BadgeTone = VerdictTone | 'act' | 'muted' | 'neutral';

/**
 * A problem's bank status as a tone (audit O6): Validated ok, Not
 * validated partial, an AI draft waiting, Archived muted.
 */
export function problemTone(status: string): BadgeTone {
  if (status === 'Validated') return 'ok';
  if (status === 'Not validated') return 'part';
  if (status === 'AI draft') return 'wait';
  if (status === 'Archived') return 'muted';
  return 'neutral';
}
