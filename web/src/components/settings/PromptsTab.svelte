<!--
  PromptsTab: the "AI prompts" tab of the Dashboard. An admin sees the
  Preferences box; the owner also sees the editors for the rules of the
  wizard prompt and of the prompt for another AI. GET /api/admin/ai/prompts
  loads each time the tab is shown, so it never shows a stale list; each
  save, restore or reset replaces its kind with the server's answer. Props:
  `active` (the tab is shown).
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import PreferencesBox from '../ai/PreferencesBox.svelte';
  import PromptEditor from '../ai/PromptEditor.svelte';
  import PageState from '../PageState.svelte';
  import { errorText, getJSON, isSignedOut } from '../../lib/api';
  import type { PromptKindData, PromptsData } from '../../lib/prompts';

  interface Props {
    active: boolean;
  }

  let { active }: Props = $props();

  let kinds = $state<PromptKindData[]>([]);
  let status = $state<'loading' | 'ready' | 'error'>('loading');
  let error = $state('');

  async function load() {
    try {
      kinds = (await getJSON<PromptsData>('/api/admin/ai/prompts')).kinds ?? [];
      status = 'ready';
    } catch (err) {
      if (isSignedOut(err)) return;
      error = errorText(err);
      status = 'error';
    }
  }

  function replace(next: PromptKindData) {
    kinds = kinds.map((k) => (k.kind === next.kind ? next : k));
  }

  $effect(() => {
    if (active) untrack(() => void load());
  });

  const preferences = $derived(kinds.find((k) => k.kind === 'preferences'));
  const rules = $derived(kinds.filter((k) => k.kind !== 'preferences' && k.can_edit));
</script>

{#if status === 'loading'}
  <PageState kind="loading" message="Loading the AI prompts" />
{:else if status === 'error'}
  <PageState kind="error" message={error} onretry={() => void load()} />
{:else}
  {#if preferences}<PreferencesBox data={preferences} onchange={replace} onreload={load} />{/if}
  {#each rules as k (k.kind)}
    <PromptEditor data={k} onchange={replace} onreload={load} />
  {/each}
{/if}
