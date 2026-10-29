<!--
  App: the shell around every screen. The header holds the podium
  word-mark (a link home), the main navigation and one account control
  (components/AccountMenu). Navigation is named by job: students get
  "Contests"; admins and owners get "Contests" (the organiser list at
  /admin/contests, where "/" sends them), "Problem bank" (also current on
  the AI wizard), and for owners "People". From
  768 px everything fits one row; under 768 px an organiser's nav and
  account items collapse behind a Menu button that opens a panel (Escape
  closes it), while a student keeps the links and the account button in
  the row, except in the contest workspace, where the workspace strip
  takes that space. Signed out, only the theme control shows.

  The main area shows the screen for the current path from
  lib/router.svelte.ts, after the session from GET /api/me has loaded:
  signed-out visitors to a protected path are sent to /signin with a
  ?next= back, and signed-in visitors to /signin are sent on. After each
  client-side navigation focus moves to the new page's heading. The footer
  carries the organisation's name and "Contact the organisers: {email}"
  (and its phone as a tel: link) when the server names them, the Privacy
  link once the owner approves the text (lib/site.ts), a "Help"
  button and the build year; "?" anywhere outside a text field
  opens the same dialog (components/HelpDialog). The two announcer
  regions of lib/announce.svelte.ts live at the end. Takes no props.
