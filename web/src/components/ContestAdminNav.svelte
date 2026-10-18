<!--
  ContestAdminNav: the header of the organiser screens for one
  contest: a link back to the contest admin list, the contest's title as
  the page heading with its state word, and links to Settings, Console,
  Results and Integrity, the current one marked with aria-current. Props:
  `id`, `title`, `state` (a state word, or ""), `current` ("settings",
  "console", "results" or "integrity").
-->
<script lang="ts">
  interface Props {
    id: string | number;
    title: string;
    state: string;
    current: 'settings' | 'console' | 'results' | 'integrity';
  }

  let { id, title, state, current }: Props = $props();

  const base = $derived(`/admin/contests/${encodeURIComponent(String(id))}`);
  const links = $derived([
    { id: 'settings', label: 'Settings', href: base },
    { id: 'console', label: 'Console', href: `${base}/console` },
    { id: 'results', label: 'Results', href: `${base}/results` },
    { id: 'integrity', label: 'Integrity', href: `${base}/integrity` },
  ] as const);
</script>

<a class="back" href="/admin/contests">Contest admin</a>
<div class="head">
  <h1 tabindex="-1">{title}</h1>
  {#if state}<span class="state">{state}</span>{/if}
</div>
<nav class="sub" aria-label="Contest">
  <ul>
    {#each links as l (l.id)}
      <li><a href={l.href} aria-current={current === l.id ? 'page' : undefined}>{l.label}</a></li>
    {/each}
  </ul>
</nav>

<style>
  .back {
    display: inline-block;
    margin-bottom: var(--space-2);
    border-radius: var(--radius);
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2) var(--space-4);
  }

  h1 {
    font-size: var(--fs-4);
    overflow-wrap: anywhere;
  }

  .state {
    color: var(--ink-muted);
    font-weight: 600;
  }

  .sub {
    margin-top: var(--space-4);
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
