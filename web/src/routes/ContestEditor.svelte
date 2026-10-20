<!--
  ContestEditor: an organiser's settings for one contest at
  /admin/contests/{id} (cache/v1/m4-plan.md sections 1, 2 and 4).

  - Settings: title, slug, description, start and end, fixed start or a
    flexible window with its length, freeze, scoring, feedback (subtask
    results or exam), access (allowed domains or a roster), graded,
    practice after the end, stop on a failed sample, limits, languages, the
    honour code, and Published. "Save changes" sends everything with
    PUT /api/admin/contests/{id}; refused fields show their sentence under
    the field, and focus moves to the first one. While the contest is live
    a notice points to the console, and saving a change to the start, end,
    start mode, scoring, feedback or freeze asks first.
  - Problems: the contest's problems with their letters, reordered with the
    arrow buttons, picked from validated problems in the bank. Saving pins
    each problem's current version; a problem changed since it was added is
    marked. A graded contest refuses a problem the AI wizard made when no
    person has written a solution for it; the section then asks for a
    reason, and "Grade as written" saves again with it (the server records
    the reason in the contest's audit log).
  - Roster: a CSV file or pasted text (email, name, roll number, section)
    with a preview of every parsed line and why any will be refused, and
    how many students the file adds and removes against the current
    roster, then "Replace roster" (the upload replaces the whole roster; it
    asks first when anyone would be removed from a live or published
    contest).
    The current roster loads from GET .../roster when asked for.
  - Co-organisers by email, a clone of the contest with a new slug and
    dates, and archiving.

  Removing a problem from a live or ended contest, and removing a
  co-organiser, ask first. Students see a short explanation instead.
  Props: `id`.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import { onMount, tick, untrack } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ContestAdminNav from '../components/ContestAdminNav.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Icon from '../components/Icon.svelte';
  import { ApiError, getJSON, sendBody, sendJSON } from '../lib/api';
  import {
    fromLocalInput,
    labelFor,
    parseRoster,
    rosterCsv,
    toLocalInput,
    type AdminContest,
    type AdminContestProblem,
    type ContestSettings,
    type RosterRow,
  } from '../lib/contests';
  import { keepHeadingFocus } from '../lib/focus';
  import { setTitle } from '../lib/head';
  import { routeTitle } from '../lib/routing';
  import { LANGUAGES, type ProblemSummary } from '../lib/problems';
  import { navigate } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  let { id }: { id: string } = $props();

  type Form = Omit<ContestSettings, 'starts_at' | 'ends_at' | 'duration_minutes' | 'freeze_minutes' | 'min_gap_s' | 'max_per_problem'> & {
    starts_at: string;
    ends_at: string;
    duration_minutes: string;
    freeze_minutes: string;
    min_gap_s: string;
    max_per_problem: string;
  };

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'missing' | 'error'>('loading');
  let loadError = $state('');
  let contest = $state.raw<AdminContest | null>(null);
  let form = $state<Form | null>(null);
  let fields = $state<Record<string, string>>({});
  let formError = $state('');
  let formProblems = $state<readonly string[]>([]);
  let saveBusy = $state(false);

  let picked = $state<AdminContestProblem[]>([]);
  let bank = $state<ProblemSummary[]>([]);
  let addId = $state('');
  let problemsError = $state('');
  let problemsBusy = $state(false);
  // AI problems the server refused for a graded contest, and the reason to grade them as written.
  let aiLabels = $state<string[]>([]);
  let aiReason = $state('');
  let aiError = $state('');

  let rosterText = $state('');
  let rosterBusy = $state(false);
  let rosterError = $state('');
  let rosterFile: HTMLInputElement | undefined = $state();
  let currentRoster = $state<RosterRow[] | null>(null);
  const rosterCount = $derived(contest ? (contest.roster_count ?? contest.roster?.length ?? 0) : 0);

  let adminEmail = $state('');
  let adminError = $state('');

  let cloneSlug = $state('');
  let cloneStart = $state('');
  let cloneEnd = $state('');
  let cloneError = $state('');

  // One confirmation dialog for every action that asks first.
  let confirm = $state<null | { title: string; body: string; label: string; danger: boolean; run: () => Promise<void> }>(null);
  let confirmBusy = $state(false);

  const isOrganiser = $derived(session.user?.role === 'admin' || session.user?.role === 'owner');
  const base = $derived(`/api/admin/contests/${encodeURIComponent(id)}`);
  const roster = $derived(rosterText.trim() === '' ? null : parseRoster(rosterText));
  const live = $derived(contest?.state === 'live' || contest?.state === 'frozen');
  const startedOrOver = $derived(live || contest?.state === 'ended' || contest?.state === 'practice');
  // What the file would change: students added and removed against the current roster.
  const rosterDiff = $derived.by(() => {
    if (!roster || currentRoster === null) return null;
    const now = new Set(currentRoster.map((r) => r.email.toLowerCase()));
    const next = new Set(roster.lines.filter((l) => l.error === '').map((l) => l.email));
    return {
      before: now.size,
      after: next.size,
      added: [...next].filter((e) => !now.has(e)).length,
      removed: [...now].filter((e) => !next.has(e)).length,
    };
  });

  // The current roster is fetched once a file is previewed, to show what it changes.
  $effect(() => {
    if (roster && currentRoster === null && contest) untrack(() => void fetchRoster());
  });
  const available = $derived(bank.filter((p) => p.validated && !p.archived && !picked.some((x) => x.problem_id === p.id)));

  function toForm(c: AdminContest): Form {
    return {
      slug: c.slug,
      title: c.title,
      description_md: c.description_md ?? '',
      starts_at: toLocalInput(c.starts_at),
      ends_at: toLocalInput(c.ends_at),
      start_mode: c.start_mode,
      duration_minutes: String(c.duration_minutes || 120),
      freeze_minutes: String(c.freeze_minutes ?? 0),
      scoring_mode: c.scoring_mode,
      access: c.access,
      feedback_mode: c.feedback_mode,
      graded: c.graded,
      published: c.published,
      practice_after_end: c.practice_after_end,
      stop_on_sample_fail: c.stop_on_sample_fail,
      min_gap_s: String(c.min_gap_s),
      max_per_problem: String(c.max_per_problem),
      languages: [...(c.languages ?? [])],
      honour_text: c.honour_text ?? '',
      hold_reveal: c.hold_reveal ?? false,
    };
  }

  function adopt(c: AdminContest) {
    contest = c;
    form = toForm(c);
    picked = (c.problems ?? []).map((p) => ({ ...p }));
    setTitle(c.title, routeTitle('admin-contest'));
    if (cloneSlug === '') {
      cloneSlug = `${c.slug}-copy`;
      cloneStart = toLocalInput(c.starts_at + 7 * 86_400_000);
      cloneEnd = toLocalInput(c.ends_at + 7 * 86_400_000);
    }
  }

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

  async function load() {
    loadState = 'loading';
    try {
      const data = await getJSON<AdminContest>(base);
      const restore = keepHeadingFocus();
      adopt(data);
      loadState = 'ready';
      void restore();
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.status === 404) {
        loadState = 'missing';
        setTitle('Contest not found');
        return;
      }
      loadError = messageOf(e);
      loadState = 'error';
    }
    try {
      const data = await getJSON<{ problems: ProblemSummary[] }>('/api/admin/problems');
      bank = data.problems ?? [];
    } catch {
      bank = [];
    }
  }

  /** Takes a response that is the contest, or reloads it. */
  async function settle(value: unknown) {
    if (value && typeof value === 'object' && 'id' in value && 'problems' in value) adopt(value as AdminContest);
    else {
      try {
        adopt(await getJSON<AdminContest>(base));
      } catch (e) {
        handleAuth(e);
      }
    }
  }

  function num(v: string): number {
    const n = Number(v);
    return Number.isFinite(n) ? Math.round(n) : NaN;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!form || !contest) return;
    fields = {};
    formError = '';
    formProblems = [];
    const starts = fromLocalInput(form.starts_at);
    const ends = fromLocalInput(form.ends_at);
    const local: Record<string, string> = {};
    if (form.title.trim() === '') local.title = 'Enter a title.';
    if (starts === null) local.starts_at = 'Enter the start date and time.';
    if (ends === null) local.ends_at = 'Enter the end date and time.';
    else if (starts !== null && ends <= starts) local.ends_at = 'The contest must end after it starts.';
    if (form.languages.length === 0) local.languages = 'Allow at least one language.';
    for (const k of ['freeze_minutes', 'min_gap_s', 'max_per_problem', 'duration_minutes'] as const) {
      if (Number.isNaN(num(form[k]))) local[k] = 'Enter a whole number.';
    }
    if (Object.keys(local).length > 0) {
      fields = local;
      await tick();
      const first = document.querySelector<HTMLElement>('.settings [aria-invalid="true"]');
      if (first) first.focus();
      else if (local.languages) document.querySelector<HTMLElement>('.settings .checks input')?.focus();
      return;
    }
    const body: ContestSettings = {
      ...form,
      title: form.title.trim(),
      slug: form.slug.trim(),
      starts_at: starts as number,
      ends_at: ends as number,
      duration_minutes: num(form.duration_minutes),
      freeze_minutes: num(form.freeze_minutes),
      min_gap_s: num(form.min_gap_s),
      max_per_problem: num(form.max_per_problem),
    };
    const c = contest;
    // The form works in whole minutes, so times are compared to the minute.
    const minute = (ms: number) => Math.floor(ms / 60_000);
    // shortcut: published joins this confirm until the v2 console item C5 gives it its own.
    const liveChange =
      body.published !== c.published ||
      minute(body.starts_at) !== minute(c.starts_at) ||
      minute(body.ends_at) !== minute(c.ends_at) ||
      body.start_mode !== c.start_mode ||
      (body.start_mode === 'window' && body.duration_minutes !== c.duration_minutes) ||
      body.scoring_mode !== c.scoring_mode ||
      body.feedback_mode !== c.feedback_mode ||
      body.freeze_minutes !== c.freeze_minutes;
    if (live && liveChange) {
      confirm = {
        title: 'Change a live contest?',
        body: 'Students see the change at once: new times, scoring, feedback or freeze can change their clocks and scores, and unpublishing hides the contest from them. To give more time, Extend contest in the console is safer.',
        label: 'Save changes',
        danger: true,
        run: () => putSettings(body),
      };
      return;
    }
    await putSettings(body);
  }

  async function putSettings(body: ContestSettings) {
    if (!contest) return;
    const publishing = body.published && !contest.published;
    saveBusy = true;
    try {
      await settle(await sendJSON<AdminContest>('PUT', base, body));
      showToast(publishing ? 'Published' : 'Saved changes');
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError) {
        fields = { ...e.fields };
        formProblems = e.problems;
        const known = Object.keys(e.fields).filter((k) => document.getElementById(`f-${k}`));
        if (known.length < Object.keys(e.fields).length || known.length === 0) formError = e.message;
      } else {
        formError = messageOf(e);
      }
      await tick();
      document.querySelector<HTMLElement>('.settings [aria-invalid="true"]')?.focus();
    } finally {
      saveBusy = false;
    }
  }

  function toggleLanguage(lang: string, on: boolean) {
    if (!form) return;
    form.languages = on ? [...form.languages, lang].filter((v, i, a) => a.indexOf(v) === i) : form.languages.filter((l) => l !== lang);
  }

  // Problems.
  function addProblem() {
    const p = bank.find((x) => String(x.id) === addId);
    if (!p) return;
    const used = new Set(picked.map((x) => x.label));
    let i = 0;
    while (used.has(labelFor(i))) i++;
    picked = [...picked, { problem_id: p.id, title: p.title || p.slug, label: labelFor(i), ready: true, max_score: p.points }];
    addId = '';
  }

  function move(i: number, by: number) {
    const j = i + by;
    if (j < 0 || j >= picked.length) return;
    const next = picked.slice();
    [next[i], next[j]] = [next[j], next[i]];
    // Letters follow the order, so A is always the first problem.
    picked = next.map((p, k) => ({ ...p, label: labelFor(k) }));
    // At the top or bottom the same arrow is disabled, so focus takes the other one.
    tick().then(() => {
      const same = document.getElementById(`move-${by < 0 ? 'up' : 'down'}-${j}`) as HTMLButtonElement | null;
      const other = document.getElementById(`move-${by < 0 ? 'down' : 'up'}-${j}`) as HTMLButtonElement | null;
      (same && !same.disabled ? same : other)?.focus();
    });
  }

  function removeProblem(i: number) {
    picked = picked.filter((_, k) => k !== i).map((p, k) => ({ ...p, label: labelFor(k) }));
    // The removed row took the focused button with it: focus the row now in
    // its place, or the problem picker when the list is empty.
    tick().then(() => {
      const row = document.getElementById(`remove-${Math.min(i, picked.length - 1)}`);
      (row ?? document.getElementById('add-problem'))?.focus();
    });
  }

  async function saveProblems() {
    const gone = (contest?.problems ?? []).filter((p) => !picked.some((x) => x.problem_id === p.problem_id));
    if (startedOrOver && gone.length > 0) {
      const names = gone.map((p) => `problem ${p.label} (${p.title})`).join(', ');
      confirm = {
        title: 'Save problems?',
        body: `This takes ${names} out of the contest, and ${gone.length === 1 ? 'its' : 'their'} submissions leave the board.`,
        label: 'Save problems',
        danger: true,
        run: () => putProblems(),
      };
      return;
    }
    await putProblems();
  }

  /** Saves the problems again with the reason to grade the refused AI problems as written. */
  async function gradeAsWritten(event: SubmitEvent) {
    event.preventDefault();
    aiError = '';
    if (aiReason.trim().length < 10) {
      aiError = 'Write a reason of at least 10 characters.';
      document.getElementById('ai-reason')?.focus();
      return;
    }
    await putProblems(aiReason.trim());
  }

  async function putProblems(reason = '') {
    problemsError = '';
    const labels = picked.map((p) => p.label.trim().toUpperCase());
    if (labels.some((l) => !/^[A-Z]{1,3}$/.test(l))) {
      problemsError = 'Each letter must be one to three capital letters.';
      return;
    }
    if (new Set(labels).size !== labels.length) {
      problemsError = 'Each problem needs its own letter.';
      return;
    }
    problemsBusy = true;
    try {
      const value = await sendJSON<unknown>(
        'PUT',
        `${base}/problems`,
        picked.map((p, i) =>
          reason !== '' && aiLabels.includes(labels[i])
            ? { problem_id: p.problem_id, label: labels[i], ai_reason: reason }
            : { problem_id: p.problem_id, label: labels[i] },
        ),
      );
      await settle(value);
      showToast(reason !== '' ? 'Saved problems with your reason' : 'Saved problems');
      aiLabels = [];
      aiReason = '';
    } catch (e) {
      if (handleAuth(e)) return;
      if (e instanceof ApiError && e.code === 'ai_unconfirmed') {
        aiLabels = [...e.problems];
        aiError = reason !== '' ? e.message : '';
        problemsError = reason !== '' ? '' : e.message;
        await tick();
        document.getElementById('ai-reason')?.focus();
        return;
      }
      problemsError = e instanceof ApiError && e.problems.length > 0 ? `${e.message} ${e.problems.join(' ')}` : messageOf(e);
    } finally {
      problemsBusy = false;
    }
  }

  // Roster.
  async function readRosterFile() {
    const file = rosterFile?.files?.[0];
    if (!file) return;
    rosterText = await file.text();
    if (rosterFile) rosterFile.value = '';
  }

  async function fetchRoster() {
    try {
      const data = await getJSON<{ roster: RosterRow[] }>(`${base}/roster`);
      currentRoster = data.roster ?? [];
    } catch (e) {
      if (!handleAuth(e)) currentRoster = contest?.roster ?? [];
    }
  }

  async function uploadRoster() {
    if (!roster) return;
    rosterError = '';
    if (roster.valid === 0) {
      rosterError = 'No line has a valid email. Check the file and try again.';
      return;
    }
    if (currentRoster === null) await fetchRoster();
    const d = rosterDiff;
    if (d && d.removed > 0 && (live || contest?.published)) {
      confirm = {
        title: 'Replace the roster?',
        body: `${d.before} ${d.before === 1 ? 'student' : 'students'} now, ${d.after} after. The ${d.removed} not in the file lose access${live ? ' and are taken out of the contest at once' : ''}.`,
        label: 'Replace roster',
        danger: true,
        run: putRoster,
      };
      return;
    }
    await putRoster();
  }

  async function putRoster() {
    if (!roster) return;
    rosterBusy = true;
    try {
      const csv = rosterCsv(roster.lines.filter((l) => l.error === ''));
      const value = await sendBody<unknown>('PUT', `${base}/roster`, new Blob([csv], { type: 'text/csv' }), 'text/csv');
      // PUT .../roster answers with the roster itself, so the contest is fetched again.
      await settle(value);
      showToast(`Replaced the roster: ${roster.valid} ${roster.valid === 1 ? 'student' : 'students'}`);
      rosterText = '';
      currentRoster = null;
    } catch (e) {
      if (handleAuth(e)) return;
      rosterError = e instanceof ApiError && e.problems.length > 0 ? `${e.message} ${e.problems.join(' ')}` : messageOf(e);
    } finally {
      rosterBusy = false;
    }
  }

  async function loadRoster(event: Event) {
    if (!(event.currentTarget as HTMLDetailsElement).open || currentRoster !== null) return;
    await fetchRoster();
  }

  // Co-organisers.
  async function addAdmin(event: SubmitEvent) {
    event.preventDefault();
    adminError = '';
    const email = adminEmail.trim();
    if (email === '') {
      adminError = 'Enter an email address.';
      document.getElementById('admin-email')?.focus();
      return;
    }
    try {
      await settle(await sendJSON<unknown>('POST', `${base}/admins`, { email }));
      showToast(`Added ${email}`);
      adminEmail = '';
    } catch (e) {
      if (handleAuth(e)) return;
      adminError = e instanceof ApiError ? (e.fields.email ?? e.message) : messageOf(e);
      document.getElementById('admin-email')?.focus();
    }
  }

  function removeAdmin(email: string, who: string) {
    confirm = {
      title: `Remove ${who}?`,
      body: 'They can no longer change this contest or run its console.',
      label: `Remove ${who}`,
      danger: true,
      run: async () => {
        await settle(await sendJSON<unknown>('DELETE', `${base}/admins`, { email }));
        showToast(`Removed ${who}`);
      },
    };
  }

  async function runConfirm() {
    if (!confirm) return;
    confirmBusy = true;
    try {
      await confirm.run();
      confirm = null;
    } catch (e) {
      confirm = null;
      if (!handleAuth(e)) showToast(messageOf(e), 'error');
    } finally {
      confirmBusy = false;
    }
  }

  // Clone and archive.
  async function cloneContest(event: SubmitEvent) {
    event.preventDefault();
    cloneError = '';
    const starts = fromLocalInput(cloneStart);
    const ends = fromLocalInput(cloneEnd);
    if (cloneSlug.trim() === '' || starts === null || ends === null || ends <= starts) {
      cloneError = 'Enter a new slug, and a start before the end.';
      return;
    }
    try {
      const made = await sendJSON<AdminContest>('POST', `${base}/clone`, { slug: cloneSlug.trim(), starts_at: starts, ends_at: ends });
      showToast(`Cloned as ${made.slug}`);
      navigate(`/admin/contests/${made.id}`);
    } catch (e) {
      if (handleAuth(e)) return;
      cloneError = e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : messageOf(e);
    }
  }

  function archive() {
    if (!contest) return;
    confirm = {
      title: `Archive ${contest.title}?`,
      body: 'Students will no longer see it. Results and submissions are kept.',
      label: 'Archive contest',
      danger: true,
      run: async () => {
        await sendJSON<void>('DELETE', base);
        showToast('Archived');
        navigate('/admin/contests');
      },
    };
  }

  onMount(() => {
    setTitle(routeTitle('admin-contest'));
    if (!isOrganiser) loadState = 'forbidden';
    else void load();
  });
