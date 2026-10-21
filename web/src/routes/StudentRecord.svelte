<!--
  StudentRecord: one student's record for owners and admins at
  /admin/students/{id}/record (v2 P4 F9.2b, F9.3; GET
  /api/admin/students/{id}/record, contract C12, decision D97). The shell
  loads it lazily, and every visit is logged for the owner, which the
  header says.

  Beside the work from 1280 px, first on smaller screens, the rating
  panel: the number out of 10 and its band, then any change waiting for
  a second organiser ("Waiting for a second organiser", with Confirm or
  Decline for those who may), then "Edit rating": a new value from 0 to
  10 and a reason the student reads, required (an empty or short reason
  is refused before anything is sent). The confirmation names the old and
  new value and band, and says when a falling band waits for a second
  organiser; a 202 answer shows the waiting state.

  The work column, top to bottom: the rating history, newest first, each
  line with its cause, change, new value, who made and who confirmed it
  and the reason; the line that crossed a band carries a badge saying
  which way ("Dropped from Good standing to Outcomes on record"). Then
  the standing as the student sees it (band and dated lines), the cases
  the student is in (each opens its case page), the private notes still
  kept, and the appeals, where an appeal against a manual adjustment is
  ruled on here (an appeal against an outcome is ruled on its case page).
  Nobody opens their own record (403); an erased account is missing.
  Props: `id` (the account).