-->
<script lang="ts">
  import { onMount, tick, type Component } from 'svelte';
  import AccountMenu from './components/AccountMenu.svelte';
  import HelpDialog from './components/HelpDialog.svelte';
  import ThemeDialog from './components/ThemeDialog.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import Toasts from './components/Toasts.svelte';
  import PageState from './components/PageState.svelte';
  import ThemeMenu from './components/ThemeMenu.svelte';
  import { regions } from './lib/announce.svelte';
  import { loadMode, runMode, telHref } from './lib/mode.svelte';
  import { installLinkInterception, matchRoute, navigate, router } from './lib/router.svelte';
  import { loadSession, session, signOut, signOutGuard } from './lib/session.svelte';
  import { stopAllStreams } from './lib/stream';
  import { shell } from './lib/shell.svelte';
  import { isPlainKey, isTypingTarget } from './lib/shortcuts';
  import { safeNext } from './lib/signin';
  import { publishHeight } from './lib/publishHeight';
  import { PRIVACY_LINKED } from './lib/site';
  import { showToast } from './lib/toast.svelte';
  import ContestPage from './routes/ContestPage.svelte';
  import Home from './routes/Home.svelte';
  import NotFound from './routes/NotFound.svelte';
  import Privacy from './routes/Privacy.svelte';
  import SignIn from './routes/SignIn.svelte';
  import StandingsPage from './routes/StandingsPage.svelte';

  const year = __BUILD_YEAR__;

  let menuOpen = $state(false);
  let menuButton: HTMLButtonElement | undefined = $state();

  const match = $derived(matchRoute(router.path));
  const route = $derived(match.route);
  const signedIn = $derived(session.status === 'signed-in');
  // An EventSource cannot see a 401: once the session ends, stop every
  // stream so a signed-out page neither polls nor reconnects (C1-041).
  $effect(() => {
    if (session.status === 'signed-out') stopAllStreams();
  });
  const isOwner = $derived(session.user?.role === 'owner');
  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const needsSignIn = $derived(route.auth && session.status === 'signed-out');
  const leaveSignIn = $derived(route.name === 'signin' && signedIn);
  // A contest problem in the workspace: on a phone the student's link row
  // gives way to the workspace strip (usability audit S7).
  const inWorkspace = $derived(shell.workspace && route.name.startsWith('contest'));
  // Organisers land on their own contest list; "/" is the student list (N1).
  const organiserHome = $derived(route.name === 'home' && isOrganiser);

  /** The main navigation for the signed-in role, by job (N1). */
  const navLinks = $derived.by(() => {
    const name = route.name;
    if (!isOrganiser) {
      return [{ href: '/', label: 'Contests', current: ['home', 'contest', 'contest-problem', 'standings'].includes(name) }];
    }
    const links = [
      {
        href: '/admin/contests',
        label: 'Contests',
        current: ['admin-contests', 'admin-contest', 'contest-console', 'contest-results', 'contest-integrity', 'integrity-pair'].includes(name),
      },
      { href: '/admin/problems', label: 'Problem bank', current: ['problems', 'problem', 'problem-try', 'ai-new'].includes(name) },
    ];
    if (isOwner) links.push({ href: '/admin/people', label: 'People', current: name === 'people' });
    return links;
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
   * Screens loaded with import() on first visit (F8.1): every organiser
   * screen, so a student's first load carries none of them. The student contest screens stay
   * in the entry chunk. A loaded module renders at once on the next visit,
   * so returning to a screen never shows the loading line again.
   */
  type ScreenProps = { id?: string; signal?: string };
  type Screen = Component<ScreenProps>;
  const screen = (load: () => Promise<{ default: unknown }>) => async () => (await load()).default as Screen;
  const lazyScreens: Record<string, () => Promise<Screen>> = {
    people: screen(() => import('./routes/People.svelte')),
    problems: screen(() => import('./routes/ProblemBank.svelte')),
    problem: screen(() => import('./routes/ProblemEditor.svelte')),
    'problem-try': screen(() => import('./routes/ProblemTry.svelte')),
    'ai-new': screen(() => import('./routes/AiWizard.svelte')),
    'admin-contests': screen(() => import('./routes/AdminContests.svelte')),
    'admin-contest': screen(() => import('./routes/ContestEditor.svelte')),
    'contest-console': screen(() => import('./routes/ContestConsole.svelte')),
    'contest-results': screen(() => import('./routes/ContestResults.svelte')),
    'contest-integrity': screen(() => import('./routes/ContestIntegrity.svelte')),
    'integrity-pair': screen(() => import('./routes/IntegrityPair.svelte')),
  };
  const loadedScreens = $state<Record<string, Screen>>({});
  let failedScreen = $state<string | null>(null);
  const lazyName = $derived(Object.hasOwn(lazyScreens, route.name) ? route.name : null);
  /** The lazily loaded screen's props; a change of any remounts it. */
  const lazyProps = $derived.by((): ScreenProps => {
    const p = match.params;
    if (route.name === 'integrity-pair') return { id: p.id, signal: p.signal };
    return p.id === undefined ? {} : { id: p.id };
  });
  $effect(() => {
    const name = lazyName;
    if (name === null || loadedScreens[name]) return;
    failedScreen = null;
    lazyScreens[name]().then(
      (s) => (loadedScreens[name] = s),
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

  // Sign out during a live contest asks first (P5 F7.6, N2); the draft is saved before the session ends either way.
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
    const err = await signOut();
    if (err) showToast(err.message, 'error');
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="page">
  <a class="skip" href="#main">Skip to content</a>

  <!-- Its real height, testing banner and wrapped rows included, lets the workspace fill the rest of the window (audit 2026-10-20 S2). -->
  <!-- B4, B6: inside a contest problem the workspace's own row (under 1024 px) or bar replaces the site header. -->
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
              {#each navLinks as l (l.href)}
                <li><a href={l.href} aria-current={l.current ? 'page' : undefined}>{l.label}</a></li>
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
    {:else if route.name === 'signin'}
      <SignIn />
    {:else if route.name === 'home'}
      <Home />
    {:else if route.name === 'contest' || route.name === 'contest-problem'}
      {#key match.params.slug}
        <ContestPage slug={match.params.slug} label={match.params.label ?? ''} />
      {/key}
    {:else if route.name === 'standings'}
      {#key match.params.slug}
        <StandingsPage slug={match.params.slug} />
      {/key}
    {:else if lazyName !== null}
      {@const Screen = loadedScreens[lazyName]}
      {#if Screen}
        {#key `${lazyName}/${Object.values(lazyProps).join('/')}`}
          <Screen {...lazyProps} />
        {/key}
      {:else if failedScreen === lazyName}
        <!-- A chunk that failed (often a new release) needs a full reload, not another import (C2-069). -->
        <PageState kind="error" message="This page could not load. Reload to try again." onretry={() => location.reload()} />
      {:else}
        <PageState kind="loading" message="Loading the page" />
      {/if}
    {:else if route.name === 'privacy'}
      <Privacy />
    {:else}
      <NotFound />
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
<!-- Only a contest workspace asks before sign out (F7.6), so the dialog exists only beside one. -->
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
    min-height: 100dvh;
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

  /* One row from 768 px (N7): word-mark, nav, then the account button. */
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
     an organiser's nav and account items go behind Menu (N7, audit 04-15). */
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
