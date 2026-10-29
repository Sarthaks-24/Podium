/**
 * A small history-API router.
 *
 * The current path and query string are reactive state on `router`. Screens
 * change the URL with navigate(); the browser's back and forward buttons are
 * handled through popstate. installLinkInterception() turns clicks on plain
 * same-origin links into client-side navigations, so components can use
 * ordinary <a href="/..."> elements. Links under /api/ always get a full page
 * load, because /api/auth/google/start is a server redirect.
 *
 * A screen with unsaved work can install a navigation guard with
 * setNavigationGuard(); intercepted link clicks and the browser's back and
 * forward buttons ask it first and stay put when it says no. The browser
 * has already moved by the time popstate fires, so a refused back puts the
 * page's own address back with pushState.
 *
 * The route table, matching and interception rules live in routing.ts and
 * are re-exported here.
 */
import { internalHref, resolveTarget, revealHash } from './routing';

export * from './routing';

class RouterState {
  /** The current path, for example "/admin/people". */
  path = $state('/');
  /** The current query string including "?", or an empty string. */
  search = $state('');

  /** The current query parameters. */
  get query(): URLSearchParams {
    return new URLSearchParams(this.search);
  }

  /** Path plus query, suitable for a ?next= value. */
  get fullPath(): string {
    return this.path + this.search;
  }
}

/** Reactive location state shared by the whole app. */
export const router = new RouterState();

/** The address the page last showed, hash included, to go back to when a guard refuses. */
let shown = '';

function syncFromWindow(): void {
  router.path = window.location.pathname;
  router.search = window.location.search;
  shown = router.path + router.search + window.location.hash;
}

/** Back and forward: a move to another page asks the guard first; a hash move never does. */
function onPopState(): void {
  const page = window.location.pathname + window.location.search;
  if (page !== router.path + router.search && guard !== null && !guard(page + window.location.hash)) {
    window.history.pushState(null, '', shown);
    return;
  }
  syncFromWindow();
}

if (typeof window !== 'undefined') {
  syncFromWindow();
  window.addEventListener('popstate', onPopState);
}

/**
 * Decides whether an in-app link, or back or forward, may navigate to `to`. Returning false keeps
 * the current screen; the guard is then responsible for asking the person
 * and calling navigate() itself if they agree.
 */
export type NavigationGuard = (to: string) => boolean;

let guard: NavigationGuard | null = null;

/**
 * Installs the guard consulted before intercepted link clicks and back or forward navigate.
 * There is one guard at a time; the returned function removes it if it is
 * still the installed one.
 */
export function setNavigationGuard(fn: NavigationGuard): () => void {
  guard = fn;
  return () => {
    if (guard === fn) guard = null;
  };
}

/** Changes the URL without a page load and updates `router`. */
export function navigate(to: string, options: { replace?: boolean } = {}): void {
  if (typeof window === 'undefined') return;
  const { target, hasHash } = resolveTarget(to, window.location.origin);
  const here = window.location.pathname + window.location.search + window.location.hash;
  if (options.replace) {
    window.history.replaceState(null, '', target);
  } else if (target !== here) {
    window.history.pushState(null, '', target);
  }
  syncFromWindow();
  if (!hasHash) window.scrollTo(0, 0);
  else revealHash(window.location.hash, (id) => document.getElementById(id));
}

/**
 * Intercepts clicks on same-origin links anywhere in the document. Returns a
 * function that removes the listener.
 */
export function installLinkInterception(doc: Document = document): () => void {
  function onClick(event: MouseEvent) {
    const start = event.target;
    if (!(start instanceof Element)) return;
    const anchor = start.closest('a');
    if (!(anchor instanceof HTMLAnchorElement)) return;
    const to = internalHref(
      event,
      { href: anchor.getAttribute('href'), target: anchor.target, download: anchor.hasAttribute('download') },
      window.location.origin,
      { path: window.location.pathname, search: window.location.search },
    );
    if (to === null) return;
    event.preventDefault();
    if (guard !== null && !guard(to)) return;
    navigate(to);
  }
  doc.addEventListener('click', onClick);
  return () => doc.removeEventListener('click', onClick);
}
