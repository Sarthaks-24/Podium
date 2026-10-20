<!--
  Privacy: the /privacy page (v2 N4). Open to everyone, signed in or not.
  Until the owner approves the drafted notice (decision D85, P3 FP.1) it
  says the notice is being written and names the organisers' contact email
  from GET /api/auth/config, which the shell has already loaded. Nothing
  links here yet: lib/site.ts PRIVACY_LINKED turns on the footer and
  sign-in links once the text is approved. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { setTitle } from '../lib/head';
  import { runMode } from '../lib/mode.svelte';
  import { routeTitle } from '../lib/routing';

  onMount(() => setTitle(routeTitle('privacy')));
</script>

<PageHeader title="Privacy" />
<div class="text">
  <p>The privacy notice for podium is being written. It will say what podium records about you, who can see it and how long it is kept.</p>
  {#if runMode.contactEmail !== ''}
    <p>Until it is published, ask the organisers: <a href="mailto:{runMode.contactEmail}">{runMode.contactEmail}</a></p>
  {/if}
</div>

<style>
  .text {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-narrow);
    margin-top: var(--space-4);
    overflow-wrap: anywhere;
  }
</style>
