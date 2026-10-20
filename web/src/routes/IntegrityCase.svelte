<!--
  IntegrityCase: one integrity case at /admin/integrity/cases/{case}
  (integrity-plan.md sections 5 and 7, docs/INTEGRITY.md "Reviewing").
  From GET /api/admin/integrity/cases/{case}: the title and state, the
  students with whether the case was shared with them and their
  explanations (every saved version, with its time), the evidence copied
  when the case was opened (a later check cannot change it; its links open
  the case's own copy), and the decision log, oldest first, with each
  reviewer's name, which nothing can edit. Every change of state needs a
  note of at least 20 characters (PUT with {state, note,
  seen_explanations}, the latest explanation version of each student as
  loaded): "Share with the students" and "Take action" ask first. "Take
  action" stays disabled, with the reason, until the case is shared and
  every student explained since the latest share or had their 7 days run
  out, the rule the server applies. Clearing is always open, and a
  decided case can be reopened, which needs a new share. If a student
  changed their explanation since the page loaded, the server refuses the
  decision and the page offers a reload that keeps the note. Names and
  problem labels come from the contest's latest check when it loads.
  Props: `id` (the case).
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onDestroy, onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import IntegrityNote from '../components/IntegrityNote.svelte';
  import SignalTable from '../components/SignalTable.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import type { AdminContest } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import {
    MIN_NOTE,
    RESPONSE_DAYS,
    actionState,
    caseStateWord,
    latestExplanationAt,
    noteCounter,
    noteLength,
    seenExplanations,
    type Case,
    type CaseState,
    type CaseUser,
    type Decision,
    type ReviewUser,
    type Run,
  } from '../lib/review';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let kase = $state.raw<Case | null>(null);
  let run = $state.raw<Run | null>(null);
  let contestTitle = $state('');

  let note = $state('');
  let noteError = $state('');
  let actionError = $state('');
  let busy = $state(false);
  let noteField: HTMLTextAreaElement | undefined = $state();
  let confirm = $state<null | { state: CaseState; title: string; body: string; label: string; danger: boolean }>(null);
  // A student saved a newer explanation than the one on screen.
  let stale = $state(false);
  // The server's clock, ticking, for the response windows.
  let skew = 0;
  let now = $state(Date.now());
  let timer: ReturnType<typeof setInterval> | undefined;

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const api = $derived(`/api/admin/integrity/cases/${encodeURIComponent(id)}`);
  const back = $derived(kase ? `/admin/contests/${kase.contest_id}/integrity` : '/admin/contests');
  const action = $derived(kase ? actionState(kase, now) : { open: false, opensAt: null });
  const actionHelp = $derived.by(() => {
    if (!kase || action.open) return '';
    if (kase.state !== 'awaiting_student') return 'Take action opens once the case is shared with every student, so they can explain first.';
    const when = action.opensAt ? ` on ${formatDateTime(action.opensAt)}` : '';
    return `Take action opens when every student has explained since the case was shared, or when their ${RESPONSE_DAYS} days to explain end${when}.`;
  });
  const decided = $derived(kase?.state === 'cleared' || kase?.state === 'actioned');

  function waitingText(u: CaseUser): string {
    if (u.asked_at === null || u.asked_at === undefined || u.respond_by === null || u.respond_by === undefined) return '';
    const at = latestExplanationAt(u);
    if (at !== null && at >= u.asked_at) return '';
    return now >= u.respond_by ? 'Days to explain are over' : `Can explain until ${formatDateTime(u.respond_by)}`;
  }
  // Names for the evidence: the case's own students first, then the check's map.
  const users = $derived.by(() => {
    const out: Record<string, ReviewUser> = { ...(run?.users ?? {}) };
    for (const u of kase?.users ?? []) out[String(u.user_id)] = { name: u.name, email: out[String(u.user_id)]?.email ?? '' };
    return out;
  });

  function handleAuth(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    if (e.status === 403 && e.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  function messageOf(e: unknown): string {
    return e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
  }

  function setClock(serverNow: number | undefined) {
    if (serverNow) skew = serverNow - Date.now();
    now = Date.now() + skew;
  }

  async function load() {
    loadState = 'loading';
    try {
      const c = await getJSON<Case & { server_now?: number }>(api);
      const restore = keepHeadingFocus();
      setClock(c.server_now);
      kase = c;
      stale = false;
      setTitle(c.title, routeTitle('integrity-case'));
      loadState = 'ready';
      void restore();
      void loadContext(c.contest_id);
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Case not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
    }
  }

  // Names and labels are a convenience; the case reads without them.
  async function loadContext(contestId: number) {
    const contestApi = `/api/admin/contests/${contestId}`;
    const [r, c] = await Promise.allSettled([getJSON<Run>(`${contestApi}/integrity`), getJSON<AdminContest>(contestApi)]);
    if (r.status === 'fulfilled') run = r.value;
    if (c.status === 'fulfilled') contestTitle = c.value.title;
  }

  function byName(d: Decision): string {
    if (session.user && d.by === session.user.id) return 'you';
    if (d.by_name) return d.by_name;
    const u = run?.users?.[String(d.by)];
    return u?.name || u?.email || 'An organiser';
  }

  // Reloads the case after a refused decision, keeping the note.
  async function reload() {
    try {
      const c = await getJSON<Case & { server_now?: number }>(api);
      setClock(c.server_now);
      kase = c;
      stale = false;
      actionError = '';
    } catch (e) {
      if (!handleAuth(e)) actionError = messageOf(e);
    }
  }

  async function checkNote(): Promise<boolean> {
    actionError = '';
    noteError = noteLength(note) < MIN_NOTE ? `Write a note of at least ${MIN_NOTE} characters for the log first.` : '';
    if (noteError) {
      await tick();
      noteField?.focus();
      return false;
    }
    return true;
  }

  async function ask(state: CaseState) {
    if (!(await checkNote())) return;
    if (state === 'awaiting_student') {
      confirm = {
        state,
        title: 'Share with the students?',
        body: "Each student sees their own evidence, never another student's name or code, and can write an explanation.",
        label: 'Share with the students',
        danger: false,
      };
    } else if (state === 'actioned') {
      confirm = {
        state,
        title: 'Take action?',
        body: 'This closes the case with action taken. The log keeps your note, and nothing in it can be edited later.',
        label: 'Take action',
        danger: true,
      };
    } else {
      await change(state);
    }
  }

  const DONE: Record<CaseState, string> = {
    open: 'Reopened the case',
    awaiting_student: 'Shared with the students',
    cleared: 'Cleared the case',
    actioned: 'Action taken',
  };

  async function change(state: CaseState) {
    busy = true;
    actionError = '';
    try {
      const c = await sendJSON<Case & { server_now?: number }>('PUT', api, {
        state,
        note: note.trim(),
        seen_explanations: kase ? seenExplanations(kase) : {},
      });
      setClock(c.server_now);
      kase = c;
      note = '';
      noteError = '';
      showToast(DONE[state]);
      await tick();
      document.getElementById('decide-title')?.focus();
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.fields.note) noteError = e.fields.note;
      else actionError = messageOf(e);
      stale = e instanceof ApiError && e.code === 'explanation_changed';
    } finally {
      busy = false;
      confirm = null;
    }
  }

  onMount(() => {
    setTitle(routeTitle('integrity-case'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
    timer = setInterval(() => (now = Date.now() + skew), 30_000);
  });

  onDestroy(() => clearInterval(timer));
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Case" />
  <PageState kind="forbidden" message="Only owners and admins can review integrity." />
{:else if loadState !== 'ready' || !kase}
  <PageHeader
    title={loadState === 'missing' ? 'Case not found' : 'Case'}
    crumbs={[{ label: 'Contests', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Case not found' : 'Case' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the case" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no case with that number, or you cannot review its contest." parent={{ href: '/admin/contests', label: 'Go to the contest list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <PageHeader
    title={kase.title}
    crumbs={[
      { label: 'Contests', href: '/admin/contests' },
      { label: contestTitle || 'Contest', href: `/admin/contests/${kase.contest_id}` },
      { label: 'Integrity', href: back },
      { label: kase.title },
    ]}
    state={{ word: caseStateWord(kase.state), kind: kase.state }}
  />
  <p class="muted small dates">Opened <span class="mono">{formatDateTime(kase.created_at)}</span>, last changed <span class="mono">{formatDateTime(kase.updated_at)}</span>.</p>

  <div class="note"><IntegrityNote /></div>

  <section class="section" aria-labelledby="students-title">
    <h2 id="students-title">Students</h2>
    <div class="table-wrap students">
      <table class="table table-dense table-stack">
        <caption class="visually-hidden">Students in this case</caption>
        <thead>
          <tr>
            <th scope="col">Student</th>
            <th scope="col">Shared</th>
            <th scope="col">Explanation</th>
          </tr>
        </thead>
        <tbody>
          {#each kase.users as u (u.user_id)}
            {@const waiting = waitingText(u)}
            {@const versions = u.explanations ?? []}
            <tr>
              <th scope="row" class="s-name">{u.name}</th>
              <td class="s-shared">
                {#if u.shared_at !== null}Shared <span class="mono">{formatDateTime(u.asked_at ?? u.shared_at)}</span>{:else}<span class="muted">Not shared yet</span>{/if}
                {#if u.shared_at !== null && (u.asked_at === null || u.asked_at === undefined)}<span class="turn muted">Reopened: share again for a new turn</span>{/if}
                {#if waiting}<span class="turn muted">{waiting}</span>{/if}
              </td>
              <td class="s-text">
                {#if versions.length > 1}
                  <ol class="versions" aria-label="{u.name}'s explanations, oldest first">
                    {#each versions as v, i (v.created_at)}
                      <li>
                        <p class="muted small">{i === versions.length - 1 ? 'Latest version' : `Version ${i + 1}`}, written <span class="mono">{formatDateTime(v.created_at)}</span></p>
                        <p class="prose">{v.text}</p>
                      </li>
                    {/each}
                  </ol>
                {:else if u.explanation.trim() !== ''}
                  <p class="prose">{u.explanation}</p>
                  {#if u.explained_at !== null}<p class="muted small">Written <span class="mono">{formatDateTime(u.explained_at)}</span></p>{/if}
                {:else}
                  <span class="muted">{u.shared_at !== null ? 'No explanation yet' : 'Can explain once shared'}</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </section>

  <section class="section" aria-labelledby="evidence-title">
    <h2 id="evidence-title">Evidence</h2>
    <p class="muted small">Copied when the case was opened; a later check does not change it. Each student sees only their own part.</p>
    {#if kase.evidence.length === 0}
      <p class="muted">No evidence in this case.</p>
    {:else}
      <SignalTable signals={kase.evidence} {users} problems={run?.problems} contestId={kase.contest_id} caseId={kase.id} caption="Evidence in this case" showProblem />
    {/if}
  </section>

  <section class="section" aria-labelledby="decide-title">
    <h2 id="decide-title" tabindex="-1">{decided ? 'Reopen' : 'Decide'}</h2>
    <form class="decide" novalidate onsubmit={(e) => e.preventDefault()}>
      <div class="field">
        <label for="case-decision-note">Note for the log</label>
        <textarea
          id="case-decision-note"
          class="input"
          rows="4"
          bind:this={noteField}
          bind:value={note}
          aria-invalid={noteError ? 'true' : undefined}
          aria-describedby="case-decision-help case-decision-count{noteError ? ' case-decision-error' : ''}"
        ></textarea>
        <p id="case-decision-help" class="field-help">Every change needs a reason. The log keeps it, and nothing in the log can be edited later.</p>
        <p id="case-decision-count" class="field-help">{noteCounter(note)}</p>
        {#if noteError}<p id="case-decision-error" class="field-error">{noteError}</p>{/if}
      </div>
      <div class="buttons">
        {#if decided}
          <button type="button" class="btn btn-secondary" disabled={busy} onclick={() => ask('open')}>Reopen the case</button>
        {:else}
          {#if kase.state === 'open'}
            <button type="button" class="btn btn-primary" disabled={busy} onclick={() => ask('awaiting_student')}>Share with the students</button>
          {/if}
          <button type="button" class="btn btn-secondary" disabled={busy} onclick={() => ask('cleared')}>Clear the case</button>
          <button
            type="button"
            class="btn btn-danger"
            disabled={busy || !action.open}
            aria-describedby={action.open ? undefined : 'action-help'}
            onclick={() => ask('actioned')}>Take action</button
          >
        {/if}
      </div>
      {#if !decided && !action.open}
        <p id="action-help" class="muted small">{actionHelp}</p>
      {/if}
      {#if actionError}
        <div class="refused" role="alert">
          <p class="field-error">{actionError}</p>
          {#if stale}<button type="button" class="btn btn-secondary" onclick={reload}>Reload the case</button>{/if}
        </div>
      {/if}
    </form>
  </section>

  <section class="section" aria-labelledby="log-title">
    <h2 id="log-title">Decision log</h2>
    {#if kase.decisions.length === 0}
      <p class="muted">Nothing recorded yet.</p>
    {:else}
      <ol class="log">
        {#each kase.decisions as d, i (i)}
          <li>
            <p class="log-head">
              <span class="mono when">{formatDateTime(d.created_at)}</span>
              <span><strong>{caseStateWord(d.state)}</strong>, by {byName(d)}</span>
            </p>
            <p class="prose">{d.note}</p>
          </li>
        {/each}
      </ol>
    {/if}
  </section>

  <ConfirmDialog
    open={confirm !== null}
    title={confirm?.title ?? ''}
    body={confirm?.body ?? ''}
    confirmLabel={confirm?.label ?? ''}
    danger={confirm?.danger ?? false}
    {busy}
    onconfirm={() => confirm && change(confirm.state)}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  .dates {
    margin-top: var(--space-2);
  }

  .note {
    margin-top: var(--space-5);
  }

  .small {
    font-size: var(--fs-0);
  }

  .section {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-6);
    min-width: 0;
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .prose {
    max-width: 60ch;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .s-name {
    font-weight: 500;
    white-space: nowrap;
  }

  .s-shared {
    white-space: nowrap;
  }

  .turn {
    display: block;
    font-size: var(--fs-0);
  }

  .s-text {
    padding-block: var(--space-3);
  }

  .versions {
    display: grid;
    gap: var(--space-3);
    padding: 0;
    list-style: none;
  }

  .refused {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-4);
  }

  .decide {
    display: grid;
    gap: var(--space-3);
    max-width: 48rem;
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .log {
    display: grid;
    max-width: 56rem;
    padding: 0;
    border: var(--rule) solid var(--line);
    background: var(--surface);
    list-style: none;
  }

  .log li {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--rule) solid var(--line);
  }

  .log li:last-child {
    border-bottom: 0;
  }

  .log-head {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    font-size: var(--fs-0);
  }

  .log-head strong {
    font-weight: 600;
  }

  .when {
    color: var(--ink-muted);
  }

  /* Phones: name and share state on one line, the explanation below. */
  @media (width < 768px) {
    .students tbody tr {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--space-1);
    }

    .s-shared {
      color: var(--ink-muted);
      white-space: normal;
    }
  }
</style>
