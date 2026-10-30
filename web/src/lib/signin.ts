/**
 * Copy and links for the sign-in page, as pure functions.
 *
 * Messages are returned as a list of parts: plain text, or an email address
 * that the page renders as a mailto link. This keeps HTML out of the strings
 * so nothing is ever rendered with {@html}.
 */

/** A piece of a message: text, or an email address shown as a mailto link. */
export type MessagePart = string | { email: string };

/** The error codes /api/auth/google/callback can send back (docs/API.md). */
export type SignInErrorCode = 'not_listed' | 'unverified' | 'disabled' | 'account' | 'expired' | 'google' | 'server' | 'rate_limited';

const ERROR_CODES: readonly SignInErrorCode[] = ['not_listed', 'unverified', 'disabled', 'account', 'expired', 'google', 'server', 'rate_limited'];

/** Narrows a ?error= value to a known code, or null. A bookmarked v3 "domain" reads as not on the list. */
export function parseErrorCode(value: string | null): SignInErrorCode | null {
  return ERROR_CODES.find((c) => c === (value === 'domain' ? 'not_listed' : value)) ?? null;
}

/** The line under the heading that says which accounts may sign in. */
export function audienceLine(): string {
  return 'Sign in with the Google account of the email the organisers added.';
}

/** The person to contact: a mailto part, or plain words when none is configured. */
function contact(email: string): MessagePart {
  return email.trim() !== '' ? { email } : 'an organiser';
}

/** The error banner for a ?error= code, or null when the code is unknown. */
export function errorMessage(
  code: SignInErrorCode | null,
  contactEmail: string,
): MessagePart[] | null {
  const who = contact(contactEmail);
  switch (code) {
    case 'not_listed':
      return ['Your email is not on the list for this site. Ask ', who, ' to add it.'];
    case 'unverified':
      return ['Google has not verified this email address yet. Verify it with Google, then try again.'];
    case 'disabled':
      return ['This account has been disabled. Ask ', who, ' if you think this is a mistake.'];
    case 'account':
      return [
        'This email is linked to a different Google account. Ask ',
        who,
        ' to reset the link if your address moved to a new Google account.',
      ];
    case 'expired':
      return ['That sign-in attempt expired. Start again.'];
    case 'google':
      return ['Google sign-in did not complete. Try again in a moment.'];
    case 'server':
      return ['Something went wrong on the server while signing you in. Try again in a minute.'];
    case 'rate_limited':
      return ['Too many people are signing in from this network at once. Wait a few seconds, then start again.'];
    default:
      return null;
  }
}

/**
 * Keeps ?next= to a path on this site. Anything that is not a single-slash
 * path, or that points back at the sign-in page, becomes "/".
 */
export function safeNext(value: string | null): string {
  if (value === null || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return '/';
  if (value === '/signin' || value.startsWith('/signin?') || value.startsWith('/api/')) return '/';
  return value;
}

/** The link that starts Google sign-in and comes back to `next`. */
export function startHref(next: string): string {
  return `/api/auth/google/start?next=${encodeURIComponent(safeNext(next))}`;
}
