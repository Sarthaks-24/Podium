<!--
  Home: the signed-in landing page, "Your contests". Contests arrive in M4;
  until then it shows the empty state from design.md section 8. The app
  shell sends signed-out visitors to /signin first. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { setTitle } from '../lib/head';

  onMount(() => setTitle('Your contests'));
</script>

<h1 tabindex="-1">Your contests</h1>
<p class="empty muted">No contests yet. When an organiser schedules one, it appears here.</p>

<style>
  .empty {
    margin-top: var(--space-4);
  }
</style>
