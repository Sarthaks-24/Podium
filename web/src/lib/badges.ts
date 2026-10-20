/**
 * The tone of a badge (styles/controls.css "Badges", audit 2026-10-20
 * change 3). A badge carries a verdict or a state word, never anything
 * else; these helpers pick its tint so every screen colours the same word
 * the same way.
 */
import type { Tone as VerdictTone } from './workspace';

export type BadgeTone = VerdictTone | 'act' | 'muted' | 'neutral';

/**
 * A contest state as a tone: Live and Frozen in the action colour, Ended
 * and Archived muted, the rest (Upcoming, Practice, Draft) neutral.
 */
export function contestTone(state: string): BadgeTone {
  if (state === 'live' || state === 'frozen') return 'act';
  if (state === 'ended' || state === 'archived') return 'muted';
  return 'neutral';
}

/**
 * A score as a tone: full marks ok, some points partial, none bad, and no
 * score yet waiting.
 */
export function scoreTone(score: number | null | undefined, max: number): BadgeTone {
  if (score === null || score === undefined) return 'wait';
  if (max > 0 && score >= max) return 'ok';
  return score > 0 ? 'part' : 'bad';
}

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
