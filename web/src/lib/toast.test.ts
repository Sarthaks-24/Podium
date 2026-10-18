import { describe, expect, it } from 'vitest';
import { MAX_TOASTS, dismissToast, showToast, toasts } from './toast.svelte';

describe('toasts', () => {
  it('shows at most three, the newest replacing the oldest', () => {
    for (const t of toasts.items) dismissToast(t.id);
    for (let i = 1; i <= 5; i++) showToast(`Message ${i}`, i === 2 ? 'error' : 'info');
    expect(toasts.items).toHaveLength(MAX_TOASTS);
    expect(toasts.items.map((t) => t.message)).toEqual(['Message 3', 'Message 4', 'Message 5']);
  });
});
