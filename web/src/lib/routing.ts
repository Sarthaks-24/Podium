/**
 * The route table, and the matching and link-interception rules of the
 * router in router.svelte.ts, kept as pure functions so they can be tested
 * without a browser. Each row names its screen, who may open it and the part
 * of the main navigation it belongs to; the shell (App.svelte) reads all of
 * that from here. The component for each row is in screens.ts, typed by
 * route name so no row can lack one, and kept apart so this file imports no
 * component.
 *
 * Links under /api/ are never intercepted: /api/auth/google/start is a server
 * redirect to Google and needs a full page load.
 */
import { organises } from './roles';
import type { Role } from './session.svelte';

/** Names of the screens the app can show. */
export type RouteName =
  | 'home'
  | 'signin'
  | 'dashboard'
  | 'problems'
  | 'problem'
  | 'problem-try'
  | 'ai-new'
  | 'contest'
  | 'contest-problem'
  | 'standings'
  | 'admin-contests'
  | 'admin-contest'
  | 'contest-console'
  | 'contest-results'
  | 'contest-integrity'
  | 'integrity-pair'
  | 'privacy'
  | 'not-found';

/** The parts of the main navigation; the link of a screen's own part is marked current. */
export type Section = 'student' | 'contests' | 'problems' | 'dashboard';

/** Who may open a screen: anyone who organises, admins and owners, or owners only. */
export type Access = 'organiser' | 'admin' | 'owner';

/** One row of the route table. */
export interface RouteDef {
  /** Path pattern; a segment starting with ":" captures a parameter. */
  pattern: string;
  name: RouteName;
  /** True when the screen needs a signed-in user. */
  auth: boolean;
  /**
   * The page's own title: the whole tab title for a fixed screen, and the
   * fixed part for a screen that adds a contest or problem name. setTitle
   * callers read it through routeTitle().
   */
  title: string;
  /** Who may open the screen; anyone signed in when left out. */
  role?: Access;
  /** The part of the main navigation the screen belongs to. */
  section?: Section;
}

/** The route table. Order matters: the first match wins. */
export const ROUTES: readonly RouteDef[] = [
  { pattern: '/', name: 'home', auth: true, title: 'Your contests', section: 'student' },
  { pattern: '/signin', name: 'signin', auth: false, title: 'Sign in' },
  { pattern: '/admin/dashboard', name: 'dashboard', auth: true, title: 'Dashboard', role: 'admin', section: 'dashboard' },
  // The Dashboard kept the old People and Settings address, so old links and bookmarks still open it.
  { pattern: '/admin/people', name: 'dashboard', auth: true, title: 'Dashboard', role: 'admin', section: 'dashboard' },
  { pattern: '/admin/problems', name: 'problems', auth: true, title: 'Problems', role: 'organiser', section: 'problems' },
  { pattern: '/admin/problems/:id', name: 'problem', auth: true, title: 'Problem', role: 'organiser', section: 'problems' },
  { pattern: '/admin/problems/:id/try', name: 'problem-try', auth: true, title: 'Problems', role: 'organiser', section: 'problems' },
  { pattern: '/admin/ai/new', name: 'ai-new', auth: true, title: 'New problem with AI', role: 'organiser', section: 'problems' },
  { pattern: '/contests/:slug', name: 'contest', auth: true, title: 'Contest', section: 'student' },
  { pattern: '/contests/:slug/standings', name: 'standings', auth: true, title: 'Standings', section: 'student' },
  { pattern: '/contests/:slug/problems/:label', name: 'contest-problem', auth: true, title: 'Contest', section: 'student' },
  { pattern: '/admin/contests', name: 'admin-contests', auth: true, title: 'Contests', role: 'organiser', section: 'contests' },
  { pattern: '/admin/contests/:id', name: 'admin-contest', auth: true, title: 'Setup', role: 'organiser', section: 'contests' },
  { pattern: '/admin/contests/:id/console', name: 'contest-console', auth: true, title: 'Live', role: 'organiser', section: 'contests' },
  { pattern: '/admin/contests/:id/results', name: 'contest-results', auth: true, title: 'Results', role: 'organiser', section: 'contests' },
  { pattern: '/admin/contests/:id/integrity', name: 'contest-integrity', auth: true, title: 'Similar code', role: 'organiser', section: 'contests' },
  {
    pattern: '/admin/contests/:id/integrity/signals/:signal',
    name: 'integrity-pair',
    auth: true,
    title: 'Signal',
    role: 'organiser',
    section: 'contests',
  },
  { pattern: '/privacy', name: 'privacy', auth: false, title: 'Privacy' },
];

/** Shown for any path that matches nothing in ROUTES. */
export const NOT_FOUND: RouteDef = {
  pattern: '*',
  name: 'not-found',
  auth: false,
  title: 'Page not found',
};

/** The title of a route by name, from the route table. */
export function routeTitle(name: RouteName): string {
  return (ROUTES.find((r) => r.name === name) ?? NOT_FOUND).title;
}

/** True when someone signed in with `role` may open the route's screen. */
export function mayOpen(route: Pick<RouteDef, 'role'>, role: Role | undefined): boolean {
  if (route.role === 'owner') return role === 'owner';
  if (route.role === 'admin') return role === 'admin' || role === 'owner';
  if (route.role === 'organiser') return organises(role);
  return true;
}

/** The one sentence the shell shows, under the page's title, to someone a row does not let in. */
export function forbiddenText(route: Pick<RouteDef, 'role' | 'section'>): string {
  if (route.role === 'owner') return 'Only owners can change this.';
  if (route.role === 'admin') return 'Only admins and owners can open the Dashboard.';
  return route.section === 'problems' ? 'Only organisers can manage problems.' : 'Only organisers can manage contests.';
}

/** One link of the main navigation. */
export interface NavItem {
  section: Section;
  href: string;
  label: string;
}

/** The main navigation by role: a student's contests; an organiser's contests and problems, and the Dashboard for admins and owners. */
export function mainNav(role: Role): NavItem[] {
  if (role === 'student') return [{ section: 'student', href: '/', label: 'Contests' }];
  const items: NavItem[] = [
    { section: 'contests', href: '/admin/contests', label: 'Contests' },
    { section: 'problems', href: '/admin/problems', label: 'Problems' },
  ];
  if (role === 'admin' || role === 'owner') items.push({ section: 'dashboard', href: '/admin/dashboard', label: 'Dashboard' });
  return items;
}

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

/**
 * Scrolls to the element a navigation's hash names: a
 * link such as /admin/contests/4/console#versions-title lands on a screen
 * that draws the element only after its data loads, so the browser's own
 * jump finds nothing. Looks again every 100 ms for up to `waitMs`, then
 * gives up quietly.
 */
export function revealHash(
  hash: string,
  find: (id: string) => { scrollIntoView(options?: ScrollIntoViewOptions): void } | null,
  setTimer: (fn: () => void, ms: number) => unknown = (fn, ms) => setTimeout(fn, ms),
  waitMs = 3000,
): void {
  if (hash.length < 2) return;
  let id: string;
  try {
    id = decodeURIComponent(hash.slice(1));
  } catch {
    return;
  }
  let left = Math.ceil(waitMs / 100);
  const tick = () => {
    const el = find(id);
    if (el) el.scrollIntoView({ block: 'start' });
    else if (left-- > 0) setTimer(tick, 100);
  };
  tick();
}
