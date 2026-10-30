<!--
  Privacy: the /privacy page. Open to everyone, signed in
  or not. The notice is drafted in src/content/privacy.md and waits for
  the owner's approval: until lib/site.ts PRIVACY_LINKED is
  true the page says the notice is being written and names the
  organisers' contact email, and nothing links here. "/privacy?draft"
  shows the drafted notice with a line saying it is a draft, so the owner
  can read it in place. The full notice renders the Markdown and, at its
  marker, a table of every retention period from GET /api/auth/config
  (lib/privacy.ts), never typed by hand, then who to ask.
  Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import Markdown from '../components/Markdown.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { setTitle } from '../lib/head';
  import { loadMode, runMode } from '../lib/mode.svelte';
  import { privacyParts } from '../lib/privacy';
  import { router } from '../lib/router.svelte';
  import { routeTitle } from '../lib/routing';
  import { PRIVACY_LINKED } from '../lib/site';

  const draft = $derived(!PRIVACY_LINKED && router.query.has('draft'));
  const show = $derived(PRIVACY_LINKED || draft);
  const parts = privacyParts();
  const askEmail = $derived(runMode.organisation.privacy_contact || runMode.contactEmail);

  onMount(() => {
    setTitle(routeTitle('privacy'));
    void loadMode();
  });
</script>

<PageHeader title="Privacy" />
{#if !show}
  <div class="text">
    <p>The privacy notice for podium is being written. It will say what podium records about you, who can see it and how long it is kept.</p>
    {#if runMode.contactEmail !== ''}
      <p>Until it is published, ask the organisers: <a href="mailto:{runMode.contactEmail}">{runMode.contactEmail}</a></p>
    {/if}
  </div>
{:else}
  <div class="notice">
    {#if draft}
      <p class="banner banner-info" role="note">Draft for the owner's approval. Nothing links to this page yet.</p>
    {/if}
    <Markdown source={parts.before} headingBase={1} />
    {#if runMode.status === 'loading'}
      <p class="muted" role="status">Loading the retention periods</p>
    {:else if runMode.retention.length === 0}
      <p>The list of retention periods could not load. Reload the page, or ask the organisers for it.</p>
    {:else}
      <div class="table-wrap retention">
        <table class="table">
          <caption class="visually-hidden">How long podium keeps each kind of data</caption>
          <thead>
            <tr><th scope="col">What</th><th scope="col">Kept for</th></tr>
          </thead>
          <tbody>
            {#each runMode.retention as r (r.id)}
              <tr><th scope="row">{r.data}</th><td>{r.kept}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
    {#if parts.after !== ''}<Markdown source={parts.after} headingBase={1} />{/if}
    {#if askEmail !== ''}
      <p>Questions about this notice: <a href="mailto:{askEmail}">{askEmail}</a>{#if runMode.organisation.name !== ''}{` (${runMode.organisation.name})`}{/if}.</p>
    {/if}
  </div>
{/if}

<style>
  .text,
  .notice {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-prose);
    margin-top: var(--space-4);
    overflow-wrap: anywhere;
  }

  .retention {
    margin-block: var(--space-2);
  }

  /* Each section of the notice starts with room above its heading. */
  .notice :global(h2) {
    margin-top: var(--space-6);
    margin-bottom: var(--space-2);
  }

  .retention tbody th {
    font-weight: 500;
  }
</style>
