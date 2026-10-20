<!--
  NotFound: shown for any path the router does not know (v2 N5, audit
  04-13). Echoes the address that failed and offers the links that fit the
  visitor: "Your contests" for a student, "Contests" and "Problem bank" for
  an organiser, and "Sign in" when nobody is signed in. Takes no props.
-->
<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import { setTitle } from '../lib/head';
  import { router } from '../lib/router.svelte';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';

  const role = $derived(session.status === 'signed-in' ? (session.user?.role ?? 'student') : null);
  const links = $derived(
    role === null
      ? [{ href: '/signin', label: 'Sign in' }]
      : role === 'student'
        ? [{ href: '/', label: 'Your contests' }]
        : [
            { href: '/admin/contests', label: 'Contests' },
            { href: '/admin/problems', label: 'Problem bank' },
          ],
  );

  onMount(() => setTitle(routeTitle('not-found')));
</script>

<PageHeader title="Page not found" />
<p class="text">Nothing lives at <span class="mono path">{router.path}</span>.</p>
<ul class="links">
  {#each links as l (l.href)}<li><a href={l.href}>{l.label}</a></li>{/each}
</ul>

<style>
  .text {
    margin-top: var(--space-4);
    overflow-wrap: anywhere;
  }

  .links {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-5);
    margin-top: var(--space-3);
    padding: 0;
    list-style: none;
  }

  .links a {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
  }
</style>
