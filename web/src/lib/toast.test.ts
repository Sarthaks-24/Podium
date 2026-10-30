import { describe, expect, it, vi } from 'vitest';
import { CLEAR_MS, regions } from './announce.svelte';
import { ACTION_TOAST_MS, MAX_TOASTS, TOAST_MS, dismissToast, runToastAction, showToast, toasts } from './toast.svelte';

describe('toasts', () => {
  it('shows at most three, the newest replacing the oldest', () => {
    for (const t of toasts.items) dismissToast(t.id);
    for (let i = 1; i <= 5; i++) showToast(`Message ${i}`);
    expect(toasts.items).toHaveLength(MAX_TOASTS);
    expect(toasts.items.map((t) => t.message)).toEqual(['Message 3', 'Message 4', 'Message 5']);
  });

  it('never evicts an error toast to make room for info toasts', () => {
    for (const t of toasts.items) dismissToast(t.id);
    showToast('Could not save the answer', 'error');
    for (let i = 1; i <= 4; i++) showToast(`Info ${i}`);
    expect(toasts.items.map((t) => t.message)).toEqual(['Could not save the answer', 'Info 3', 'Info 4']);
    for (let i = 1; i <= 3; i++) showToast(`Error ${i}`, 'error');
    showToast('Info 5');
    expect(toasts.items.filter((t) => t.kind === 'error')).toHaveLength(4);
    expect(toasts.items.filter((t) => t.kind === 'info').map((t) => t.message)).toEqual(['Info 5']);
  });

  it('shows the same info sentence once, the newest replacing its copy', () => {
    for (const t of toasts.items) dismissToast(t.id);
    showToast('Pasting is blocked in this contest. Nothing was pasted.');
    showToast('Saved');
    const id = showToast('Pasting is blocked in this contest. Nothing was pasted.');
    expect(toasts.items.map((t) => t.message)).toEqual(['Saved', 'Pasting is blocked in this contest. Nothing was pasted.']);
    expect(toasts.items.at(-1)?.id).toBe(id);
  });

  it('speaks info toasts through the polite announcer, and leaves errors to their alert', () => {
    vi.useFakeTimers();
    try {
      regions.polite = '';
      showToast('Saved the settings');
      vi.advanceTimersByTime(CLEAR_MS);
      expect(regions.polite).toBe('Saved the settings');
      showToast('Could not save', 'error');
      vi.advanceTimersByTime(CLEAR_MS);
      expect(regions.polite).toBe('Saved the settings');
    } finally {
      vi.useRealTimers();
    }
  });

  it('runs an action once, closing its toast first, and keeps it 8 s while saying where it is (v4 U4)', () => {
    vi.useFakeTimers();
    try {
      for (const t of toasts.items) dismissToast(t.id);
      regions.polite = '';
      let undone = 0;
      const id = showToast('Removed A plus B', 'info', { label: 'Undo', run: () => undone++ });
      vi.advanceTimersByTime(CLEAR_MS);
      expect(regions.polite).toBe('Removed A plus B. Undo is in the corner for 8 seconds.');
      vi.advanceTimersByTime(TOAST_MS);
      expect(toasts.items.map((t) => t.id)).toContain(id);
      runToastAction(id);
      runToastAction(id);
      expect(undone).toBe(1);
      expect(toasts.items).toHaveLength(0);
      const later = showToast('Removed C', 'info', { label: 'Undo', run: () => {} });
      vi.advanceTimersByTime(ACTION_TOAST_MS);
      expect(toasts.items.map((t) => t.id)).not.toContain(later);
    } finally {
      vi.useRealTimers();
    }
  });
});
