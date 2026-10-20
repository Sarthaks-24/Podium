/**
 * One announcer for the whole app (v2 F5, audit 06). App.svelte renders two
 * always-present, visually hidden live regions, one polite and one
 * assertive, from `regions`. polite() and assertive() first clear their
 * region and set the text a moment later, so the same sentence said twice
 * ("Saved", then "Saved") is announced twice: screen readers only speak a
 * region when its content changes.
 *
 * Use polite() for results the person waits for (a run finished, a draft
 * saved) and assertive() only for something that needs action now. Widgets
 * that own a running text, such as the contest clock, keep their own region.
 */

/** How long a region stays empty before the new text goes in. */
export const CLEAR_MS = 100;

class Regions {
  polite = $state('');
  assertive = $state('');
}

/** What the two live regions in App.svelte show. */
export const regions = new Regions();

const timers: Record<'polite' | 'assertive', ReturnType<typeof setTimeout> | undefined> = {
  polite: undefined,
  assertive: undefined,
};

function say(kind: 'polite' | 'assertive', text: string): void {
  clearTimeout(timers[kind]);
  regions[kind] = '';
  timers[kind] = setTimeout(() => {
    regions[kind] = text;
  }, CLEAR_MS);
}

/** Announces text when the screen reader is next idle. */
export function polite(text: string): void {
  say('polite', text);
}

/** Announces text at once, interrupting. Only for something that needs action now. */
export function assertive(text: string): void {
  say('assertive', text);
}
