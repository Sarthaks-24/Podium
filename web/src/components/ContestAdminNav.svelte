<!--
  ContestAdminNav: the PageHeader of the organiser screens for one
  contest: the trail "Contests, the contest, the current screen", the
  contest's title as the page heading with its state word, and the contest
  sub-nav (v2 N3): Settings, Console with the count of unanswered
  questions, Results and Integrity, the current one marked with
  aria-current, then "Student view" at the end, the contest as a student
  sees it. The count comes from the `unanswered` prop when the screen
  already has it (the console), otherwise from one request to the
  contest's clarifications (until the contest response carries it, C10 in
  P3); a failed request shows no count. The links wrap onto a second row
  on narrow screens, so the page never scrolls sideways at 320 px.

  Props: `id`, `slug` (for "Student view"), `title`, `state` (a
  ContestState, or "" for none), `published` (false reads "Draft", and an
  ended contest with practice open reads "Ended, practice open":
  lib/contests organiserState), `current` ("settings", "console",
  "results" or "integrity"), `unanswered` (optional), and an optional
  `actions` snippet for the title row.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { getJSON } from '../lib/api';
  import { organiserState, type Clarification, type ContestState } from '../lib/contests';
  import PageHeader from './PageHeader.svelte';

  interface Props {
    id: string | number;
    slug: string;
    title: string;
    state: ContestState | '';
    /** False shows the state word "Draft" (audit O1); left out, the state alone decides. */
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
    { id: 'settings', label: 'Settings', href: base },
    { id: 'console', label: 'Console', href: `${base}/console` },
    { id: 'results', label: 'Results', href: `${base}/results` },
    { id: 'integrity', label: 'Integrity', href: `${base}/integrity` },
  ] as const);
  const here = $derived(links.find((l) => l.id === current)?.label ?? '');

  $effect(() => {
    if (unanswered !== undefined) return;
    // The page path is /admin/contests/{id}; the API lives under /api (audit O4).
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
  crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: title, href: base }, { label: here }]}
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

  /* State is a shape (F9): a border, which forced-colours mode keeps. */
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

  /* Phones: narrower padding; the links wrap rather than scroll (N3). */
  @media (width < 768px) {
    .sub a {
      padding-inline: var(--space-2);
    }

    .student-view {
      margin-left: 0;
    }
  }
</style>
