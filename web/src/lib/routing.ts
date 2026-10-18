/**
 * Route matching and link-interception rules for the router in
 * router.svelte.ts, kept as pure functions so they can be tested without a
 * browser.
 *
 * Links under /api/ are never intercepted: /api/auth/google/start is a server
 * redirect to Google and needs a full page load.
 */

/** Names of the screens the app can show. */
export type RouteName =
  | 'home'
  | 'signin'
  | 'people'
  | 'problems'
  | 'problem'
  | 'problem-try'
  | 'contest'
  | 'contest-problem'
  | 'standings'
  | 'admin-contests'
  | 'admin-contest'
  | 'contest-console'
  | 'contest-results'
  | 'contest-integrity'
  | 'integrity-pair'
  | 'integrity-case'
  | 'my-integrity'
  | 'not-found';

/** One row of the route table. */
export interface RouteDef {
  /** Path pattern; a segment starting with ":" captures a parameter. */
  pattern: string;
  name: RouteName;
  /** True when the screen needs a signed-in user. */
  auth: boolean;
}

/** The route table. Order matters: the first match wins. */
export const ROUTES: readonly RouteDef[] = [
  { pattern: '/', name: 'home', auth: true },
  { pattern: '/signin', name: 'signin', auth: false },
  { pattern: '/admin/people', name: 'people', auth: true },
  { pattern: '/admin/problems', name: 'problems', auth: true },
  { pattern: '/admin/problems/:id', name: 'problem', auth: true },
  { pattern: '/admin/problems/:id/try', name: 'problem-try', auth: true },
  { pattern: '/contests/:slug', name: 'contest', auth: true },
  { pattern: '/contests/:slug/standings', name: 'standings', auth: true },
  { pattern: '/contests/:slug/problems/:label', name: 'contest-problem', auth: true },
  { pattern: '/admin/contests', name: 'admin-contests', auth: true },
  { pattern: '/admin/contests/:id', name: 'admin-contest', auth: true },
  { pattern: '/admin/contests/:id/console', name: 'contest-console', auth: true },
  { pattern: '/admin/contests/:id/results', name: 'contest-results', auth: true },
  { pattern: '/admin/contests/:id/integrity', name: 'contest-integrity', auth: true },
  { pattern: '/admin/contests/:id/integrity/signals/:signal', name: 'integrity-pair', auth: true },
  { pattern: '/admin/integrity/cases/:case', name: 'integrity-case', auth: true },
  { pattern: '/integrity', name: 'my-integrity', auth: true },
];

/** Shown for any path that matches nothing in ROUTES. */
export const NOT_FOUND: RouteDef = { pattern: '*', name: 'not-found', auth: false };

/** The result of matching a path against ROUTES. */
export interface RouteMatch {
  route: RouteDef;
  params: Record<string, string>;
}

function segments(path: string): string[] {
  return path.split('/').filter((s) => s !== '');
}

/**
 * Finds the route for a path. Trailing slashes are ignored; matching is case
 * sensitive. Paths that match nothing return NOT_FOUND.
 */
export function matchRoute(path: string, routes: readonly RouteDef[] = ROUTES): RouteMatch {
  const parts = segments(path);
  for (const route of routes) {
    const pattern = segments(route.pattern);
    if (pattern.length !== parts.length) continue;
    const params: Record<string, string> = {};
    let ok = true;
    for (let i = 0; i < pattern.length; i++) {
      if (pattern[i].startsWith(':')) {
        try {
          params[pattern[i].slice(1)] = decodeURIComponent(parts[i]);
        } catch {
          ok = false;
          break;
        }
      } else if (pattern[i] !== parts[i]) {
        ok = false;
        break;
      }
    }
    if (ok) return { route, params };
  }
  return { route: NOT_FOUND, params: {} };
}

/** The parts of a click event the interception rules look at. */
export interface ClickLike {
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  defaultPrevented: boolean;
}

/** The parts of an anchor element the interception rules look at. */
export interface AnchorLike {
  /** The raw href attribute, or null when the anchor has none. */
  href: string | null;
  /** The target attribute, or an empty string. */
  target: string;
  /** True when the anchor has a download attribute. */
  download: boolean;
}

/** Path prefixes that always get a full page load. */
const SERVER_PATHS = ['/api/', '/healthz'];

/**
 * Decides whether a link click becomes a client-side navigation. Returns the
 * path, query and hash to navigate to, or null to let the browser handle the
 * click (modified clicks, other buttons, targets, downloads, other origins,
 * server paths, and hash-only jumps on the current page).
 */
export function internalHref(
  click: ClickLike,
  anchor: AnchorLike,
  origin: string,
  current: { path: string; search: string } = { path: '', search: '' },
): string | null {
  if (click.defaultPrevented || click.button !== 0) return null;
  if (click.metaKey || click.ctrlKey || click.shiftKey || click.altKey) return null;
  if (anchor.href === null || anchor.download) return null;
  if (anchor.target !== '' && anchor.target !== '_self') return null;

  let url: URL;
  try {
    // Relative hrefs ("#main", "people") resolve against the current page.
    url = new URL(anchor.href, new URL(current.path + current.search, origin));
  } catch {
    return null;
  }
  if (url.origin !== origin) return null;
  if (url.pathname === '/api' || SERVER_PATHS.some((p) => url.pathname.startsWith(p))) return null;
  if (url.hash !== '' && url.pathname === current.path && url.search === current.search) return null;
  return url.pathname + url.search + url.hash;
}

/**
 * Resolves a navigation target against the origin. Returns the path, query
 * and hash to put in the address bar, and whether it carries a hash.
 */
export function resolveTarget(to: string, origin: string): { target: string; hasHash: boolean } {
  const url = new URL(to, origin);
  return { target: url.pathname + url.search + url.hash, hasHash: url.hash !== '' };
}
