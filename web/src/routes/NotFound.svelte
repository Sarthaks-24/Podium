<!--
  NotFound: shown for any path the router does not know. Says the page does
  not exist and links to the contest list. Takes no props.
-->
<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount } from 'svelte';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';

  onMount(() => setTitle(routeTitle('not-found')));
</script>

<PageHeader title="Page not found" />
<p class="text">That page does not exist. <a href="/">Go to your contests.</a></p>

<style>
  .text {
    margin-top: var(--space-4);
  }
</style>
