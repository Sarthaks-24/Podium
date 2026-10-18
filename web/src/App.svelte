<!--
  App: the shell around every screen. The header holds the podium word-mark
  (a link home) and, once signed in, the main navigation ("Contests", plus
  "Problems" for admins and owners and "People" for owners), the user's
  name, a Sign out button and the theme
  menu. Under 768 px those collapse behind a Menu button that opens a
  panel; Escape closes it. The main area shows the screen for the current
  path from lib/router.svelte.ts, after the session from GET /api/me has
  loaded: signed-out visitors to a protected path are sent to /signin with a
  ?next= back, and signed-in visitors to /signin are sent on. After each
  client-side navigation focus moves to the new page's heading. The footer
  carries the build year. Takes no props.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Toasts from './components/Toasts.svelte';
  import ThemeMenu from './lib/ThemeMenu.svelte';
  import { loadMode, runMode } from './lib/mode.svelte';
  import { installLinkInterception, matchRoute, navigate, router } from './lib/router.svelte';
  import { displayName, loadSession, session, signOut } from './lib/session.svelte';
  import { safeNext } from './lib/signin';
  import { showToast } from './lib/toast.svelte';
  import Home from './routes/Home.svelte';
  import NotFound from './routes/NotFound.svelte';
  import People from './routes/People.svelte';
  import ProblemBank from './routes/ProblemBank.svelte';
  import ProblemEditor from './routes/ProblemEditor.svelte';
  import SignIn from './routes/SignIn.svelte';

  const year = __BUILD_YEAR__;

  let menuOpen = $state(false);
  let menuButton: HTMLButtonElement | undefined = $state();

  const match = $derived(matchRoute(router.path));
  const route = $derived(match.route);
  const signedIn = $derived(session.status === 'signed-in');
  const isOwner = $derived(session.user?.role === 'owner');
  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const needsSignIn = $derived(route.auth && session.status === 'signed-out');
  const leaveSignIn = $derived(route.name === 'signin' && signedIn);

  // Redirects are not navigations the person chose, so they keep focus where
  // it is. The redirect's target path is remembered so the focus effect can
  // tell it apart, whichever of the two effects runs first.
  let redirectedTo: string | null = null;
  let lastPath: string | null = null;

  function redirect(to: string) {
    redirectedTo = to.split(/[?#]/)[0];
    navigate(to, { replace: true });
  }

  $effect(() => {
    if (needsSignIn) {
      redirect(`/signin?next=${encodeURIComponent(router.fullPath)}`);
    } else if (leaveSignIn) {
      redirect(safeNext(router.query.get('next')));
    }
  });

  $effect(() => {
    const path = router.path;
    if (session.status === 'loading') return;
    const wasRedirect = redirectedTo === path;
    redirectedTo = null;
    if (lastPath !== null && lastPath !== path) {
      menuOpen = false;
      if (!wasRedirect) tick().then(() => document.querySelector<HTMLElement>('main h1')?.focus());
    }
    lastPath = path;
  });

  onMount(() => {
    const removeInterception = installLinkInterception();
    loadSession().then((err) => {
      if (err) showToast(err.message, 'error');
    });
    loadMode();
    return removeInterception;
  });

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && menuOpen) {
      menuOpen = false;
      menuButton?.focus();
    }
  }

  async function onSignOut() {
    menuOpen = false;
    const err = await signOut();
    if (err) showToast(err.message, 'error');
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="page">
  <a class="skip" href="#main">Skip to content</a>


  <header class="bar">
    {#if runMode.mode === 'testing'}
      <p class="mode-banner" role="note">
        Testing mode: sign-in is off and anyone at this machine can sign in as anyone. Do not use it for a real contest.
      </p>
    {/if}
    <div class="inner bar-inner">
      <a class="wordmark" href="/">podium</a>

      {#if signedIn && session.user}
        <button
          type="button"
          class="btn btn-secondary btn-compact menu-button"
          aria-expanded={menuOpen}
          aria-controls="site-menu"
          bind:this={menuButton}
          onclick={() => (menuOpen = !menuOpen)}
        >
          Menu
        </button>

        <div id="site-menu" class="menu" class:open={menuOpen}>
          <nav class="nav" aria-label="Main">
            <ul>
              <li>
                <a href="/" aria-current={route.name === 'home' ? 'page' : undefined}>Contests</a>
              </li>
              {#if isOrganiser}
                <li>
                  <a
                    href="/admin/problems"
                    aria-current={route.name === 'problems' || route.name === 'problem' ? 'page' : undefined}>Problems</a
                  >
                </li>
              {/if}
              {#if isOwner}
                <li>
                  <a href="/admin/people" aria-current={route.name === 'people' ? 'page' : undefined}>People</a>
                </li>
              {/if}
            </ul>
          </nav>
          <div class="account">
            <span class="who">{displayName(session.user)}</span>
            <button type="button" class="btn btn-quiet btn-compact signout" onclick={onSignOut}>Sign out</button>
            <ThemeMenu />
          </div>
        </div>
      {:else}
        <ThemeMenu />
      {/if}
    </div>
  </header>

  <main id="main" class="inner main" tabindex="-1">
    {#if session.status === 'loading' || needsSignIn || leaveSignIn}
      <p class="visually-hidden" role="status">Loading</p>
    {:else if route.name === 'signin'}
      <SignIn />
    {:else if route.name === 'home'}
      <Home />
    {:else if route.name === 'people'}
      <People />
    {:else if route.name === 'problems'}
      <ProblemBank />
    {:else if route.name === 'problem'}
      {#key match.params.id}
        <ProblemEditor id={match.params.id} />
      {/key}
    {:else}
      <NotFound />
    {/if}
  </main>

  <footer class="inner foot">
    <p>&copy; {year} podium</p>
  </footer>
</div>

<Toasts />

<style>
  .page {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto 1fr auto;
    min-height: 100vh;
    min-height: 100dvh;
  }

  .skip {
    position: absolute;
    left: var(--gutter);
    top: var(--space-2);
    z-index: 30;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--act);
    transform: translateY(-200%);
  }

  .skip:focus-visible {
    transform: none;
  }

  .mode-banner {
    padding: var(--space-2) var(--gutter);
    border-bottom: var(--rule) solid var(--line);
    background: var(--part-tint);
    color: var(--ink);
    font-size: var(--fs-0);
  }

  .inner {
    width: 100%;
    max-width: var(--content-max);
    margin-inline: auto;
    padding-inline: var(--gutter);
  }

  .bar {
    border-bottom: var(--rule) solid var(--line);
  }

  .bar-inner {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-5);
    min-height: var(--space-7);
    padding-block: var(--space-2);
  }

  .wordmark {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
    margin-right: auto;
    color: var(--ink);
    font-size: var(--fs-2);
    font-weight: 600;
    line-height: var(--lh-tight);
    text-decoration: none;
    border-radius: var(--radius);
  }

  .menu-button {
    display: none;
  }

  .menu {
    display: flex;
    flex: 1 1 auto;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
  }

  .nav ul {
    display: flex;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  .nav a {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
    padding-inline: var(--space-2);
    border-radius: var(--radius);
    color: var(--ink-muted);
    font-weight: 500;
    text-decoration: none;
    transition: color var(--dur-fast) var(--ease);
  }

  .nav a:hover {
    color: var(--ink);
  }

  .nav a[aria-current='page'] {
    color: var(--ink);
    text-decoration: underline;
    text-decoration-color: var(--act);
    text-decoration-thickness: 2px;
    text-underline-offset: 0.45em;
  }

  .account {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .who {
    min-width: 0;
    overflow: hidden;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .main {
    padding-block: var(--space-6) var(--space-7);
  }

  .main:focus {
    outline: none;
  }

  .foot {
    padding-block: var(--space-5);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  @media (max-width: 767px) {
    .menu-button {
      display: inline-flex;
    }

    .menu {
      display: none;
      flex-basis: 100%;
      flex-direction: column;
      align-items: stretch;
      gap: var(--space-3);
      padding-block: var(--space-2) var(--space-4);
      border-top: var(--rule) solid var(--line);
    }

    .menu.open {
      display: flex;
    }

    .nav ul {
      flex-direction: column;
      gap: 0;
    }

    .nav a {
      width: 100%;
      padding-inline: 0;
    }

    .account {
      flex-wrap: wrap;
      padding-top: var(--space-3);
      border-top: var(--rule) solid var(--line);
    }

    .who {
      flex-basis: 100%;
      font-size: var(--fs-1);
    }

    /* Line the quiet button's text up with the name and links above it. */
    .signout {
      margin-left: calc(-1 * var(--space-3));
    }
  }
</style>
