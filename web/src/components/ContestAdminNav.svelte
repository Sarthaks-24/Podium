<!--
  ContestAdminNav: the PageHeader of the organiser screens for one
  contest: the trail "Contest admin, the contest, the current screen", the
  contest's title as the page heading with its state word, and links to
  Settings, Console, Results and Integrity, the current one marked with
  aria-current. Props: `id`, `title`, `state` (a ContestState, or "" for
  none), `current` ("settings", "console", "results" or "integrity"), and
  an optional `actions` snippet for the title row.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { stateWord, type ContestState } from '../lib/contests';
  import PageHeader from './PageHeader.svelte';

  interface Props {
    id: string | number;
    title: string;
    state: ContestState | '';
    current: 'settings' | 'console' | 'results' | 'integrity';
    actions?: Snippet;
  }

  let { id, title, state, current, actions }: Props = $props();

  const base = $derived(`/admin/contests/${encodeURIComponent(String(id))}`);
  const links = $derived([
    { id: 'settings', label: 'Settings', href: base },
    { id: 'console', label: 'Console', href: `${base}/console` },
    { id: 'results', label: 'Results', href: `${base}/results` },
    { id: 'integrity', label: 'Integrity', href: `${base}/integrity` },
  ] as const);
  const here = $derived(links.find((l) => l.id === current)?.label ?? '');
</script>

<PageHeader
  {title}
  crumbs={[{ label: 'Contest admin', href: '/admin/contests' }, { label: title, href: base }, { label: here }]}
  state={state ? { word: stateWord(state), kind: state } : null}
  {actions}
>
  {#snippet nav()}
    <nav class="sub" aria-label="Contest">
      <ul>
        {#each links as l (l.id)}
          <li><a href={l.href} aria-current={current === l.id ? 'page' : undefined}>{l.label}</a></li>
        {/each}
      </ul>
    </nav>
  {/snippet}
</PageHeader>

<style>
  .sub {
    border-bottom: var(--rule) solid var(--line);
  }

  .sub ul {
    display: flex;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  .sub a {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h-touch);
    padding-inline: var(--space-3);
    border-radius: var(--radius) var(--radius) 0 0;
    color: var(--ink-muted);
    font-weight: 500;
    text-decoration: none;
  }

  .sub a:hover {
    color: var(--ink);
  }

  .sub a[aria-current='page'] {
    color: var(--ink);
    box-shadow: inset 0 calc(-2 * var(--rule)) 0 var(--act);
  }

  .sub a[aria-current='page']:focus-visible {
    box-shadow:
      inset 0 calc(-2 * var(--rule)) 0 var(--act),
      var(--focus);
  }

  /* Phones: four tabs fit at 375 px with narrower padding. */
  @media (max-width: 767px) {
    .sub a {
      padding-inline: var(--space-2);
    }
  }
</style>
