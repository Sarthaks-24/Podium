/**
 * The six curated presets (v2.0.1 E5): engine inputs, not token blocks. Each
 * is a background, an accent and a contrast level that theme-engine.ts turns
 * into a whole theme, so it passes the D41 rules by construction and the
 * property test and theme-contrast.test.ts cover it like any custom theme.
 * They come from the v1 theme set (D39) and were checked against the
 * design.md section 2 bans: no neon on slate, no cream with terracotta, no
 * gradients. The four hand-tuned themes in tokens.css are separate.
 *
 * Ids are the values stored as the account's theme; labels are sentence case.
 */
import type { ThemeInput } from './theme-engine';

export interface Preset {
  id: string;
  label: string;
  input: ThemeInput;
}

/** What Custom starts as the first time it is chosen: the standard light colours. */
export const CUSTOM_START: ThemeInput = { background: '#f3f4f1', accent: '#2b3fd6', contrast: 'standard' };

export const PRESETS: readonly Preset[] = [
  { id: 'nordic-frost', label: 'Nordic frost', input: { background: '#ECEFF4', accent: '#4C6A92', contrast: 'standard' } },
  { id: 'soft-lavender', label: 'Soft lavender', input: { background: '#F2EFF9', accent: '#6247B0', contrast: 'standard' } },
  { id: 'overcast', label: 'Overcast', input: { background: '#F1F2F4', accent: '#4B5563', contrast: 'standard' } },
  { id: 'nordic-slate', label: 'Nordic slate', input: { background: '#2B313B', accent: '#88B4C8', contrast: 'standard' } },
  { id: 'oled-black', label: 'OLED black', input: { background: '#000000', accent: '#8FA8E8', contrast: 'standard' } },
  { id: 'cosmic-violet', label: 'Cosmic violet', input: { background: '#1A1428', accent: '#A898E0', contrast: 'standard' } },
];
