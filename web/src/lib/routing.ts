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
  | 'ai-settings'
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
  | 'integrity-case'
  | 'my-integrity'
  | 'my-case'
  | 'progress'
  | 'admin-progress'
  | 'students'
  | 'student'
  | 'student-record'
  | 'inbox'
  | 'privacy'
  | 'contact'
  | 'contact-inbox'
  | 'not-found';

/** One row of the route table. */
export interface RouteDef {
  /** Path pattern; a segment starting with ":" captures a parameter. */
  pattern: string;
  name: RouteName;
  /** True when the screen needs a signed-in user. */
  auth: boolean;
  /**
   * True when the page may be indexed and gets a full head (title,
   * description, canonical, Open Graph) from the server (D100, contract C2).
   * Every other route keeps the default-closed noindex block of index.html.
   */
  public: boolean;
  /**
   * The page's own title: the whole tab title for a fixed screen, and the
   * fixed part for a screen that adds a contest or problem name. setTitle
   * callers read it through routeTitle(), and the server's head reads it
   * from routes.json, so the two never drift.
   */
  title: string;
  /** One sentence for the description meta. Shown to search engines on public routes only. */
  description: string;
}

/** The route table. Order matters: the first match wins. */
export const ROUTES: readonly RouteDef[] = [
  { pattern: '/', name: 'home', auth: true, public: false, title: 'Your contests', description: 'The contests you can enter, with their times and your scores.' },
  { pattern: '/signin', name: 'signin', auth: false, public: true, title: 'Sign in', description: 'Sign in to podium with your college Google account to enter the weekly programming contests.' },
  { pattern: '/admin/people', name: 'people', auth: true, public: false, title: 'People', description: 'Organisers, allowed email domains and testing mode.' },
  { pattern: '/admin/problems', name: 'problems', auth: true, public: false, title: 'Problem bank', description: 'Every problem, its validation state and where it is used.' },
  { pattern: '/admin/problems/:id', name: 'problem', auth: true, public: false, title: 'Problem', description: 'Edit a problem: statement, tests, checker and validation.' },
  { pattern: '/admin/problems/:id/try', name: 'problem-try', auth: true, public: false, title: 'Problem bank', description: 'Try a problem as a student would see it.' },
  { pattern: '/admin/ai', name: 'ai-settings', auth: true, public: false, title: 'AI assistant', description: 'The AI provider key and model for this session.' },
  { pattern: '/admin/ai/new', name: 'ai-new', auth: true, public: false, title: 'New problem with AI', description: 'Draft a new problem with the AI assistant.' },
  { pattern: '/contests/:slug', name: 'contest', auth: true, public: false, title: 'Contest', description: 'A contest: its problems, time left and your submissions.' },
  { pattern: '/contests/:slug/standings', name: 'standings', auth: true, public: false, title: 'Standings', description: 'The standings of a contest.' },
  { pattern: '/contests/:slug/problems/:label', name: 'contest-problem', auth: true, public: false, title: 'Contest', description: 'One problem of a contest, with the editor and your results.' },
  { pattern: '/admin/contests', name: 'admin-contests', auth: true, public: false, title: 'Contests', description: 'Every contest, its state and its settings.' },
  { pattern: '/admin/contests/:id', name: 'admin-contest', auth: true, public: false, title: 'Contest settings', description: 'Edit a contest: times, problems, rules and publishing.' },
  { pattern: '/admin/contests/:id/console', name: 'contest-console', auth: true, public: false, title: 'Console', description: 'Run a live contest: submissions, questions, announcements and time.' },
  { pattern: '/admin/contests/:id/results', name: 'contest-results', auth: true, public: false, title: 'Results', description: 'Final scores and problem statistics for a contest.' },
  { pattern: '/admin/contests/:id/integrity', name: 'contest-integrity', auth: true, public: false, title: 'Integrity', description: 'Signals worth a look and open cases for a contest.' },
  { pattern: '/admin/contests/:id/integrity/signals/:signal', name: 'integrity-pair', auth: true, public: false, title: 'Signal', description: 'One integrity signal with its evidence.' },
  { pattern: '/admin/integrity/cases/:case', name: 'integrity-case', auth: true, public: false, title: 'Case', description: 'One integrity case: evidence, notes and the decision.' },
  { pattern: '/privacy', name: 'privacy', auth: false, public: false, title: 'Privacy', description: 'What podium records about you, who can see it and how long it is kept.' },
  { pattern: '/contact', name: 'contact', auth: true, public: false, title: 'Write to the organisers', description: 'Send the organisers a message about a contest, your account or your data.' },
  { pattern: '/admin/contact', name: 'contact-inbox', auth: true, public: false, title: 'Inbox', description: 'Messages people sent to the organisers, with a reply link.' },
  { pattern: '/integrity', name: 'my-integrity', auth: true, public: false, title: 'Questions and standing', description: 'Your standing, and the questions the organisers asked about your submissions with your replies.' },
  { pattern: '/integrity/cases/:case', name: 'my-case', auth: true, public: false, title: 'Question about your submission', description: 'One question from the organisers about your submission, your reply and any decision.' },
  { pattern: '/progress', name: 'progress', auth: true, public: false, title: 'Your progress', description: 'Your final results across contests, where points were lost and what to practise next.' },
  { pattern: '/admin/users/:id/progress', name: 'admin-progress', auth: true, public: false, title: 'Student progress', description: 'The final results of one student across contests, as they see them.' },
  { pattern: '/admin/students', name: 'students', auth: true, public: false, title: 'Students', description: 'Every student with their graded contests, latest result and rating, with filters for who may need help.' },
  { pattern: '/admin/students/:id', name: 'student', auth: true, public: false, title: 'Student', description: 'One student across graded contests, beside the class median.' },
  { pattern: '/admin/students/:id/record', name: 'student-record', auth: true, public: false, title: 'Record', description: "One student's standing, rating, cases, notes and appeals, for organisers. Every visit is logged." },
  { pattern: '/notices', name: 'inbox', auth: true, public: false, title: 'Notices', description: 'Notices about your results and about questions from the organisers.' },
];

/** Shown for any path that matches nothing in ROUTES. */
export const NOT_FOUND: RouteDef = {
  pattern: '*',
  name: 'not-found',
  auth: false,
  public: true,
  title: 'Page not found',
  description: 'That page does not exist on podium.',
};

/** The title of a route by name, from the route table. */
export function routeTitle(name: RouteName): string {
  return (ROUTES.find((r) => r.name === name) ?? NOT_FOUND).title;
}

/** The shape of routes.json, written by the Vite build (contract C2). */
export interface RouteManifest {
  /** Bumped when a field changes meaning, so the server can refuse a manifest it does not understand. */
  version: 1;
  /** Every route in match order. */
  routes: RouteDef[];
  /** The page shown for any other path; the server answers it with status 404. */
  notFound: RouteDef;
}

/** Builds routes.json from the route table. The Vite build writes it into dist/. */
export function routeManifest(): RouteManifest {
  const row = (r: RouteDef): RouteDef => ({
    pattern: r.pattern,
    name: r.name,
    auth: r.auth,
    public: r.public,
    title: r.title,
    description: r.description,
  });
  return { version: 1, routes: ROUTES.map(row), notFound: row(NOT_FOUND) };
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
 * Scrolls to the element a navigation's hash names (walk 2026-10-21): a
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
