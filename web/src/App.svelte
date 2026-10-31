<!--
  App: the shell around every screen. The header holds the word-mark, the
  navigation named by job (lib/routing mainNav) and one account control;
  under 768 px an organiser's links fold behind Menu, while a student
  keeps theirs in the row except in the contest workspace, whose strip
  takes that space. The main area waits for GET /api/me, then shows the
  screen the route table names (screens.ts loads organiser screens on
  first visit); signed-out visitors to a protected path go to /signin and
  come back after, someone the row does not let in sees one shared
  forbidden state, and focus moves to the new page's heading after each
  navigation. The footer carries the organisers' contact, the Privacy link
  once the text is approved, and Help, which "?" also opens.
-->
<script lang="ts">
  import { organises } from './lib/roles';
  import { onMount, tick } from 'svelte';
  import AccountMenu from './components/AccountMenu.svelte';
  import HelpDialog from './components/HelpDialog.svelte';
  import ThemeDialog from './components/ThemeDialog.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import Toasts from './components/Toasts.svelte';
  import PageHeader from './components/PageHeader.svelte';
  import PageState from './components/PageState.svelte';
  import ThemeMenu from './components/ThemeMenu.svelte';
  import { regions } from './lib/announce.svelte';
  import { setTitle } from './lib/head';
  import { loadMode, runMode, telHref } from './lib/mode.svelte';
  import { forbiddenText, installLinkInterception, mainNav, matchRoute, mayOpen, navigate, router, type RouteName } from './lib/router.svelte';
  import { loadSession, session, signOut, signOutGuard } from './lib/session.svelte';
  import { stopAllStreams } from './lib/stream';
  import { shell } from './lib/shell.svelte';
  import { isPlainKey, isTypingTarget } from './lib/shortcuts';
  import { safeNext } from './lib/signin';
  import { publishHeight } from './lib/publishHeight';
  import { PRIVACY_LINKED } from './lib/site';
  import { showToast } from './lib/toast.svelte';
  import { Landing, SCREENS, screenKey, type Screen } from './screens';

  const year = __BUILD_YEAR__;

  let menuOpen = $state(false);
  let menuButton: HTMLButtonElement | undefined = $state();

  const match = $derived(matchRoute(router.path));
  const route = $derived(match.route);
  const signedIn = $derived(session.status === 'signed-in');
  // An EventSource cannot see a 401: once the session ends, stop every
  // stream so a signed-out page neither polls nor reconnects.
  $effect(() => {
    if (session.status === 'signed-out') stopAllStreams();
  });
  const isOrganiser = $derived(organises(session.user?.role));
  // v4 P1 (V4-8): a signed-out / is the landing page, not a bounce to sign-in.
  const showLanding = $derived(route.name === 'home' && session.status === 'signed-out');
  const needsSignIn = $derived(route.auth && session.status === 'signed-out' && !showLanding);
  const leaveSignIn = $derived(route.name === 'signin' && signedIn);
  // A contest problem in the workspace: on a phone the student's link row
  // gives way to the workspace strip.
  const inWorkspace = $derived(shell.workspace && route.name.startsWith('contest'));
  // Organisers land on their own contest list; "/" is the student list.
  const organiserHome = $derived(route.name === 'home' && isOrganiser);
  /** False when the row names a role the person signed in does not have. */
  const allowed = $derived(mayOpen(route, session.user?.role));
  const nav = $derived(session.user ? mainNav(session.user.role) : []);

  // A screen sets its own title; the forbidden state stands in for it.
  $effect(() => {
    if (signedIn && !allowed) setTitle(route.title);
  });

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
    } else if (organiserHome) {
      redirect('/admin/contests');
    }
  });

  $effect(() => {
    const path = router.path;
    if (session.status === 'loading') return;
    const wasRedirect = redirectedTo === path;
    redirectedTo = null;
    if (lastPath !== null && lastPath !== path) {
      menuOpen = false;
      if (!wasRedirect) tick().then(focusHeading);
    }
    lastPath = path;
  });

  /**
   * Moves focus to the new page's heading. A lazily loaded screen has no
   * heading yet, so it waits a moment for one, and takes focus only if
   * nothing else has it by then.
   */
  function focusHeading() {
    const heading = document.querySelector<HTMLElement>('main h1');
    if (heading) {
      heading.focus();
      return;
    }
    const main = document.getElementById('main');
    if (!main || typeof MutationObserver === 'undefined') return;
    // The link that was pressed still holds focus; anything else means the person moved on.
    const before = document.activeElement;
    const observer = new MutationObserver(() => {
      const h1 = main.querySelector<HTMLElement>('h1');
      if (!h1) return;
      observer.disconnect();
      const now = document.activeElement;
      if (now === null || now === document.body || now === before) h1.focus();
    });
    observer.observe(main, { childList: true, subtree: true });
    setTimeout(() => observer.disconnect(), 3000);
  }

  /**
   * The current row's screen. One loaded with import() renders at once on
   * the next visit, so returning to a screen never shows the loading line
   * again. The path's parameters are its props.
   */
  const screenDef = $derived(SCREENS[route.name]);
  const loaded = $state<Partial<Record<RouteName, Screen>>>({});
  let failedScreen = $state<RouteName | null>(null);
  const Current = $derived(screenDef.component ?? loaded[route.name]);
  const currentKey = $derived(screenKey(screenDef, match.params));
  $effect(() => {
    const name = route.name;
    const def = SCREENS[name];
    // It loads beside GET /api/me, and never for someone the row turns away.
    if (!def.load || loaded[name] || (session.status !== 'loading' && !allowed)) return;
    failedScreen = null;
    def.load().then(
      (s) => (loaded[name] = s),
      () => (failedScreen = name),
    );
  });

  onMount(() => {
    const removeInterception = installLinkInterception();
    loadSession().then((err) => {
      if (err) showToast(err.message, 'error');
    });
    void loadMode();
    return removeInterception;
  });

  function onKeydown(event: KeyboardEvent) {
    // A dialog opened from the menu takes its own Escape.
    const dialogOpen = document.querySelector('dialog[open]') !== null;
    if (event.key === 'Escape' && menuOpen && !event.defaultPrevented && !dialogOpen) {
      menuOpen = false;
      menuButton?.focus();
    } else if (event.key === '?' && !event.defaultPrevented && isPlainKey(event) && !isTypingTarget(event.target) && !dialogOpen) {
      event.preventDefault();
      menuOpen = false;
      shell.help = true;
    }
  }

  // Sign out during a live contest asks first; the draft is saved before the session ends either way.
  let signOutAsk = $state<string | null>(null);

  async function onSignOut() {
    menuOpen = false;
    const why = signOutGuard();
    if (why !== null) {
      signOutAsk = why;
      return;
    }
    await finishSignOut();
  }

  // The dialog gives focus back to "Sign out", which closed with its menu; the account button takes it instead.
  function cancelSignOut() {
    signOutAsk = null;
    setTimeout(() => {
      const trigger = [...document.querySelectorAll<HTMLElement>('.bar .trigger, .bar .menu-button')].find((el) => el.offsetParent !== null);
      (trigger ?? document.querySelector<HTMLElement>('main h1'))?.focus();
    }, 0);
  }

  async function finishSignOut() {
    signOutAsk = null;
    // The page has signed out already; only the server's copy may still be open.
    if (await signOut()) showToast('Signed out on this device, but the server did not confirm it. To end the session everywhere, sign in and sign out again once you are online.', 'error');
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="page">
  <a class="skip" href="#main">Skip to content</a>

  <!-- Its real height, testing banner and wrapped rows included, lets the workspace fill the rest of the window. -->
  <!-- Inside a contest problem the workspace's own row (under 1024 px) or bar replaces the site header. -->
  <header class="bar" class:hide-narrow={inWorkspace} use:publishHeight={'--shell-head'}>
    {#if runMode.mode === 'testing'}
      <p class="mode-banner" role="note">
        Testing mode: sign-in is off and anyone at this machine can sign in as anyone. Do not use it for a real contest.
      </p>
    {/if}
    <div class="inner bar-inner">
      <a class="wordmark" href="/" translate="no">podium</a>

      {#if signedIn && session.user}
        {#if isOrganiser}
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
        {/if}

        <div
          id="site-menu"
          class="menu"
          class:collapsible={isOrganiser}
          class:student-menu={!isOrganiser}
          class:in-workspace={inWorkspace}
          class:open={menuOpen}
        >
          <nav class="nav" aria-label="Main">
            <ul>
              {#each nav as l (l.href)}
                <li><a href={l.href} aria-current={l.section === route.section ? 'page' : undefined}>{l.label}</a></li>
              {/each}
            </ul>
          </nav>
          {#if isOrganiser}
            <div class="account-inline"><AccountMenu user={session.user} inline onsignout={onSignOut} /></div>
          {/if}
        </div>
        <div class="account-popover" class:organiser={isOrganiser}>
          <AccountMenu user={session.user} onsignout={onSignOut} />
        </div>
      {:else}
        <div class="signed-out-theme"><ThemeMenu /></div>
      {/if}
    </div>
  </header>

  <main
    id="main"
    class="inner main"
    class:main-workspace={route.name === 'problem-try' || inWorkspace}
    tabindex="-1"
  >
    {#if session.status === 'loading' || needsSignIn || leaveSignIn || organiserHome}
      <p class="visually-hidden" role="status">Loading</p>
    {:else if showLanding}
      <Landing />
    {:else if !allowed}
      <PageHeader title={route.title} />
      <PageState kind="forbidden" message={forbiddenText(route)} />
    {:else if Current}
      {#key currentKey}
        <Current {...match.params} />
      {/key}
    {:else if failedScreen === route.name}
      <!-- A chunk that failed (often a new release) needs a full reload, not another import. -->
      <PageState kind="error" message="This page could not load. Reload to try again." onretry={() => location.reload()} />
    {:else}
      <PageState kind="loading" message="Loading the page" />
    {/if}
  </main>

  <footer class="inner foot">
    <!-- Sign-in carries the same line in its own column. -->
    {#if runMode.contactEmail !== '' && route.name !== 'signin'}
      <p class="contact">
        {#if runMode.organisation.name !== ''}{`${runMode.organisation.name}. `}{/if}Contact the organisers:
        <a href="mailto:{runMode.contactEmail}">{runMode.contactEmail}</a>{#if runMode.organisation.phone !== ''},
          <a href={telHref(runMode.organisation.phone)}>{runMode.organisation.phone}</a>{/if}
      </p>
    {/if}
    <ul class="foot-links">
      {#if PRIVACY_LINKED}<li><a href="/privacy">Privacy</a></li>{/if}
      <li class="keys">
        <button type="button" class="btn btn-quiet btn-compact" aria-keyshortcuts="?" onclick={() => (shell.help = true)}>
          Help
        </button>
      </li>
      <li><span>&copy; {year} podium</span></li>
    </ul>
  </footer>
</div>

<Toasts />
<!-- The app's two announcer regions (lib/announce.svelte.ts). Always present, so the first message is spoken too. -->
<div class="visually-hidden" aria-live="polite" data-announcer="polite">{regions.polite}</div>
<div class="visually-hidden" aria-live="assertive" data-announcer="assertive">{regions.assertive}</div>
<HelpDialog open={shell.help} organiser={isOrganiser} onclose={() => (shell.help = false)} />
{#if signedIn}<ThemeDialog open={shell.theme} onclose={() => (shell.theme = false)} />{/if}
<!-- Only a contest workspace asks before sign out, so the dialog exists only beside one. -->
{#if shell.workspace}
  <ConfirmDialog
    open={signOutAsk !== null}
    title="Sign out during the contest?"
    body={signOutAsk ?? ''}
    confirmLabel="Sign out"
    onconfirm={() => void finishSignOut()}
    oncancel={cancelSignOut}
  />
{/if}

<style>
  .page {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto 1fr auto;
    min-height: 100vh;
    /* body pads the safe areas (v4 U9), so the shell fills what is left. */
    min-height: calc(100dvh - env(safe-area-inset-top) - env(safe-area-inset-bottom));
  }

  .skip {
    position: absolute;
    left: var(--gutter);
    top: var(--space-2);
    z-index: var(--z-skip);
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

  /* One row from 768 px: word-mark, nav, then the account button. */
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
    min-width: 0;
  }

  .account-inline {
    display: none;
  }

  .account-popover,
  .signed-out-theme {
    margin-left: auto;
  }

  .nav ul {
    display: flex;
    flex-wrap: wrap;
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
    white-space: nowrap;
    transition: color var(--dur-fast) var(--ease);
  }

  .nav a:hover {
    color: var(--ink);
  }

  .nav a[aria-current='page'] {
    color: var(--ink);
    text-decoration: underline;
    text-decoration-color: var(--act);
    text-decoration-thickness: calc(2 * var(--rule));
    text-underline-offset: 0.45em;
  }

  .main {
    padding-block: var(--space-6) var(--space-7);
  }

  .main:focus {
    outline: none;
  }

  /* The workspace fills the window below the header. */
  .bar.hide-narrow {
    display: none;
  }

  .main-workspace {
    padding-block: var(--space-3);
  }

  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-5);
    padding-block: var(--space-5);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  .contact {
    overflow-wrap: anywhere;
  }

  .foot-links {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-4);
    margin-left: auto;
    padding: 0;
    list-style: none;
  }

  .foot-links a {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
  }

  .keys .btn {
    color: inherit;
  }

  /* Touch screens without a keyboard have no use for the list. */
  @media (hover: none) and (pointer: coarse) {
    .keys {
      display: none;
    }
  }

  /* Phones: a student keeps the links and the account button in the row;
     an organiser's nav and account items go behind Menu. */
  @media (width < 768px) {
    .bar-inner {
      gap: var(--space-2) var(--space-3);
    }

    .wordmark {
      margin-right: auto;
    }

    .menu {
      flex: 0 1 auto;
    }

    .menu-button {
      display: inline-flex;
    }

    .account-popover.organiser {
      display: none;
    }

    .menu.collapsible {
      display: none;
      flex-basis: 100%;
      flex-direction: column;
      align-items: stretch;
      gap: var(--space-3);
      padding-block: var(--space-2) var(--space-4);
      border-top: var(--rule) solid var(--line);
    }

    .menu.collapsible.open {
      display: flex;
    }

    .collapsible .nav ul {
      flex-direction: column;
      gap: 0;
    }

    .collapsible .nav a {
      width: 100%;
      padding-inline: 0;
    }

    .collapsible .account-inline {
      display: block;
      padding-top: var(--space-3);
      border-top: var(--rule) solid var(--line);
    }

    .account-popover {
      margin-left: 0;
    }

    /* A student's links take their own row under the word-mark and Account. */
    .menu.student-menu {
      flex-basis: 100%;
      order: 1;
    }

    /* In the contest workspace that row would push the problem down; the
       strip has previous, next and the clock, and the word-mark goes home. */
    .menu.student-menu.in-workspace {
      display: none;
    }

    .student-menu .nav ul {
      gap: 0 var(--space-3);
    }

    .student-menu .nav a {
      padding-inline: 0;
    }

    .foot-links {
      margin-left: 0;
    }
  }
</style>
