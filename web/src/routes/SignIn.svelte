<!--
  SignIn: the public sign-in page (design.md 5.1). One left-aligned column:
  the heading, a line saying which Google accounts may sign in (built from
  GET /api/auth/config), an error banner when Google sign-in came back with
  ?error=, the "Continue with Google" link, and a contact line. The link is
  a full page navigation to /api/auth/google/start carrying the ?next= path
  this page was opened with. When the server runs in testing mode the link
  is replaced by the TestSignIn form. Signed-in visitors are redirected away
  by the app shell before this renders. Takes no props.
-->
<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import Message from '../components/Message.svelte';
  import TestSignIn from '../components/TestSignIn.svelte';
  import { getJSON } from '../lib/api';
  import { parseMode, runMode } from '../lib/mode.svelte';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { router } from '../lib/router.svelte';
  import { audienceLine, errorMessage, parseErrorCode, safeNext, startHref } from '../lib/signin';

  interface AuthConfig {
    allowed_domains: string[];
    contact_email: string;
    mode?: string;
  }

  let config = $state<AuthConfig | null>(null);
  let configFailed = $state(false);

  const next = $derived(safeNext(router.query.get('next')));
  const errorCode = $derived(parseErrorCode(router.query.get('error')));
  const domains = $derived(config?.allowed_domains ?? []);
  const contactEmail = $derived(config?.contact_email ?? '');
  const ready = $derived(config !== null || configFailed);
  const error = $derived(ready ? errorMessage(errorCode, domains, contactEmail) : null);

  onMount(() => {
    setTitle(routeTitle('signin'));
    let active = true;
    getJSON<AuthConfig>('/api/auth/config')
      .then((c) => {
        if (!active) return;
        config = { allowed_domains: c.allowed_domains ?? [], contact_email: c.contact_email ?? '', mode: c.mode };
        runMode.mode = parseMode(c.mode);
      })
      .catch(() => {
        if (active) configFailed = true;
      });
    return () => {
      active = false;
    };
  });
</script>

<div class="signin">
  <PageHeader title="Weekly programming contests" />
  <p class="audience" aria-live="polite">
    {#if ready && config?.mode !== 'testing'}{audienceLine(domains)}{/if}
  </p>

  {#if error}
    <p class="banner banner-error error" role="alert"><Message parts={error} /></p>
  {/if}

  {#if config?.mode === 'testing'}
    <p class="testing">This server is in testing mode. Pick any email and role; nothing is checked.</p>
    <TestSignIn {next} />
  {:else}
    <p class="action">
      <a class="btn btn-primary" href={startHref(next)}>Continue with Google</a>
    </p>
  {/if}

  {#if contactEmail !== ''}
    <p class="contact muted">
      Contact the organisers: <a href="mailto:{contactEmail}">{contactEmail}</a>
    </p>
  {/if}
</div>

<style>
  .signin {
    max-width: 40rem;
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
</style>
