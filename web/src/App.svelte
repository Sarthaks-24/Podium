<!--
  App: the M0 shell. A header with the podium word-mark (a link home) and the
  theme control, a main area with the page heading and a live line that says
  whether the Go server answers /healthz, and a footer with the build year.
  There is no router yet; later milestones replace the main area with routed
  screens. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import ThemeToggle from './lib/ThemeToggle.svelte';
  import { checkHealth, type HealthState } from './lib/health';

  const year = __BUILD_YEAR__;
  let health = $state<HealthState>('checking');

  onMount(() => {
    let active = true;
    checkHealth().then((result) => {
      if (active) health = result;
    });
    return () => {
      active = false;
    };
  });
</script>

<div class="page">
  <header class="bar">
    <div class="inner bar-inner">
      <a class="wordmark" href="/">podium</a>
      <ThemeToggle />
    </div>
  </header>

  <main class="inner main">
    <h1>Podium</h1>
    <p class="status" data-state={health} role="status">
      {#if health === 'checking'}
        Checking the server
      {:else if health === 'up'}
        Server is up
      {:else if import.meta.env.DEV}
        Server is not reachable. Start it with <code>make dev</code>.
      {:else}
        Server is not reachable. Try again in a minute.
      {/if}
    </p>
  </main>

  <footer class="inner foot">
    <p>&copy; {year} podium</p>
  </footer>
</div>

<style>
  .page {
    display: grid;
    grid-template-rows: auto 1fr auto;
    min-height: 100vh;
    min-height: 100dvh;
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
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    min-height: var(--space-7);
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

  .main {
    padding-block: var(--space-7);
  }

  .status {
    margin-top: var(--space-3);
    color: var(--ink-muted);
  }

  .status[data-state='up'] {
    color: var(--ok);
  }

  .status[data-state='down'] {
    color: var(--bad);
  }

  .foot {
    padding-block: var(--space-5);
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }
</style>
