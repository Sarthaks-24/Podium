// @vitest-environment jsdom
// The one dismissal rule shared by the popovers.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { dismissable } from './dismissable';

afterEach(() => {
  document.body.innerHTML = '';
});

function setup(open = true) {
  const root = document.createElement('div');
  const trigger = document.createElement('button');
  const inside = document.createElement('button');
  root.append(trigger, inside);
  const outside = document.createElement('button');
  const dialog = document.createElement('dialog');
  const inDialog = document.createElement('button');
  dialog.append(inDialog);
  document.body.append(root, outside, dialog);
  const onclose = vi.fn();
  const action = dismissable(root, { open, onclose, trigger });
  return { root, trigger, inside, outside, dialog, inDialog, onclose, action };
}

describe('dismissable', () => {
  it('closes on Escape and gives focus back to the trigger', () => {
    const t = setup();
    t.inside.focus();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
    expect(t.onclose).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(t.trigger);
  });

  it('leaves Escape to an open dialog and to a handler that took it', () => {
    const t = setup();
    t.dialog.setAttribute('open', '');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
    t.dialog.removeAttribute('open');
    const taken = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true });
    taken.preventDefault();
    window.dispatchEvent(taken);
    expect(t.onclose).not.toHaveBeenCalled();
  });

  it('closes on a press or focus outside, not inside or in a dialog', () => {
    const t = setup();
    t.inside.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    t.inDialog.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    t.inside.dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: t.inDialog }));
    expect(t.onclose).not.toHaveBeenCalled();
    t.outside.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    expect(t.onclose).toHaveBeenCalledTimes(1);
    t.inside.dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: t.outside }));
    expect(t.onclose).toHaveBeenCalledTimes(2);
  });

  it('does nothing while closed, and nothing after destroy', () => {
    const t = setup(false);
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
    t.outside.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    expect(t.onclose).not.toHaveBeenCalled();
    t.action.update({ open: true, onclose: t.onclose, trigger: t.trigger });
    t.action.destroy();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
    expect(t.onclose).not.toHaveBeenCalled();
  });
});
