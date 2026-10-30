<!--
  PageState: what a screen shows instead of its content, one of five kinds.
  - loading: a status line, shown only after 300 ms so a fast load never
    flashes; the live region is there from the start.
  - error: what happened in a banner, and a "Try again" button.
  - forbidden: why the screen is closed, then where to ask: the organisers'
    contact email as a mailto link when the server has one, otherwise the
    `ask` sentence.
  - missing: what is not there, and a link to the parent screen.
  - empty: what is not there yet, and the one next action (the `action`
    snippet) when there is one.
  Props: `kind`, `message`, `onretry` (error), `ask` (forbidden fallback),
  `parent` ({ href, label }, missing), `action` (snippet, empty), `delay`
  (loading, in ms).
-->
<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { runMode } from '../lib/mode.svelte';

  interface Props {
    kind: 'loading' | 'error' | 'forbidden' | 'missing' | 'empty';
    message: string;
    onretry?: () => void;
    ask?: string;
    parent?: { href: string; label: string } | null;
    action?: Snippet;
    delay?: number;
  }

  let { kind, message, onretry, ask = 'Ask an owner if you need access.', parent = null, action, delay = 300 }: Props = $props();

  let shown = $state(false);

  onMount(() => {
    if (kind !== 'loading') return;
    const timer = setTimeout(() => (shown = true), delay);
    return () => clearTimeout(timer);
  });
</script>

{#if kind === 'loading'}
  <p class="page-state muted" role="status">{shown ? message : ''}</p>
{:else if kind === 'error'}
  <div class="page-state banner banner-error" role="alert"><p>{message}</p></div>
  {#if onretry}<p class="retry"><button type="button" class="btn btn-secondary" onclick={onretry}>Try again</button></p>{/if}
{:else if kind === 'forbidden'}
  <p class="page-state">
    {message}
    {#if runMode.contactEmail}Write to <a href="mailto:{runMode.contactEmail}">{runMode.contactEmail}</a> if you need access.{:else}{ask}{/if}
  </p>
{:else if kind === 'missing'}
  <p class="page-state">{message} {#if parent}<a href={parent.href}>{parent.label}</a>{/if}</p>
{:else}
  <p class="page-state muted">{message}</p>
  {#if action}<div class="next">{@render action()}</div>{/if}
{/if}

<style>
  .page-state {
    max-width: var(--measure-narrow);
    margin-top: var(--space-4);
  }

  .retry,
  .next {
    margin-top: var(--space-3);
  }
</style>
