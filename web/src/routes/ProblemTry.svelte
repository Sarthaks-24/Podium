<!--
  ProblemTry: an organiser tries a problem as a student would, at
  /admin/problems/{id}/try (cache/v1/m3-plan.md section 7a). It mounts the
  shared Workspace on the problem's workspace routes; submissions made here
  are the organiser's own and count in no contest. Students see a short
  explanation instead, because the problem bank is never shown to them.
  Props: `id`, the problem id from the address.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import Workspace from '../components/Workspace.svelte';
  import { setTitle } from '../lib/head';
  import { session } from '../lib/session.svelte';
  import { problemWorkspace } from '../lib/workspace';

  let { id }: { id: string } = $props();

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');

  onMount(() => {
    if (!isOrganiser) setTitle('Problems');
  });
</script>

{#if isOrganiser}
  <Workspace
    base={problemWorkspace(id)}
    backHref="/admin/problems/{encodeURIComponent(id)}"
    backLabel="Problem editor"
    note="You are trying this problem as a student. These submissions count in no contest."
  />
{:else}
  <h1 tabindex="-1">Problems</h1>
  <p class="lead">Only admins can manage problems. Ask an owner if you need access.</p>
{/if}

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }
</style>
