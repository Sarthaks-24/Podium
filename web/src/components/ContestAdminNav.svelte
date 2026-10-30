<!--
  ContestAdminNav: the header of the organiser screens for one contest:
  the trail, the contest's title with its state word, and the tabs Setup,
  Live (with the unanswered count) and Results, then "Student view". The
  count comes from the `unanswered` prop when the screen has it, otherwise
  from one request, and a failed request shows no count. The similar-code
  page has no tab and is named in the trail. The links wrap onto a second
  row on narrow screens, so the page never scrolls sideways at 320 px.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { getJSON } from '../lib/api';
  import { organiserHome, organiserState, type Clarification, type ContestState } from '../lib/contests';
  import PageHeader from './PageHeader.svelte';

  interface Props {
    id: string | number;
    slug: string;
    title: string;
    state: ContestState | '';
    /** False shows the state word "Draft"; left out, the state alone decides. */
    published?: boolean;
    current: 'settings' | 'console' | 'results' | 'integrity';
    unanswered?: number;
    actions?: Snippet;
  }

  let { id, slug, title, state: contestState, published, current, unanswered, actions }: Props = $props();

  let fetched = $state<number | null>(null);
  const count = $derived(unanswered ?? fetched ?? 0);

  const base = $derived(`/admin/contests/${encodeURIComponent(String(id))}`);
  const links = $derived([
    { id: 'settings', label: 'Setup', href: base },
    { id: 'console', label: 'Live', href: `${base}/console` },
    { id: 'results', label: 'Results', href: `${base}/results` },
  ] as const);
  const here = $derived(current === 'integrity' ? 'Similar code' : (links.find((l) => l.id === current)?.label ?? ''));

  $effect(() => {
    if (unanswered !== undefined) return;
    // The page path is /admin/contests/{id}; the API lives under /api.
    const url = `/api${base}/clarifications`;
    let live = true;
    getJSON<{ clarifications: Clarification[] }>(url)
      .then((data) => {
        if (live) fetched = (data.clarifications ?? []).filter((q) => !q.answer).length;
      })
      .catch(() => {
        if (live) fetched = null;
      });
    return () => {
      live = false;
    };
  });
</script>

<PageHeader
  {title}
  crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: title, href: contestState ? organiserHome({ id, state: contestState, published }) : base }, { label: here }]}
  state={contestState ? organiserState({ state: contestState, published }) : null}
  {actions}
>
  {#snippet nav()}
    <nav class="sub" aria-label="Contest">
      <ul>
        {#each links as l (l.id)}
          <li>
            <a href={l.href} aria-current={current === l.id ? 'page' : undefined}
              >{l.label}{#if l.id === 'console' && count > 0}<span class="count mono">{count}</span><span class="visually-hidden">
                  unanswered {count === 1 ? 'question' : 'questions'}</span
                >{/if}</a
            >
          </li>
        {/each}
        <li class="student-view"><a href="/contests/{encodeURIComponent(slug)}">Student view</a></li>
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
    flex-wrap: wrap;
    gap: 0 var(--space-1);
    padding: 0;
    list-style: none;
  }

  .sub a {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h-touch);
    padding-inline: var(--space-3);
    border-bottom: calc(2 * var(--rule)) solid transparent;
    border-radius: var(--radius) var(--radius) 0 0;
    color: var(--ink-muted);
    font-weight: 500;
    text-decoration: none;
  }

  .sub a:hover {
    color: var(--ink);
  }

  /* State is a shape: a border, which forced-colours mode keeps. */
  .sub a[aria-current='page'] {
    border-bottom-color: var(--act);
    color: var(--ink);
  }

  /* "Student view" leaves the organiser screens, so it sits apart at the end. */
  .student-view {
    margin-left: auto;
  }

  .count {
    min-width: 1.5em;
    padding: 0 var(--space-1);
    border-radius: var(--radius);
    background: var(--act);
    color: var(--act-ink);
    font-size: var(--fs-0);
    line-height: var(--lh-tight);
    text-align: center;
  }

  @media (forced-colors: active) {
    .sub a {
      border-bottom-color: Canvas;
    }

    .sub a[aria-current='page'] {
      border-bottom-color: Highlight;
    }

    .count {
      border: var(--rule) solid CanvasText;
    }
  }

  /* Phones: narrower padding; the links wrap rather than scroll. */
  @media (width < 768px) {
    .sub a {
      padding-inline: var(--space-2);
    }

    .student-view {
      margin-left: 0;
    }
  }
</style>