</script>

{#snippet err(key: string)}
  {#if fields[key]}<p id="f-{key}-error" class="field-error">{fields[key]}</p>{/if}
{/snippet}

{#if loadState === 'forbidden'}
  <PageHeader title="Contest settings" />
  <PageState kind="forbidden" message="Only admins can manage contests." />
{:else if loadState !== 'ready' || !contest || !form}
  <PageHeader
    title={loadState === 'missing' ? 'Contest not found' : 'Contest settings'}
    crumbs={[{ label: 'Contest admin', href: '/admin/contests' }, { label: loadState === 'missing' ? 'Contest not found' : 'Contest settings' }]}
  />
  {#if loadState === 'loading'}
    <PageState kind="loading" message="Loading the contest" />
  {:else if loadState === 'missing'}
    <PageState kind="missing" message="There is no contest with that number." parent={{ href: '/admin/contests', label: 'Go back to the list.' }} />
  {:else}
    <PageState kind="error" message={loadError} onretry={load} />
  {/if}
{:else}
  <ContestAdminNav {id} title={contest.title} state={contest.state} current="settings" />

  <form class="settings section" novalidate onsubmit={save} aria-labelledby="settings-title">
    <h2 id="settings-title">Settings</h2>
    {#if live}
      <div class="banner banner-info" role="note">
        <p>
          This contest is live. Changes here reach students at once. To give more time, use Extend contest in the
          <a href="/admin/contests/{encodeURIComponent(id)}/console">console</a>.
        </p>
      </div>
    {/if}

    <div class="grid">
      <div class="field">
        <label for="f-title">Title</label>
        <input id="f-title" class="input" type="text" bind:value={form.title} aria-invalid={fields.title ? 'true' : undefined} aria-describedby={fields.title ? 'f-title-error' : undefined} />
        {@render err('title')}
      </div>
      <div class="field">
        <label for="f-slug">Slug</label>
        <input id="f-slug" class="input mono" type="text" spellcheck="false" bind:value={form.slug} aria-invalid={fields.slug ? 'true' : undefined} aria-describedby={fields.slug ? 'f-slug-error' : undefined} />
        {@render err('slug')}
      </div>
      <div class="field">
        <label for="f-starts_at">Starts</label>
        <input id="f-starts_at" class="input" type="datetime-local" bind:value={form.starts_at} aria-invalid={fields.starts_at ? 'true' : undefined} aria-describedby={fields.starts_at ? 'f-starts_at-error' : undefined} />
        {@render err('starts_at')}
      </div>
      <div class="field">
        <label for="f-ends_at">Ends</label>
        <input id="f-ends_at" class="input" type="datetime-local" bind:value={form.ends_at} aria-invalid={fields.ends_at ? 'true' : undefined} aria-describedby={fields.ends_at ? 'f-ends_at-error' : undefined} />
        {@render err('ends_at')}
      </div>
    </div>

    <fieldset class="group">
      <legend>Start</legend>
      <label class="choice"><input type="radio" name="start_mode" value="fixed" bind:group={form.start_mode} /> Everyone starts together</label>
      <label class="choice"><input type="radio" name="start_mode" value="window" bind:group={form.start_mode} /> Each student starts any time in the window</label>
      {#if form.start_mode === 'window'}
        <div class="field narrow">
          <label for="f-duration_minutes">Minutes per student</label>
          <input id="f-duration_minutes" class="input mono" type="number" min="10" inputmode="numeric" bind:value={form.duration_minutes} aria-invalid={fields.duration_minutes ? 'true' : undefined} aria-describedby="f-duration-help{fields.duration_minutes ? ' f-duration_minutes-error' : ''}" />
          <p id="f-duration-help" class="field-help">Students see the board only once their own window closes.</p>
          {@render err('duration_minutes')}
        </div>
      {/if}
      {@render err('start_mode')}
    </fieldset>

    <div class="grid">
      <div class="field">
        <label for="f-freeze_minutes">Freeze the board, minutes before the end</label>
        <input id="f-freeze_minutes" class="input mono" type="number" min="0" inputmode="numeric" bind:value={form.freeze_minutes} aria-invalid={fields.freeze_minutes ? 'true' : undefined} aria-describedby="f-freeze-help{fields.freeze_minutes ? ' f-freeze_minutes-error' : ''}" />
        <p id="f-freeze-help" class="field-help">0 for no freeze. Applies to fixed-start contests.</p>
        {@render err('freeze_minutes')}
      </div>
      <div class="field">
        <label for="f-scoring_mode">Scoring</label>
        <select id="f-scoring_mode" class="select" bind:value={form.scoring_mode} aria-invalid={fields.scoring_mode ? 'true' : undefined} aria-describedby={fields.scoring_mode ? 'f-scoring_mode-error' : undefined}>
          <option value="best_subtask">Best result per subtask</option>
          <option value="best_submission">Best single submission</option>
        </select>
        {@render err('scoring_mode')}
      </div>
      <div class="field">
        <label for="f-feedback_mode">Feedback</label>
        <select id="f-feedback_mode" class="select" bind:value={form.feedback_mode} aria-invalid={fields.feedback_mode ? 'true' : undefined} aria-describedby="f-feedback-help{fields.feedback_mode ? ' f-feedback_mode-error' : ''}">
          <option value="subtask">Subtask results and test verdicts</option>
          <option value="exam">Exam: only "Submitted" until the end</option>
        </select>
        <p id="f-feedback-help" class="field-help">Hidden test inputs and outputs are never shown.</p>
        {@render err('feedback_mode')}
      </div>
      <div class="field">
        <label for="f-access">Who can take part</label>
        <select id="f-access" class="select" bind:value={form.access} aria-invalid={fields.access ? 'true' : undefined} aria-describedby={fields.access ? 'f-access-error' : undefined}>
          <option value="domain">Anyone from the allowed domains</option>
          <option value="roster">Only students on the roster</option>
        </select>
        {@render err('access')}
      </div>
      <div class="field">
        <label for="f-min_gap_s">Seconds between submissions</label>
        <input id="f-min_gap_s" class="input mono" type="number" min="0" inputmode="numeric" bind:value={form.min_gap_s} aria-invalid={fields.min_gap_s ? 'true' : undefined} aria-describedby={fields.min_gap_s ? 'f-min_gap_s-error' : undefined} />
        {@render err('min_gap_s')}
      </div>
      <div class="field">
        <label for="f-max_per_problem">Submissions per problem</label>
        <input id="f-max_per_problem" class="input mono" type="number" min="1" inputmode="numeric" bind:value={form.max_per_problem} aria-invalid={fields.max_per_problem ? 'true' : undefined} aria-describedby={fields.max_per_problem ? 'f-max_per_problem-error' : undefined} />
        {@render err('max_per_problem')}
      </div>
    </div>

    <fieldset class="group" aria-describedby={fields.languages ? 'f-languages-error' : undefined}>
      <legend>Languages</legend>
      <div class="checks">
        {#each LANGUAGES as l (l.id)}
          <label class="choice">
            <input type="checkbox" checked={form.languages.includes(l.id)} onchange={(e) => toggleLanguage(l.id, e.currentTarget.checked)} />
            {l.label}
          </label>
        {/each}
      </div>
      {@render err('languages')}
    </fieldset>

    <fieldset class="group">
      <legend>Options</legend>
      <label class="choice"><input type="checkbox" bind:checked={form.graded} /> Graded: times shown to 0.1 s, honour code recorded</label>
      <label class="choice"><input type="checkbox" bind:checked={form.practice_after_end} /> Open the problems for practice after the end</label>
      <label class="choice"><input type="checkbox" bind:checked={form.stop_on_sample_fail} /> Stop judging at a failed sample</label>
      <label class="choice"><input type="checkbox" bind:checked={form.hold_reveal} /> Hold the full board after the end until you reveal it</label>
    </fieldset>

    <div class="field">
      <label for="f-description_md">Description (Markdown)</label>
      <textarea id="f-description_md" class="input" rows="4" bind:value={form.description_md} aria-invalid={fields.description_md ? 'true' : undefined} aria-describedby={fields.description_md ? 'f-description_md-error' : undefined}></textarea>
      {@render err('description_md')}
    </div>
    <div class="field">
      <label for="f-honour_text">Honour code (Markdown)</label>
      <textarea
        id="f-honour_text"
        class="input"
        rows="6"
        placeholder={contest.default_honour_text ?? ''}
        bind:value={form.honour_text}
        aria-describedby="f-honour-help{fields.honour_text ? ' f-honour_text-error' : ''}"
      ></textarea>
      <p id="f-honour-help" class="field-help">Leave empty for the default, which lists what is recorded and for how long.</p>
      {@render err('honour_text')}
    </div>

    <fieldset class="group">
      <legend>Visibility</legend>
      <label class="choice">
        <input id="f-published" type="checkbox" bind:checked={form.published} aria-describedby="f-published-help{fields.published ? ' f-published-error' : ''}" />
        Published: students can see the contest
      </label>
      <p id="f-published-help" class="field-help">Publishing checks that every problem is still the version that was validated.</p>
      {@render err('published')}
    </fieldset>

    {#if formError}
      <div class="banner banner-error" role="alert">
        <p>{formError}</p>
        {#if formProblems.length > 0}<ul>{#each formProblems as p, i (i)}<li>{p}</li>{/each}</ul>{/if}
      </div>
    {/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={saveBusy}>Save changes</button>
    </div>
  </form>

  <section class="section" aria-labelledby="problems-title">
    <h2 id="problems-title">Problems</h2>
    <p class="muted small">Only validated problems can be added. Saving pins the version each problem has now.</p>
    {#if picked.length === 0}
      <p class="muted">No problems yet. Add validated problems from the bank.</p>
    {:else}
      <ol class="picked">
        {#each picked as p, i (p.problem_id)}
          <li class="pick">
            <label class="visually-hidden" for="label-{i}">Letter for {p.title}</label>
            <input id="label-{i}" class="input mono letter" type="text" maxlength="3" bind:value={p.label} />
            <span class="pick-title">
              {p.title}
              {#if p.ready === false}<span class="warn"><Icon name="alert-triangle" /> Changed since it was added. Validate it, then save problems again.</span>{/if}
            </span>
            <span class="buttons">
              <button id="move-up-{i}" type="button" class="btn btn-quiet btn-compact" aria-label="Move {p.title} up" title="Move up" disabled={i === 0} onclick={() => move(i, -1)}><Icon name="chevron-up" /></button>
              <button id="move-down-{i}" type="button" class="btn btn-quiet btn-compact" aria-label="Move {p.title} down" title="Move down" disabled={i === picked.length - 1} onclick={() => move(i, 1)}><Icon name="chevron-down" /></button>
              <button id="remove-{i}" type="button" class="btn btn-danger-quiet btn-compact" aria-label="Remove {p.title}" title="Remove" onclick={() => removeProblem(i)}><Icon name="trash" /></button>
            </span>
          </li>
        {/each}
      </ol>
    {/if}
    <div class="row">
      <div class="field grow">
        <label for="add-problem">Add a validated problem</label>
        <select id="add-problem" class="select" bind:value={addId}>
          <option value="">Choose a problem</option>
          {#each available as p (p.id)}<option value={String(p.id)}>{p.title || p.slug} ({p.points} points)</option>{/each}
        </select>
      </div>
      <button type="button" class="btn btn-secondary" disabled={addId === ''} onclick={addProblem}>Add problem</button>
    </div>
    {#if problemsError}<p class="field-error" role="alert">{problemsError}</p>{/if}
    <div class="actions">
      <button type="button" class="btn btn-primary" disabled={problemsBusy} onclick={saveProblems}>Save problems</button>
    </div>
    {#if aiLabels.length > 0}
      <form class="ai-confirm" novalidate onsubmit={gradeAsWritten}>
        <div class="field">
          <label for="ai-reason">Reason to grade {aiLabels.length === 1 ? `problem ${aiLabels[0]}` : `problems ${aiLabels.join(', ')}`} as the AI wrote it</label>
          <textarea
            id="ai-reason"
            class="input"
            rows="2"
            bind:value={aiReason}
            aria-invalid={aiError ? 'true' : undefined}
            aria-describedby="ai-reason-help{aiError ? ' ai-reason-error' : ''}"
          ></textarea>
          <p id="ai-reason-help" class="field-help">
            Better: write your own solution in the problem editor and validate it. The reason is kept in this contest's audit log.
          </p>
          {#if aiError}<p id="ai-reason-error" class="field-error">{aiError}</p>{/if}
        </div>
        <div class="row">
          <button type="submit" class="btn btn-danger" disabled={problemsBusy}>Grade as written</button>
          <button type="button" class="btn btn-secondary" onclick={() => { aiLabels = []; aiReason = ''; aiError = ''; problemsError = ''; }}>Cancel</button>
        </div>
      </form>
    {/if}
  </section>

  <section class="section" aria-labelledby="roster-title">
    <h2 id="roster-title">Roster</h2>
    <p class="muted small">
      {rosterCount} {rosterCount === 1 ? 'student' : 'students'} on the roster.
      {form.access === 'roster' ? 'Only they can take part.' : 'Used for names and roll numbers in the export; access is by domain.'}
    </p>
    <div class="row">
      <button type="button" class="btn btn-secondary" onclick={() => rosterFile?.click()}><Icon name="upload" /> Choose a CSV file</button>
      <input bind:this={rosterFile} class="file" type="file" accept=".csv,text/csv" aria-label="Roster CSV file" tabindex="-1" onchange={readRosterFile} />
    </div>
    <div class="field">
      <label for="roster-text">Or paste the CSV</label>
      <textarea id="roster-text" class="input textarea-code" rows="5" spellcheck="false" bind:value={rosterText} aria-describedby="roster-help"></textarea>
      <p id="roster-help" class="field-help">One student per line: email, name, roll number, section. A header row naming the columns is fine.</p>
    </div>
    {#if roster}
      <p class="small" role="status">
        {roster.valid} {roster.valid === 1 ? 'line' : 'lines'} ready, {roster.invalid} refused.
        {#if rosterDiff}The file replaces the whole roster: it adds {rosterDiff.added} and removes {rosterDiff.removed}
          {rosterDiff.removed === 1 ? 'student' : 'students'}.{/if}
      </p>
      <div class="table-wrap preview">
        <table class="table table-dense">
          <caption class="visually-hidden">Roster preview</caption>
          <thead>
            <tr><th scope="col" class="right">Line</th><th scope="col">Email</th><th scope="col">Name</th><th scope="col">Roll number</th><th scope="col">Section</th><th scope="col">Check</th></tr>
          </thead>
          <tbody>
            {#each roster.lines.slice(0, 200) as l (l.line)}
              <tr class:bad={l.error !== ''}>
                <td class="num right">{l.line}</td>
                <td class="email">{l.email}</td>
                <td>{l.display_name}</td>
                <td class="num">{l.roll_number}</td>
                <td>{l.section}</td>
                <td class="check">{l.error || 'Ready'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if roster.lines.length > 200}<p class="muted small">Showing the first 200 of {roster.lines.length} lines.</p>{/if}
      <div class="actions">
        <button type="button" class="btn btn-primary" disabled={rosterBusy} onclick={uploadRoster}>Replace roster</button>
      </div>
    {/if}
    {#if rosterError}<p class="field-error" role="alert">{rosterError}</p>{/if}
    {#if rosterCount > 0}
      <details class="current" ontoggle={loadRoster}>
        <summary>Show the current roster</summary>
        {#if currentRoster === null}
          <p class="muted small" role="status">Loading the roster</p>
        {:else}
          <ul class="plain">
            {#each currentRoster as r (r.email)}<li><span class="mono">{r.email}</span> {r.name ?? r.display_name ?? ''} {r.roll_number}</li>{/each}
          </ul>
        {/if}
      </details>
    {/if}
  </section>

  <section class="section" aria-labelledby="admins-title">
    <h2 id="admins-title">Co-organisers</h2>
    <p class="muted small">They can change this contest and run its console, but no other contest.</p>
    {#if (contest.admins ?? []).length > 0}
      <ul class="plain admins">
        {#each contest.admins as a (a.user_id)}
          <li>
            <span>{a.name || a.email}</span> <span class="muted mono small">{a.email}</span>
            <button type="button" class="btn btn-danger-quiet btn-compact" onclick={() => removeAdmin(a.email, a.name || a.email)}>Remove {a.name || a.email}</button>
          </li>
        {/each}
      </ul>
    {/if}
    <form class="row" novalidate onsubmit={addAdmin}>
      <div class="field grow">
        <label for="admin-email">Email</label>
        <input id="admin-email" class="input" type="email" autocomplete="off" bind:value={adminEmail} aria-invalid={adminError ? 'true' : undefined} aria-describedby={adminError ? 'admin-error' : undefined} />
        {#if adminError}<p id="admin-error" class="field-error">{adminError}</p>{/if}
      </div>
      <button type="submit" class="btn btn-secondary">Add co-organiser</button>
    </form>
  </section>

  <section class="section" aria-labelledby="clone-title">
    <h2 id="clone-title">Clone</h2>
    <p class="muted small">Copies the settings and problems into a new, unpublished contest.</p>
    <form class="grid" novalidate onsubmit={cloneContest}>
      <div class="field">
        <label for="clone-slug">New slug</label>
        <input id="clone-slug" class="input mono" type="text" bind:value={cloneSlug} />
      </div>
      <div class="field">
        <label for="clone-start">Starts</label>
        <input id="clone-start" class="input" type="datetime-local" bind:value={cloneStart} />
      </div>
      <div class="field">
        <label for="clone-end">Ends</label>
        <input id="clone-end" class="input" type="datetime-local" bind:value={cloneEnd} />
      </div>
      <div class="actions span">
        <button type="submit" class="btn btn-secondary">Clone contest</button>
        {#if cloneError}<p class="field-error" role="alert">{cloneError}</p>{/if}
      </div>
    </form>
  </section>

  <section class="section" aria-labelledby="archive-title">
    <h2 id="archive-title">Archive</h2>
    <p class="muted small">Archived contests disappear from students' lists. Results and submissions are kept.</p>
    <div class="actions">
      <button type="button" class="btn btn-danger-quiet" onclick={archive}>Archive contest</button>
    </div>
  </section>

  <ConfirmDialog
    open={confirm !== null}
    title={confirm?.title ?? ''}
    body={confirm?.body ?? ''}
    confirmLabel={confirm?.label ?? ''}
    danger={confirm?.danger ?? false}
    busy={confirmBusy}
    onconfirm={runConfirm}
    oncancel={() => (confirm = null)}
  />
{/if}

<style>
  .section {
    display: grid;
    gap: var(--space-4);
    max-width: 56rem;
    margin-top: var(--space-6);
  }

  .section h2 {
    font-size: var(--fs-3);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr));
    gap: var(--space-4);
  }

  .span {
    grid-column: 1 / -1;
  }

  .group {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
    padding: 0;
    border: 0;
  }

  .group legend {
    margin-bottom: var(--space-2);
    padding: 0;
    font-size: var(--fs-0);
    font-weight: 500;
  }

  .checks {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-5);
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    cursor: pointer;
  }

  .choice input {
    flex: none;
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .narrow {
    max-width: 18rem;
    margin-top: var(--space-2);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  .banner ul {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink);
  }

  .small {
    font-size: var(--fs-0);
  }

  .ai-confirm {
    display: grid;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .picked {
    display: grid;
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .pick {
    display: grid;
    grid-template-columns: 4rem minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--space-3);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .letter {
    text-align: center;
    text-transform: uppercase;
  }

  .pick-title {
    display: grid;
    min-width: 0;
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .warn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--part);
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .buttons {
    display: flex;
    gap: var(--space-1);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
  }

  .grow {
    flex: 1 1 16rem;
  }

  .file {
    display: none;
  }

  .preview {
    max-height: 22rem;
    overflow: auto;
  }

  .right,
  .table .right {
    text-align: right;
  }

  .email {
    overflow-wrap: anywhere;
  }

  .bad .check {
    color: var(--bad);
    font-weight: 500;
  }

  .check {
    white-space: nowrap;
  }

  .current summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    font-size: var(--fs-0);
    font-weight: 500;
    cursor: pointer;
  }

  .plain {
    display: grid;
    gap: var(--space-1);
    margin-top: var(--space-2);
    padding: 0;
    list-style: none;
    font-size: var(--fs-0);
  }

  .admins li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  @media (max-width: 767px) {
    .pick {
      grid-template-columns: 3.5rem minmax(0, 1fr);
    }

    .buttons {
      grid-column: 1 / -1;
      justify-content: flex-end;
    }
  }
</style>
