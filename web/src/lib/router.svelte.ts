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
 * The route table, matching and interception rules live in routing.ts and
 * are re-exported here.
 */
import { internalHref, resolveTarget } from './routing';

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

function syncFromWindow(): void {
  router.path = window.location.pathname;
  router.search = window.location.search;
}

if (typeof window !== 'undefined') {
  syncFromWindow();
  window.addEventListener('popstate', syncFromWindow);
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
    navigate(to);
  }
  doc.addEventListener('click', onClick);
  return () => doc.removeEventListener('click', onClick);
}
