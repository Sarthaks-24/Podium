import { describe, expect, it, vi } from 'vitest';
import { CLEAR_MS, regions } from './announce.svelte';
import { MAX_TOASTS, dismissToast, showToast, toasts } from './toast.svelte';

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
});
