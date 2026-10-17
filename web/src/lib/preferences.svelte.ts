/**
 * The display preferences in effect, as reactive state, and the three ways
 * they change: the person picks a theme (choosePreferences), the account's
 * values arrive with the session (adoptAccountPreferences), or the person
 * signs out (resetPreferences). Each applies the values to <html>, keeps
 * the pre-paint cache in localStorage in step, and points the browser
 * chrome colour at the new paper. Saving to the account is the caller's
 * job, because only the caller knows whether anyone is signed in.
 */
import {
  DEFAULT_PREFERENCES,
  applyPreferences,
  fromAccount,
  getStorage,
  readPreferences,
  savePreferences,
  syncThemeColor,
  type Preferences,
} from './theme';

const storage = getStorage();

class PreferencesState {
  current = $state<Preferences>(readPreferences(storage));
}

/** The preferences on screen now. */
export const preferences = new PreferencesState();

function apply(p: Preferences) {
  preferences.current = p;
  if (typeof document === 'undefined') return;
  applyPreferences(document.documentElement, p);
  savePreferences(storage, p);
  syncThemeColor(document, p);
}

/** Applies a choice the person made on this device. */
export function choosePreferences(p: Preferences): void {
  apply(p);
}

/** Applies the account's stored values once the session has loaded. */
export function adoptAccountPreferences(theme: string | undefined, verdicts: string | undefined): void {
  apply(fromAccount(theme, verdicts));
}

/**
 * Returns to the defaults and clears the cache, so the next person at a
 * shared machine does not inherit this one's theme.
 */
export function resetPreferences(): void {
  apply({ ...DEFAULT_PREFERENCES });
}