-->
<script lang="ts">
  import Badge from '../components/Badge.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import AppealRuling from '../components/students/AppealRuling.svelte';
  import { onMount, tick } from 'svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { keepHeadingFocus } from '../lib/focus';
  import { formatDateTime } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { caseStateWord, MIN_REASON, noteCounter, noteLength, outcomePhrase, outcomeWord, type CaseAppeal } from '../lib/review';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { appealState, bandName, bandTone } from '../lib/standing';
  import {
    bandOf,
    bandWord,
    causeWord,
    crossingText,
    deltaText,
    linesText,
    parseRating,
    ratingConfirm,
    ratingEditProblems,
    ratingText,
    standingLineWord,
    type RatingAnswer,
    type StudentRecord,
  } from '../lib/students';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let forbiddenText = $state('Only owners and admins can see a record.');
  let rec = $state.raw<StudentRecord | null>(null);

  // Edit rating.
  let valueText = $state('');
  let reason = $state('');
  let errors = $state({ value: '', reason: '' });
  let editError = $state('');
  let editBusy = $state(false);
  let confirm = $state<null | ReturnType<typeof ratingConfirm> & { to: number }>(null);

  // A pending change.
  let pendingBusy = $state(false);
  let pendingError = $state('');
  let soleReason = $state('');
  let soleOpen = $state(false);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const isOwner = $derived(session.user?.role === 'owner');
  const api = $derived(`/api/admin/students/${encodeURIComponent(id)}`);
  const name = $derived(rec ? rec.student.name || rec.student.email : 'Record');
  const band = $derived(rec ? bandOf(rec.rating.value_tenths) : 'good');

  function handle(e: unknown): boolean {
    if (!(e instanceof ApiError)) return false;
    if (e.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const r = await getJSON<StudentRecord>(`${api}/record`);
      const restore = keepHeadingFocus();
      rec = r;
      setTitle(r.student.name || r.student.email, routeTitle('student-record'));
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (handle(e)) return;
      if (e instanceof ApiError && e.status === 403) {
        forbiddenText = e.message || forbiddenText;
        loadState = 'forbidden';
        return;
      }
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Record not found');
        return;
      }
      loadError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      loadState = 'error';
    }
  }

  function adopt(out: RatingAnswer) {
    if (!rec) return;
    rec = { ...rec, rating: out.rating, pending_rating: out.pending_rating };
  }

  async function askEdit(event: SubmitEvent) {
    event.preventDefault();
    editError = '';
    if (!rec) return;
    errors = ratingEditProblems(valueText, reason, rec.rating.value_tenths);
    if (errors.value || errors.reason) {
      await tick();
      document.querySelector<HTMLElement>('#rating-form [aria-invalid="true"]')?.focus();
      return;
    }
    const parsed = parseRating(valueText);
    if (!('tenths' in parsed)) return;
    confirm = { ...ratingConfirm(name, rec.rating.value_tenths, parsed.tenths), to: parsed.tenths };
  }

  async function sendEdit() {
    if (!confirm) return;
    editBusy = true;
    const to = confirm.to;
    try {
      const out = await sendJSON<RatingAnswer>('PUT', `${api}/rating`, { value_tenths: to, reason: reason.trim() });
      adopt(out);
      valueText = '';
      reason = '';
      showToast(out.pending_rating ? 'Proposed the change. A second organiser confirms it.' : `Changed the rating to ${ratingText(to)}`);
      // Close the dialog first, so the browser's return of focus to the opener comes before ours.
      confirm = null;
      await tick();
      document.getElementById(out.pending_rating ? 'pending-title' : 'rating-title')?.focus();
    } catch (e) {
      if (handle(e)) return;
      if (e instanceof ApiError && (e.fields.reason || e.fields.value_tenths)) {
        errors = { value: e.fields.value_tenths ?? '', reason: e.fields.reason ?? '' };
      } else {
        editError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      }
    } finally {
      editBusy = false;
      confirm = null;
    }
  }

  async function decide(kind: 'confirm' | 'decline', sole = false) {
    if (!rec?.pending_rating) return;
    pendingError = '';
    if (sole && noteLength(soleReason) < MIN_REASON) {
      pendingError = `Write at least ${MIN_REASON} characters on why you confirm alone.`;
      document.getElementById('sole-reason')?.focus();
      return;
    }
    pendingBusy = true;
    try {
      const out = await sendJSON<RatingAnswer>(
        'POST',
        `${api}/rating/proposals/${rec.pending_rating.id}/${kind}`,
        kind === 'confirm' && sole ? { sole_organiser_reason: soleReason.trim() } : kind === 'confirm' ? {} : undefined,
      );
      adopt(out);
      soleOpen = false;
      soleReason = '';
      showToast(kind === 'confirm' ? `Confirmed: the rating is now ${ratingText(out.rating.value_tenths)}` : 'Declined the change');
      await tick();
      document.getElementById('rating-title')?.focus();
    } catch (e) {
      if (handle(e)) return;
      pendingError = e instanceof ApiError ? e.message : 'Something went wrong. Try again in a moment.';
      if (e instanceof ApiError && e.code === 'rating_changed') void load();
    } finally {
      pendingBusy = false;
    }
  }

  function ruled(a: CaseAppeal) {
    if (!rec) return;
    rec = { ...rec, appeals: rec.appeals.map((x) => (x.id === a.id ? a : x)) };
    // An upheld adjustment appeal moves the rating: read it again.
    if (a.state === 'upheld') void load();
  }

  function appealWhat(a: CaseAppeal): string {
    if (a.adjustment_id !== null) return 'a manual adjustment of the standing';
    const c = rec?.cases.find((k) => k.id === a.case_id);
    return c?.outcome ? `${outcomePhrase(c.outcome)} in ${c.contest_title}` : 'an outcome';
  }

  onMount(() => {
    setTitle(routeTitle('student-record'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
  });
</script>

{#if loadState === 'forbidden'}
  <PageHeader title="Record" crumbs={[{ label: 'Students', href: '/admin/students' }, { label: 'Record' }]} />
  <PageState kind="forbidden" message={forbiddenText} />
{:else if loadState !== 'ready' || !rec}
  <PageHeader
    title={loadState === 'missing' ? 'Record not found' : 'Record'}
    crumbs={[{ label: 'Students', href: '/admin/students' }, { label: loadState === 'missing' ? 'Record not found' : 'Record' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the record" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no student with that number, or their account was erased." parent={{ href: '/admin/students', label: 'Go to the students.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <PageHeader
    title="{name}: record"
    crumbs={[
      { label: 'Students', href: '/admin/students' },
      { label: name, href: `/admin/students/${rec.student.id}` },
      { label: 'Record' },
    ]}
    lead="{rec.student.email}{rec.student.disabled ? ', disabled' : ''}. Each visit to this page is logged for the owner."
  />

  <div class="page-split record-split">
    <aside class="page-split-aside" aria-label="Rating">
      <section class="panel" aria-labelledby="rating-title">
        <h2 id="rating-title" tabindex="-1">Rating</h2>
        <p class="figure"><span class="mono value">{ratingText(rec.rating.value_tenths)}</span> <span class="of">of 10</span></p>
        <p class="band-line"><Badge tone={bandTone(band)} text={bandWord(band)} /></p>
        <p class="muted small">For owners and admins. The student sees the band, never the number.</p>

        {#if rec.pending_rating}
          {@const p = rec.pending_rating}
          <div class="pending" role="group" aria-labelledby="pending-title">
            <h3 id="pending-title" tabindex="-1">Waiting for a second organiser</h3>
            <p class="small">
              {p.by.name} proposed <span class="mono">{ratingText(p.from_tenths)}</span> to <span class="mono">{ratingText(p.to_tenths)}</span>
              on <MonoDigits text={formatDateTime(p.created_at)} />: {bandWord(bandOf(p.from_tenths))} to {bandWord(bandOf(p.to_tenths))}.
            </p>
            <p class="text small">{p.reason}</p>
            {#if p.may_confirm}
              <p class="muted small">You did not propose it, so you can confirm it. Nothing changes for the student until then.</p>
              <div class="action-row">
                <button type="button" class="btn btn-primary" disabled={pendingBusy} onclick={() => decide('confirm')}>Confirm the change</button>
                <button type="button" class="btn btn-secondary" disabled={pendingBusy} onclick={() => decide('decline')}>Decline the change</button>
              </div>
            {:else}
              <p class="muted small">A band that drops needs a second owner or admin, who did not propose it, to confirm.</p>
              <div class="action-row">
                <button type="button" class="btn btn-secondary" disabled={pendingBusy} onclick={() => decide('decline')}>Withdraw the change</button>
                {#if isOwner}
                  <button type="button" class="btn btn-quiet" aria-expanded={soleOpen} aria-controls="sole-box" onclick={() => (soleOpen = !soleOpen)}>I am the only organiser</button>
                {/if}
              </div>
              {#if isOwner && soleOpen}
                <div id="sole-box" class="field">
                  <label for="sole-reason">Why you confirm alone</label>
                  <textarea id="sole-reason" class="input" rows="2" bind:value={soleReason}></textarea>
                  <p class="field-help">Allowed only when no other owner or admin exists. The reason is recorded with the change.</p>
                  <button type="button" class="btn btn-secondary" disabled={pendingBusy} onclick={() => decide('confirm', true)}>Confirm alone</button>
                </div>
              {/if}
            {/if}
            {#if pendingError}<p class="field-error" role="alert">{pendingError}</p>{/if}
          </div>
        {/if}
      </section>

      <section class="panel" aria-labelledby="edit-title">
        <h2 id="edit-title">Edit rating</h2>
        <form id="rating-form" class="form" novalidate onsubmit={askEdit}>
          <div class="field value-field">
            <label for="rating-value">New rating, 0 to 10</label>
            <input
              id="rating-value"
              class="input mono"
              inputmode="decimal"
              autocomplete="off"
              bind:value={valueText}
              aria-invalid={errors.value ? 'true' : undefined}
              aria-describedby={errors.value ? 'rating-value-error' : undefined}
            />
            {#if errors.value}<p id="rating-value-error" class="field-error">{errors.value}</p>{/if}
          </div>
          <div class="field">
            <label for="rating-reason">Reason, for the student</label>
            <textarea
              id="rating-reason"
              class="input"
              rows="4"
              required
              bind:value={reason}
              aria-invalid={errors.reason ? 'true' : undefined}
              aria-describedby="rating-reason-help rating-reason-count{errors.reason ? ' rating-reason-error' : ''}"
            ></textarea>
            <p id="rating-reason-help" class="field-help">Required. The student reads it on their standing as "Standing adjusted by an organiser", and can appeal.</p>
            <p id="rating-reason-count" class="field-help">{noteCounter(reason)}</p>
            {#if errors.reason}<p id="rating-reason-error" class="field-error">{errors.reason}</p>{/if}
          </div>
          {#if editError}<p class="field-error" role="alert">{editError}</p>{/if}
          <button type="submit" class="btn btn-secondary" disabled={editBusy}>Change rating</button>
        </form>
      </section>
    </aside>

    <div class="page-split-main">
      <section class="panel" aria-labelledby="history-title">
        <h2 id="history-title">Rating history</h2>
        {#if rec.rating.history.length === 0}
          <p class="muted small">No change yet. Every student starts at 10.</p>
        {:else}
          <ol class="lines">
            {#each rec.rating.history as h (h.id)}
              {@const crossing = crossingText(h)}
              <li class:crossed={crossing !== ''}>
                <p class="line-head small">
                  <span class="when"><MonoDigits text={formatDateTime(h.at)} /></span>
                  <strong>{causeWord(h.cause)}</strong>
                  <span><span class="mono">{deltaText(h.delta_tenths)}</span>, now <span class="mono">{ratingText(h.value_tenths)}</span></span>
                  {#if crossing}<Badge tone={h.band_after === 'good' ? 'ok' : 'part'} text={crossing} />{/if}
                </p>
                {#if h.reason}<p class="text">{h.reason}</p>{/if}
                <p class="muted small">
                  {#if h.actor}By {h.actor.name}{/if}{#if h.confirmer}, confirmed by {h.confirmer.name}{/if}{#if h.semester_label}{h.actor ? ', ' : ''}semester {h.semester_label}{/if}{#if h.case_id !== null}{h.actor || h.semester_label ? '. ' : ''}<a href="/admin/integrity/cases/{h.case_id}">Case {h.case_id}</a>{/if}
                </p>
              </li>
            {/each}
          </ol>
        {/if}
      </section>

      <section class="panel" aria-labelledby="standing-title">
        <div class="panel-head">
          <h2 id="standing-title">Standing, as the student sees it</h2>
          <Badge tone={bandTone(rec.standing.band)} text={bandName(rec.standing)} />
        </div>
        {#if rec.standing.lines.length === 0}
          <p class="muted small">Nothing is on their standing.</p>
        {:else}
          <ol class="lines">
            {#each rec.standing.lines as l (l.id)}
              <li>
                <p class="line-head small">
                  <span class="when"><MonoDigits text={formatDateTime(l.at)} /></span>
                  <strong>{standingLineWord(l)}</strong>
                  <span>{l.text}</span>
                  {#if l.appeal}{@const a = appealState(l.appeal)}<Badge tone={a.tone} text={a.word} />{/if}
                </p>
                <p class="text">{l.reason}</p>
              </li>
            {/each}
          </ol>
        {/if}
      </section>

      <section class="panel" aria-labelledby="cases-title">
        <h2 id="cases-title">Cases</h2>
        {#if rec.cases.length === 0}
          <p class="muted small">No case names this student.</p>
        {:else}
          <ul class="lines">
            {#each rec.cases as c (c.id)}
              <li>
                <p class="line-head small">
                  <a href="/admin/integrity/cases/{c.id}">{c.contest_title}, case {c.id}</a>
                  <Badge tone={c.state === 'cleared' ? 'ok' : c.state === 'actioned' ? 'part' : 'wait'} text={c.outcome ? outcomeWord(c.outcome) : caseStateWord(c.state)} />
                </p>
                <p class="muted small">
                  {c.source === 'manual' ? 'Opened from a submission' : 'Opened from a check'}, <MonoDigits text={formatDateTime(c.created_at)} />{#if c.decided_at !== null}<MonoDigits text={`; decided ${formatDateTime(c.decided_at)}`} />{/if}
                </p>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="panel" aria-labelledby="notes-title">
        <h2 id="notes-title">Private notes</h2>
        <p class="muted small">Organisers only; the student is never told about a note. A note is deleted 30 days after it was written unless a case is opened from its submission.</p>
        {#if rec.notes.length === 0}
          <p class="muted small">No notes kept.</p>
        {:else}
          <ul class="lines">
            {#each rec.notes as n (n.id)}
              <li>
                <p class="line-head small">
                  <span class="when"><MonoDigits text={formatDateTime(n.created_at)} /></span>
                  <span>{n.by_name || 'An organiser'}, on submission <span class="mono">{n.submission_id}</span>{#if n.lines.length > 0}, {linesText(n.lines)}{/if}</span>
                </p>
                <p class="text">{n.text}</p>
                <p class="muted small">{#if n.expires_at === null}Kept with the case.{:else}<MonoDigits text={`Deleted on ${formatDateTime(n.expires_at)}.`} />{/if}</p>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="panel" aria-labelledby="appeals-title">
        <h2 id="appeals-title">Appeals</h2>
        {#if rec.appeals.length === 0}
          <p class="muted small">No appeals.</p>
        {:else}
          <div class="appeals">
            {#each rec.appeals as a (a.id)}
              {#if a.adjustment_id !== null}
                <AppealRuling appeal={a} what={appealWhat(a)} owner={isOwner} onruled={ruled} />
              {:else}
                {@const st = appealState(a)}
                <div class="appeal-line">
                  <p class="line-head small">
                    <span class="when"><MonoDigits text={formatDateTime(a.created_at)} /></span>
                    <span>Against {appealWhat(a)}</span>
                    <Badge tone={st.tone} text={st.word} />
                    {#if a.state === 'open' && a.overdue}<Badge tone="bad" text="Overdue" />{/if}
                  </p>
                  <p class="text">{a.text}</p>
                  {#if a.state === 'open' && a.case_id !== null}<p class="small"><a href="/admin/integrity/cases/{a.case_id}#appeals-title">Rule on it on the case page</a></p>{/if}
                </div>
              {/if}
            {/each}
          </div>
        {/if}
      </section>
    </div>
  </div>

  <ConfirmDialog
    open={confirm !== null}
    title={confirm?.title ?? ''}
    body={confirm?.body ?? ''}
    items={confirm?.items ?? []}
    confirmLabel={confirm?.label ?? ''}
    busy={editBusy}
    onconfirm={sendEdit}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  .record-split {
    margin-top: var(--space-6);
  }

  .panel h2 {
    font-size: var(--fs-sub);
  }

  .small {
    font-size: var(--fs-0);
  }

  .figure {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
  }

  /* Beats the optical step-down of inline mono digits: this is the panel's figure. */
  .figure .value {
    font-size: var(--fs-5);
    line-height: var(--lh-tight);
  }

  .of {
    color: var(--ink-muted);
    font-size: var(--fs-2);
  }

  .pending {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding: var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--paper);
  }

  .pending h3 {
    font-size: var(--fs-1);
    font-weight: 600;
  }

  .form {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
  }

  .form .field {
    width: 100%;
  }

  .value-field input {
    max-width: 8rem;
  }

  .text {
    max-width: 60ch;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .lines {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .lines > li {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
  }

  .lines > li:last-child {
    border-bottom: 0;
  }

  /* The line that crossed a band reads first: heavier words, the badge says which way. */
  .crossed .line-head strong {
    color: var(--ink);
  }

  .line-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-3);
  }

  .line-head strong {
    font-weight: 600;
  }

  /* A long crossing badge wraps on a phone instead of widening the page. */
  .line-head :global(.badge) {
    white-space: normal;
  }

  .when {
    color: var(--ink-muted);
  }

  .appeals {
    display: grid;
  }

  .appeal-line {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-top: var(--rule) solid var(--line);
  }
</style>
