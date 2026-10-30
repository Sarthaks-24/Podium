<!--
  SignIn: the public sign-in page. One left-aligned column:
  the heading, a line saying which Google accounts may sign in (built from
  GET /api/auth/config), an error banner when Google sign-in came back with
  ?error= (rate_limited included), the "Continue with Google" link, and a
  contact line with the organisers' email and phone from the organisation
  details (nothing shows for a field not entered). The link is
  a full page navigation to /api/auth/google/start carrying the ?next= path
  this page was opened with. The heading names the organisation once an
  owner has entered it. The config comes from lib/mode.svelte.ts, loaded
  once for the whole app. When the server runs in testing mode the link
  is replaced by the TestSignIn form. The privacy link joins the contact
  line once the owner approves the text (lib/site.ts PRIVACY_LINKED).
  Signed-in visitors are redirected away by the app shell before this
  renders. Takes no props.
-->
<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import Message from '../components/Message.svelte';
  import TestSignIn from '../components/TestSignIn.svelte';
  import { loadMode, runMode, telHref } from '../lib/mode.svelte';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { PRIVACY_LINKED } from '../lib/site';
  import { router } from '../lib/router.svelte';
  import { audienceLine, errorMessage, parseErrorCode, safeNext, startHref } from '../lib/signin';

  const next = $derived(safeNext(router.query.get('next')));
  const errorCode = $derived(parseErrorCode(router.query.get('error')));
  const ready = $derived(runMode.status !== 'loading');
  const testing = $derived(runMode.mode === 'testing');
  const org = $derived(runMode.organisation);
  const error = $derived(ready ? errorMessage(errorCode, runMode.allowedDomains, runMode.contactEmail) : null);

  onMount(() => {
    setTitle(routeTitle('signin'));
    void loadMode();
  });
</script>

<div class="signin">
  <PageHeader title={org.name !== '' ? `Weekly programming contests for ${org.name}` : 'Weekly programming contests'} />
  <p class="audience" aria-live="polite">
    {#if ready && !testing}{audienceLine(runMode.allowedDomains)}{/if}
  </p>

  {#if error}
    <p class="banner banner-error error" role="alert"><Message parts={error} /></p>
  {/if}

  {#if testing}
    <p class="testing">This server is in testing mode. Pick any email and role; nothing is checked.</p>
    <TestSignIn {next} />
  {:else}
    <p class="action">
      <a class="btn btn-primary" href={startHref(next)}>Continue with Google</a>
    </p>
  {/if}

  <!-- FP.2 and FP.3: the way in for anyone who cannot sign in, from the organisation's own details. -->
  {#if runMode.contactEmail !== ''}
    <p class="contact muted">
      Cannot sign in? Contact the organisers: <a href="mailto:{runMode.contactEmail}">{runMode.contactEmail}</a>{#if org.phone !== ''}, or call
        <a href={telHref(org.phone)}>{org.phone}</a>{/if}.
    </p>
  {/if}
  {#if PRIVACY_LINKED}
    <p class="privacy muted"><a href="/privacy">What podium records about you</a></p>
  {/if}
</div>

<style>
  .signin {
    max-width: var(--measure-narrow);
    padding-top: var(--space-7);
  }

  .audience {
    min-height: calc(var(--fs-2) * var(--lh-body));
    margin-top: var(--space-3);
    color: var(--ink-muted);
    font-size: var(--fs-2);
  }

  .error {
    margin-top: var(--space-5);
  }

  .action,
  .testing {
    margin-top: var(--space-5);
  }

  .contact {
    margin-top: var(--space-6);
    overflow-wrap: anywhere;
  }

  .privacy {
    margin-top: var(--space-2);
  }
</style>
