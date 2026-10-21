<!--
  People: the owner's page for organisers, allowed email domains, account
  help and the site's history (v2 O1 and F10.1; /api/admin/people,
  /api/admin/domains, /api/admin/accounts/* and /api/admin/site-audit).

  - On a fresh install (no domain yet, or nobody but the configured owner),
    a short checklist comes first: add the college's domain, add another
    organiser, share the address.
  - Organisers: a table of everyone with the admin or owner role (stacked
    rows on phones), a form to add one, and after an add an inline note
    with the address to send them and a Copy button. Someone who has not
    signed in yet reads "Invited". A role is a select on each row that
    asks before it changes (the same POST upserts the role).
  - Allowed email domains: the list and a form to add one.
  - Organisation details (council C1-011): the name, contact email, phone
    and privacy contact the public pages show, saved with PUT
    /api/admin/site-settings ("Save organisation details"); each refused
    field says what to fix under itself, and the footer and sign-in page
    pick the new values up at once.
  - Account help: one email field for "Reset Google link", "End sessions"
    and "Disable account", then the disabled accounts with Enable. As an
    address is typed the field looks the account up (GET
    /api/admin/accounts?email=, students included) and says whose it is
    ("Asha Rao, student, last signed in ..."); for an address with no
    account it says so and the three actions stay disabled. The
    confirmation names the person (audit 2026-10-20 O10).
  - Semester rollover (council C1-012, D163): a term label and an
    optional "contests that ended before" date, "Preview rollover" (POST
    /api/admin/rollover without apply) lists the contests it would archive
    and the student accounts unseen for 180 days it would disable; then
    "Archive and disable" asks first, repeating the counts, and sends the
    same request with apply. Changing the label or the date drops the
    preview, so what is applied is what was read.
  - Integrity reviewers (v2 P4 F9.5, D97): per reviewer, the cases they
    opened by hand, cleared and closed with an outcome, their proposals,
    confirmations and appeal rulings, and how many of their cases are
    overdue; then the record view log, newest first: who read whose
    record page, case or notes, and when, with "Show older views".
  - From 1280 px a sticky "On this page" list beside the sections jumps
    to each one (the page is long; below 1280 px it reads top to bottom).
  - History: every change to people, domains and the site, newest first,
    with a filter by kind and a search by email, and "Show older changes".

  Every removal, role change, reset, sign-out, disable and enable asks
  first in a ConfirmDialog. Successes are toasts; form errors appear under
  their form and are tied to the inputs with aria-describedby; removal
  errors are error toasts. Anyone who is not an owner sees a short
  explanation instead. Takes no props.
-->
<script lang="ts">
  import PageState from '../components/PageState.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { onMount, tick, untrack } from 'svelte';
  import { focusFirstInvalid } from '../lib/focus';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import MonoDigits from '../components/MonoDigits.svelte';
  import Badge from '../components/Badge.svelte';
  import { polite } from '../lib/announce.svelte';
  import { runMode, type Organisation } from '../lib/mode.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../lib/api';
  import { countOf, formatDateTime, roleLabel } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { SITE_ACTIONS, actorText, feedQuery, siteAuditSummary, type SiteAuditEntry, type SiteAuditPage } from '../lib/history';
  import { viewKindWord, type RecordView, type ReviewerLoad } from '../lib/students';
  import { routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';
  import { showToast } from '../lib/toast.svelte';

  type OrganiserRole = 'admin' | 'owner';

  interface Person {
    id: number;
    email: string;
    name: string;
    role: OrganiserRole;
    last_login_at: number | null;
    locked: boolean;
    disabled?: boolean;
  }

  interface Domain {
    domain: string;
    created_at: number;
  }

  interface Account {
    id: number;
    email: string;
    name: string;
    role: string;
    disabled: boolean;
    last_login_at: number | null;
  }

  type Pending =
    | { kind: 'person'; person: Person }
    | { kind: 'role'; person: Person; role: OrganiserRole }
    | { kind: 'domain'; domain: string }
    | { kind: 'reset'; email: string }
    | { kind: 'end-sessions'; email: string }
    | { kind: 'disable'; email: string }
    | { kind: 'enable'; email: string }
    | null;

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let people = $state<Person[]>([]);
  let domains = $state<Domain[]>([]);

  let email = $state('');
  let role = $state<OrganiserRole>('admin');
  let addingPerson = $state(false);
  let personError = $state('');
  /** The organiser just added, for the "send them this address" note. */
  let invited = $state<string | null>(null);

  let domainInput = $state('');
  let addingDomain = $state(false);
  let domainError = $state('');

  let disabled = $state<Account[]>([]);
  let accountEmail = $state('');
  let accountError = $state('');

  let pending = $state<Pending>(null);
  let removing = $state(false);

  // History.
  let history = $state<SiteAuditEntry[]>([]);
  let historyState = $state<'loading' | 'ready' | 'error'>('loading');
  let historyPage = $state(1);
  let historyMore = $state(false);
  let historyAction = $state('');
  let historySearch = $state('');
  let historyBusy = $state(false);

  // Organisation details (C1-011).
  const EMPTY_ORG: Organisation = { name: '', contact_email: '', phone: '', privacy_contact: '' };
  let org = $state<Organisation>({ ...EMPTY_ORG });
  let orgErrors = $state<Record<string, string>>({});
  let orgError = $state('');
  let orgBusy = $state(false);

  // Semester rollover (C1-012).
  interface RolloverPlan {
    applied: boolean;
    label: string;
    ended_before: number;
    idle_days: number;
    contests: { id: number; slug: string; title: string; ends_at: number }[];
    accounts: Account[];
    server_now?: number;
  }
  let rollLabel = $state('');
  let rollBefore = $state('');
  let rollError = $state('');
  let rollBusy = $state(false);
  let rollPlan = $state.raw<RolloverPlan | null>(null);
  let rollDone = $state.raw<RolloverPlan | null>(null);
  let rollConfirm = $state(false);

  // Integrity reviewers and the record view log (F9.5).
  let reviewers = $state<ReviewerLoad[] | null>(null);
  let reviewersError = $state('');
  let views = $state<RecordView[]>([]);
  let viewsMore = $state(false);
  let viewsState = $state<'loading' | 'ready' | 'error'>('loading');
  let viewsBusy = $state(false);

  // The account the help field names, looked up as the address is typed (O10).
  let lookup = $state<{ email: string; status: 'loading' | 'found' | 'none' | 'error'; account?: Account } | null>(null);
  let lookupTimer: ReturnType<typeof setTimeout> | undefined;

  let organisersHeading: HTMLHeadingElement | undefined = $state();
  let domainsHeading: HTMLHeadingElement | undefined = $state();

  const isOwner = $derived(session.user?.role === 'owner');
  const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;
  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  // Organisers other than the configured owner and the person signed in (audit O10).
  const coTeachers = $derived(people.filter((p) => !p.locked && p.id !== session.user?.id).length);

  /** The account the help field names: looked up, or already on this page. */
  const accountMatch = $derived.by(() => {
    const e = accountEmail.trim().toLowerCase();
    if (e === '') return null;
    if (lookup && lookup.email === e && lookup.status === 'found' && lookup.account) {
      const a = lookup.account;
      return { name: a.name.trim() || a.email, role: roleWord(a.role), last: a.last_login_at, disabled: a.disabled };
    }
    const person = people.find((x) => x.email.toLowerCase() === e);
    if (person) return { name: personLabel(person), role: roleLabel(person.role).toLowerCase(), last: person.last_login_at, disabled: !!person.disabled };
    const account = disabled.find((x) => x.email.toLowerCase() === e);
    if (account) return { name: account.name.trim() || account.email, role: account.role, last: account.last_login_at, disabled: true };
    return null;
  });
  const accountLooksLikeEmail = $derived(EMAIL.test(accountEmail.trim()));
  /** The lookup found no account for the address typed: the actions would only be refused. */
  const noAccount = $derived(lookup !== null && lookup.email === accountEmail.trim().toLowerCase() && lookup.status === 'none');
  /** An address typed but not yet named above (lookup running, failed or found nothing): never act on it (O10, D232, C2-031). */
  const unseen = $derived(accountEmail.trim() !== '' && accountMatch === null);

  function roleWord(role: string): string {
    return role === 'owner' || role === 'admin' || role === 'student' ? roleLabel(role).toLowerCase() : role;
  }

  // A changed label or date drops the preview, so what is applied is what was read.
  $effect(() => {
    void rollLabel;
    void rollBefore;
    untrack(() => {
      rollPlan = null;
    });
  });

  $effect(() => {
    const e = accountEmail.trim().toLowerCase();
    clearTimeout(lookupTimer);
    if (!EMAIL.test(e)) {
      lookup = null;
      return;
    }
    if (untrack(() => lookup?.email) === e) return;
    lookupTimer = setTimeout(() => void lookUp(e), 250);
  });

  async function lookUp(e: string) {
    lookup = { email: e, status: 'loading' };
    try {
      const d = await getJSON<{ account: Account }>(`/api/admin/accounts?email=${encodeURIComponent(e)}`);
      if (lookup?.email === e) lookup = { email: e, status: 'found', account: d.account };
    } catch (err) {
      if (handleSessionError(err) || lookup?.email !== e) return;
      lookup = { email: e, status: err instanceof ApiError && err.status === 404 ? 'none' : 'error' };
    }
  }

  async function saveOrganisation(event: SubmitEvent) {
    event.preventDefault();
    orgErrors = {};
    orgError = '';
    orgBusy = true;
    try {
      const saved = await sendJSON<Organisation>('PUT', '/api/admin/site-settings', {
        name: org.name.trim(),
        contact_email: org.contact_email.trim(),
        phone: org.phone.trim(),
        privacy_contact: org.privacy_contact.trim(),
      });
      org = { ...saved };
      runMode.organisation = { ...saved };
      if (saved.contact_email !== '') runMode.contactEmail = saved.contact_email;
      showToast('Saved the organisation details');
      void loadHistory(1);
    } catch (err) {
      if (handleSessionError(err)) return;
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        orgErrors = { ...err.fields };
        await focusFirstInvalid('#org-form');
      } else {
        orgError = errorText(err);
      }
    } finally {
      orgBusy = false;
    }
  }

  /** The rollover request: the label, the optional date as the start of that day, and whether to apply it. */
  function rolloverBody(apply: boolean): Record<string, unknown> {
    const body: Record<string, unknown> = { label: rollLabel.trim(), apply };
    if (rollBefore !== '') {
      const [y, m, d] = rollBefore.split('-').map(Number);
      body.ended_before = new Date(y, m - 1, d).getTime();
    }
    return body;
  }

  async function previewRollover(event: SubmitEvent) {
    event.preventDefault();
    rollError = '';
    rollDone = null;
    if (rollLabel.trim() === '') {
      rollError = 'Name the term first, for example 2026 odd semester.';
      document.getElementById('roll-label')?.focus();
      return;
    }
    rollBusy = true;
    try {
      rollPlan = await sendJSON<RolloverPlan>('POST', '/api/admin/rollover', rolloverBody(false));
    } catch (err) {
      if (!handleSessionError(err)) rollError = err instanceof ApiError ? (err.fields.label ?? err.message) : errorText(err);
    } finally {
      rollBusy = false;
    }
  }

  async function applyRollover() {
    rollBusy = true;
    try {
      // As of the preview, so an account that crossed 180 days since is not disabled unseen (C2-033).
      const asOf = rollPlan?.server_now;
      const done = await sendJSON<RolloverPlan>('POST', '/api/admin/rollover', { ...rolloverBody(true), ...(asOf ? { as_of: asOf } : {}) });
      rollDone = done;
      rollPlan = null;
      rollConfirm = false;
      disabled = [...disabled.filter((a) => !done.accounts.some((x) => x.id === a.id)), ...done.accounts.map((a) => ({ ...a, disabled: true }))].sort((a, b) =>
        a.email.localeCompare(b.email),
      );
      showToast(`Archived ${countOf(done.contests.length, 'contest')} and disabled ${countOf(done.accounts.length, 'account')}`);
      void loadHistory(1);
    } catch (err) {
      rollConfirm = false;
      if (!handleSessionError(err)) rollError = errorText(err);
    } finally {
      rollBusy = false;
    }
  }

  async function loadReviewers() {
    reviewersError = '';
    try {
      const d = await getJSON<{ reviewers: ReviewerLoad[] }>('/api/admin/integrity/oversight');
      reviewers = d.reviewers ?? [];
    } catch (err) {
      if (!handleSessionError(err)) reviewersError = errorText(err);
    }
  }

  async function loadViews(more = false) {
    viewsBusy = true;
    if (!more) viewsState = 'loading';
    try {
      const before = more && views.length > 0 ? `?before=${views[views.length - 1].id}` : '';
      const d = await getJSON<{ views: RecordView[]; has_more?: boolean }>(`/api/admin/record-views${before}`);
      views = more ? [...views, ...(d.views ?? [])] : (d.views ?? []);
      viewsMore = d.has_more === true;
      viewsState = 'ready';
    } catch (err) {
      if (handleSessionError(err)) return;
      if (more) showToast(errorText(err), 'error');
      else viewsState = 'error';
    } finally {
      viewsBusy = false;
    }
  }

  /** "Asha Verma (asha@college.edu)" when the page knows the name, else the address. */
  function accountName(target: string): string {
    const m = accountMatch;
    return m && m.name !== target ? `${m.name} (${target})` : target;
  }
  const fresh = $derived(loadState === 'ready' && (domains.length === 0 || coTeachers === 0));

  function personLabel(p: Pick<Person, 'name' | 'email'>): string {
    return p.name.trim() !== '' ? p.name : p.email;
  }

  const confirmCopy = $derived.by(() => {
    if (pending?.kind === 'person') {
      return {
        title: `Remove ${personLabel(pending.person)} as ${pending.person.role}?`,
        body: 'They keep their account and become a student.',
        label: 'Remove',
      };
    }
    if (pending?.kind === 'role') {
      const owner = pending.role === 'owner';
      return {
        title: `Make ${personLabel(pending.person)} ${owner ? 'an owner' : 'an admin'}?`,
        body: owner
          ? 'Owners can also manage people, domains and accounts on this page.'
          : 'They keep contests and problems, and can no longer open this page.',
        label: 'Change role',
      };
    }
    if (pending?.kind === 'domain') {
      return {
        title: `Remove @${pending.domain} from the allowed list?`,
        body: `People with @${pending.domain} addresses will no longer be able to sign in. Existing organisers are not affected.`,
        label: 'Remove',
      };
    }
    if (pending?.kind === 'reset') {
      return {
        title: `Reset the Google account link for ${accountName(pending.email)}?`,
        body: 'Their current sessions end. The next Google account that signs in with this address is linked to it.',
        label: 'Reset Google link',
      };
    }
    if (pending?.kind === 'end-sessions') {
      return {
        title: `End every session of ${accountName(pending.email)}?`,
        body: 'They are signed out on every device at once, open contest pages included. They can sign in again.',
        label: 'End sessions',
      };
    }
    if (pending?.kind === 'disable') {
      return {
        title: `Disable ${accountName(pending.email)}?`,
        body: 'They are signed out on every device at once and cannot sign in until an owner enables the account again.',
        label: 'Disable account',
      };
    }
    if (pending?.kind === 'enable') {
      return {
        title: `Enable ${pending.email}?`,
        body: 'They can sign in again with their Google account.',
        label: 'Enable account',
      };
    }
    return { title: '', body: '', label: 'Remove' };
  });

  /** Handles errors every request can hit; returns true when it did. */
  function handleSessionError(err: unknown): boolean {
    if (!(err instanceof ApiError)) return false;
    if (err.status === 401) {
      session.user = undefined;
      session.status = 'signed-out';
      return true;
    }
    if (err.status === 403 && err.code === 'forbidden') {
      loadState = 'forbidden';
      return true;
    }
    return false;
  }

  async function load() {
    loadState = 'loading';
    try {
      const data = await getJSON<{ people: Person[]; domains: Domain[]; disabled?: Account[]; organisation?: Organisation }>('/api/admin/people');
      people = data.people ?? [];
      if (data.organisation) org = { ...EMPTY_ORG, ...data.organisation };
      domains = data.domains ?? [];
      disabled = data.disabled ?? [];
      loadState = 'ready';
      void loadHistory(1);
      void loadReviewers();
      void loadViews();
    } catch (err) {
      if (handleSessionError(err)) return;
      loadError = errorText(err);
      loadState = 'error';
    }
  }

  async function loadHistory(page: number) {
    historyBusy = true;
    if (page === 1) historyState = 'loading';
    try {
      const data = await getJSON<SiteAuditPage>(
        `/api/admin/site-audit${feedQuery({ page, action: historyAction, q: historySearch.trim() })}`,
      );
      history = page === 1 ? (data.entries ?? []) : [...history, ...(data.entries ?? [])];
      historyPage = page;
      historyMore = data.has_more === true;
      historyState = 'ready';
    } catch (err) {
      if (handleSessionError(err)) return;
      if (page === 1) historyState = 'error';
      else showToast(errorText(err), 'error');
    } finally {
      historyBusy = false;
    }
  }

  function filterHistory(event?: SubmitEvent) {
    event?.preventDefault();
    void loadHistory(1);
  }

  onMount(() => {
    setTitle(routeTitle('people'));
    if (isOwner) {
      load();
    } else {
      loadState = 'forbidden';
    }
  });

  async function addPerson(event: SubmitEvent) {
    event.preventDefault();
    personError = '';
    const value = email.trim();
    if (value === '') {
      personError = 'Enter an email address.';
      document.getElementById('person-email')?.focus();
      return;
    }
    addingPerson = true;
    try {
      const person = await sendJSON<Person>('POST', '/api/admin/people', { email: value, role });
      const index = people.findIndex((p) => p.id === person.id);
      people = index === -1 ? [...people, person] : people.map((p) => (p.id === person.id ? person : p));
      email = '';
      invited = person.last_login_at === null ? person.email : null;
      showToast(`Added ${person.email} as ${person.role}`);
      void loadHistory(1);
    } catch (err) {
      if (!handleSessionError(err)) personError = errorText(err);
    } finally {
      addingPerson = false;
    }
  }

  async function copyAddress() {
    try {
      await navigator.clipboard.writeText(origin);
      showToast('Copied the address');
    } catch {
      polite(`Copy did not work here. The address is ${origin}`);
      showToast('Copy did not work here. Select the address and copy it yourself.', 'error');
    }
  }

  function askRole(event: Event, person: Person) {
    const select = event.currentTarget as HTMLSelectElement;
    const next = select.value as OrganiserRole;
    // The select keeps showing the current role until the change is confirmed.
    select.value = person.role;
    if (next !== person.role) pending = { kind: 'role', person, role: next };
  }

  async function confirmRole(person: Person, next: OrganiserRole) {
    removing = true;
    try {
      const updated = await sendJSON<Person>('POST', '/api/admin/people', { email: person.email, role: next });
      people = people.map((p) => (p.id === person.id ? { ...p, ...updated } : p));
      showToast(`${personLabel(person)} is now ${next === 'owner' ? 'an owner' : 'an admin'}`);
      void loadHistory(1);
    } catch (err) {
      if (!handleSessionError(err)) showToast(errorText(err), 'error');
    } finally {
      removing = false;
      pending = null;
    }
  }

  async function addDomain(event: SubmitEvent) {
    event.preventDefault();
    domainError = '';
    const value = domainInput.trim();
    if (value === '' || value === '@') {
      domainError = 'Enter a domain, for example college.edu.';
      document.getElementById('domain-input')?.focus();
      return;
    }
    addingDomain = true;
    try {
      const added = await sendJSON<Domain>('POST', '/api/admin/domains', { domain: value });
      domainInput = '';
      if (domains.some((d) => d.domain === added.domain)) {
        showToast(`@${added.domain} is already on the list`);
      } else {
        domains = [...domains, added];
        showToast(`Added @${added.domain}`);
        void loadHistory(1);
      }
    } catch (err) {
      if (!handleSessionError(err)) domainError = errorText(err);
    } finally {
      addingDomain = false;
    }
  }

  /** Checks the account email and asks before a reset, sign-out or disable. */
  function askAccount(kind: 'reset' | 'end-sessions' | 'disable') {
    accountError = '';
    const value = accountEmail.trim();
    if (value === '') {
      accountError = 'Enter an email address.';
      document.getElementById('account-email')?.focus();
      return;
    }
    if (unseen) return;
    pending = { kind, email: value };
  }

  async function confirmReset(target: string) {
    removing = true;
    try {
      await sendJSON<void>('POST', '/api/admin/accounts/reset-google', { email: target });
      accountEmail = '';
      showToast(`Reset the Google account link for ${target}`);
      void loadHistory(1);
    } catch (err) {
      if (!handleSessionError(err)) accountError = errorText(err);
    } finally {
      removing = false;
      pending = null;
    }
  }

  async function confirmAccount(kind: 'end-sessions' | 'disable' | 'enable', target: string) {
    removing = true;
    try {
      const out = await sendJSON<{ account: Account; ended: number }>('POST', `/api/admin/accounts/${kind}`, { email: target });
      if (kind === 'end-sessions') {
        showToast(out.ended === 0 ? `${target} had no open sessions` : `Ended ${countOf(out.ended, 'session')} of ${target}`);
      } else if (kind === 'disable') {
        disabled = [...disabled.filter((a) => a.id !== out.account.id), out.account].sort((a, b) => a.email.localeCompare(b.email));
        showToast(`Disabled ${target}`);
      } else {
        disabled = disabled.filter((a) => a.id !== out.account.id);
        showToast(`Enabled ${target}`);
      }
      if (kind !== 'enable') accountEmail = '';
      people = people.map((p) => (p.id === out.account.id ? { ...p, disabled: out.account.disabled } : p));
      void loadHistory(1);
    } catch (err) {
      if (!handleSessionError(err)) {
        if (kind === 'enable') showToast(errorText(err), 'error');
        else accountError = errorText(err);
      }
    } finally {
      removing = false;
      pending = null;
    }
  }

  async function confirmAction() {
    const target = pending;
    if (!target) return;
    if (target.kind === 'reset') return confirmReset(target.email);
    if (target.kind === 'role') return confirmRole(target.person, target.role);
    if (target.kind === 'end-sessions' || target.kind === 'disable' || target.kind === 'enable') {
      return confirmAccount(target.kind, target.email);
    }
    removing = true;
    try {
      if (target.kind === 'person') {
        await sendJSON<void>('DELETE', `/api/admin/people/${target.person.id}`);
        people = people.filter((p) => p.id !== target.person.id);
        if (invited === target.person.email) invited = null;
        showToast(`Removed ${personLabel(target.person)}`);
      } else {
        await sendJSON<void>('DELETE', `/api/admin/domains/${encodeURIComponent(target.domain)}`);
        domains = domains.filter((d) => d.domain !== target.domain);
        showToast(`Removed @${target.domain}`);
      }
      void loadHistory(1);
    } catch (err) {
      if (!handleSessionError(err)) {
        showToast(errorText(err), 'error');
        if (err instanceof ApiError && err.status === 404) {
          if (target.kind === 'person') people = people.filter((p) => p.id !== target.person.id);
          else domains = domains.filter((d) => d.domain !== target.domain);
        }
      }
    } finally {
      removing = false;
      pending = null;
    }
    // The button that opened the dialog may be gone; start again from the section heading.
    await tick();
    (target.kind === 'person' ? organisersHeading : domainsHeading)?.focus();
  }
</script>

<PageHeader title="People" />

{#if loadState === 'forbidden'}
  <PageState kind="forbidden" message="Only owners can manage people." />
{:else if loadState === 'loading'}
  <PageState kind="loading" message="Loading people" />
{:else if loadState === 'error'}
  <PageState kind="error" message={loadError} onretry={load} />
{:else}
  <div class="page-split people-split">
    <div class="page-split-main">
  {#if fresh}
    <section class="section start" aria-labelledby="start-heading">
      <h2 id="start-heading">Getting started</h2>
      <ul class="steps">
        <li>
          <span class="step">Add your college's email domain, so students can sign in.</span>
          <Badge tone={domains.length > 0 ? 'ok' : 'neutral'} text={domains.length > 0 ? 'Done' : 'To do'} />
        </li>
        <li>
          <span class="step">Add another organiser, so someone else can run a contest.</span>
          <Badge tone={coTeachers > 0 ? 'ok' : 'neutral'} text={coTeachers > 0 ? 'Done' : 'To do'} />
        </li>
        <li>
          <span class="step">Share the address with your students: <span class="address">{origin}</span></span>
          <button type="button" class="btn btn-secondary" onclick={copyAddress}>Copy address</button>
        </li>
      </ul>
    </section>
  {/if}

  <section class="section" aria-labelledby="organisers-heading">
    <h2 id="organisers-heading" tabindex="-1" bind:this={organisersHeading}>Organisers</h2>
    <p class="intro muted">Admins write problems and run contests. Owners can also manage people.</p>

    <div class="table-wrap organisers">
      <table class="table table-stack">
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Email</th>
            <th scope="col">Role</th>
            <th scope="col">Last sign-in</th>
            <th scope="col"><span class="visually-hidden">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each people as person (person.id)}
            <tr>
              <th scope="row" class="name">
                {#if person.name.trim() !== ''}{person.name}{:else}<span class="muted">Invited</span>{/if}
              </th>
              <td class="email">{person.email}</td>
              <td class="role">
                {#if person.locked}
                  {roleLabel(person.role)}
                {:else}
                  <select id="role-{person.id}" class="select role-select" aria-label="Role for {personLabel(person)}" value={person.role} onchange={(e) => askRole(e, person)}>
                    <option value="admin">Admin</option>
                    <option value="owner">Owner</option>
                  </select>
                {/if}
                {#if person.disabled}<span class="muted">, disabled</span>{/if}
              </td>
              <td class="num last">
                <span class="cell-label">Last sign-in</span>{#if person.last_login_at !== null}<MonoDigits text={formatDateTime(person.last_login_at)} />{:else}<span class="muted word">Never</span>{/if}
              </td>
              <td class="actions">
                {#if person.locked}
                  <span class="muted locked">Set in server configuration</span>
                {:else}
                  <button
                    type="button"
                    class="btn btn-quiet btn-danger-quiet btn-compact"
                    aria-label="Remove {personLabel(person)}"
                    onclick={() => (pending = { kind: 'person', person })}
                  >
                    Remove
                  </button>
                {/if}
              </td>
            </tr>
          {:else}
            <tr>
              <td colspan="5" class="muted">No organisers yet. Add one below.</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <form class="form" novalidate onsubmit={addPerson}>
      <div class="field grow">
        <label for="person-email">Email</label>
        <input
          id="person-email"
          aria-required="true"
          class="input"
          type="email"
          autocomplete="off"
          spellcheck="false"
          bind:value={email}
          aria-invalid={personError !== '' ? 'true' : undefined}
          aria-describedby={personError !== '' ? 'person-error' : undefined}
        />
      </div>
      <div class="field">
        <label for="person-role">Role</label>
        <select id="person-role" class="select" bind:value={role} aria-describedby={personError !== '' ? 'person-error' : undefined}>
          <option value="admin">Admin</option>
          <option value="owner">Owner</option>
        </select>
      </div>
      <button type="submit" class="btn btn-primary submit" disabled={addingPerson}>Add organiser</button>
    </form>
    {#if personError !== ''}
      <p id="person-error" class="field-error form-error" role="alert">{personError}</p>
    {/if}
    {#if invited}
      <div class="invite" role="status">
        <p>Send {invited} this address: <span class="address">{origin}</span>. They sign in with Google using that email.</p>
        <div class="invite-actions">
          <button type="button" class="btn btn-secondary" onclick={copyAddress}>Copy address</button>
          <button type="button" class="btn btn-quiet btn-compact" onclick={() => (invited = null)}>Dismiss</button>
        </div>
      </div>
    {/if}
  </section>

  <section class="section" aria-labelledby="domains-heading">
    <h2 id="domains-heading" tabindex="-1" bind:this={domainsHeading}>Allowed email domains</h2>
    <p class="intro muted">Anyone with a Google account on these domains can sign in as a student.</p>

    {#if domains.length > 0}
      <ul class="domains">
        {#each domains as d (d.domain)}
          <li>
            <span class="domain">@{d.domain}</span>
            <button
              type="button"
              class="btn btn-quiet btn-danger-quiet btn-compact"
              aria-label="Remove @{d.domain}"
              onclick={() => (pending = { kind: 'domain', domain: d.domain })}
            >
              Remove
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="empty muted">No domains yet. Until you add one, only organisers can sign in.</p>
    {/if}

    <form class="form" novalidate onsubmit={addDomain}>
      <div class="field grow">
        <label for="domain-input">Domain</label>
        <input
          id="domain-input"
          aria-required="true"
          class="input"
          type="text"
          inputmode="url"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          placeholder="college.edu"
          bind:value={domainInput}
          aria-invalid={domainError !== '' ? 'true' : undefined}
          aria-describedby={domainError !== '' ? 'domain-error' : undefined}
        />
      </div>
      <button type="submit" class="btn btn-primary submit" disabled={addingDomain}>Add domain</button>
    </form>
    {#if domainError !== ''}
      <p id="domain-error" class="field-error form-error" role="alert">{domainError}</p>
    {/if}
  </section>

  <section class="section" aria-labelledby="org-heading">
    <h2 id="org-heading">Organisation details</h2>
    <p class="intro muted">Shown on the sign-in page, in the footer and on the privacy page. Use a role address as the contact email, so your own address stops appearing on public pages.</p>
    <form id="org-form" class="org" novalidate onsubmit={saveOrganisation}>
      {#each [{ key: 'name', label: 'Organisation name', type: 'text', help: 'For example Example College of Engineering.' }, { key: 'contact_email', label: 'Contact email', type: 'email', help: 'Shown to everyone, signed in or not.' }, { key: 'phone', label: 'Phone', type: 'tel', help: 'Optional. Digits, spaces and + - ( ) only.' }, { key: 'privacy_contact', label: 'Privacy contact', type: 'text', help: 'Who to ask about personal data: a name or an address.' }] as f (f.key)}
        <div class="field">
          <label for="org-{f.key}">{f.label}</label>
          <input
            id="org-{f.key}"
            class="input"
            type={f.type}
            autocomplete="off"
            spellcheck="false"
            bind:value={org[f.key as keyof Organisation]}
            aria-invalid={orgErrors[f.key] ? 'true' : undefined}
            aria-describedby="org-{f.key}-help{orgErrors[f.key] ? ` org-${f.key}-error` : ''}"
          />
          <p id="org-{f.key}-help" class="field-help">{f.help}</p>
          {#if orgErrors[f.key]}<p id="org-{f.key}-error" class="field-error">{orgErrors[f.key]}</p>{/if}
        </div>
      {/each}
      {#if orgError}<p class="field-error" role="alert">{orgError}</p>{/if}
      <button type="submit" class="btn btn-secondary" disabled={orgBusy}>Save organisation details</button>
    </form>
  </section>

  <section class="section" aria-labelledby="help-heading">
    <h2 id="help-heading">Account help</h2>
    <p class="intro muted">
      Reset Google link when someone's address moved to a new Google account, so their next sign-in is accepted. End sessions signs
      someone out on every device, for a lost laptop or a lab PC left signed in. Disable account also stops them signing in until
      you enable it again.
    </p>

    <form class="form" novalidate onsubmit={(e) => { e.preventDefault(); askAccount('reset'); }}>
      <div class="field grow">
        <label for="account-email">Email</label>
        <input
          id="account-email"
          aria-required="true"
          class="input"
          type="email"
          autocomplete="off"
          spellcheck="false"
          bind:value={accountEmail}
          aria-invalid={accountError !== '' ? 'true' : undefined}
          aria-describedby="account-who{accountError !== '' ? ' account-error' : ''}"
        />
        <p id="account-who" class="field-help who" aria-live="polite">
          {#if accountMatch}
            <MonoDigits
              text={`${accountMatch.name}, ${accountMatch.role}${accountMatch.disabled ? ', disabled' : ''}, ${accountMatch.last !== null ? `last signed in ${formatDateTime(accountMatch.last)}` : 'never signed in'}.`}
            />
          {:else if accountLooksLikeEmail && noAccount}
            Nobody with that email has an account. Check the address.
          {:else if accountLooksLikeEmail && lookup?.status === 'error'}
            Could not look the account up. Check the address before you act.
          {:else if accountLooksLikeEmail}
            Looking up the account.
          {:else}
            The account's email address, as they sign in with it.
          {/if}
        </p>
      </div>
      <button type="submit" class="btn btn-secondary submit" disabled={unseen || (removing && pending?.kind === 'reset')} aria-describedby={unseen ? 'account-who' : undefined}>Reset Google link</button>
      <button
        type="button"
        class="btn btn-secondary submit"
        disabled={unseen || (removing && pending?.kind === 'end-sessions')}
        aria-describedby={unseen ? 'account-who' : undefined}
        onclick={() => askAccount('end-sessions')}
      >
        End sessions
      </button>
      <button
        type="button"
        class="btn btn-secondary btn-danger-secondary submit"
        disabled={unseen || (removing && pending?.kind === 'disable')}
        aria-describedby={unseen ? 'account-who' : undefined}
        onclick={() => askAccount('disable')}
      >
        Disable account
      </button>
    </form>
    {#if accountError !== ''}
      <p id="account-error" class="field-error form-error" role="alert">{accountError}</p>
    {/if}

    <h3 class="subhead">Disabled accounts</h3>
    {#if disabled.length > 0}
      <ul class="domains">
        {#each disabled as a (a.id)}
          <li>
            <span class="domain">{a.email}{#if a.name.trim() !== ''}<span class="muted"> ({a.name})</span>{/if}</span>
            <button
              type="button"
              class="btn btn-quiet btn-compact"
              aria-label="Enable {a.email}"
              onclick={() => (pending = { kind: 'enable', email: a.email })}
            >
              Enable
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="empty muted">No account is disabled.</p>
    {/if}
  </section>

  <section class="section" aria-labelledby="rollover-heading">
    <h2 id="rollover-heading">Semester rollover</h2>
    <p class="intro muted">
      At the end of a term: archive the contests that have ended and disable the student accounts nobody has used for 180 days. Nothing is
      deleted, and you can enable any account again above. Preview first; nothing changes until you confirm.
    </p>
    <form class="form" novalidate onsubmit={previewRollover}>
      <div class="field grow">
        <label for="roll-label">Term</label>
        <input id="roll-label" aria-required="true" class="input" type="text" autocomplete="off" placeholder="2026 odd semester" bind:value={rollLabel} aria-invalid={rollError ? 'true' : undefined} aria-describedby={rollError ? 'roll-error' : undefined} />
      </div>
      <div class="field">
        <label for="roll-before">Contests that ended before (optional)</label>
        <input id="roll-before" class="input" type="date" bind:value={rollBefore} />
      </div>
      <button type="submit" class="btn btn-secondary submit" disabled={rollBusy}>Preview rollover</button>
    </form>
    {#if rollError}<p id="roll-error" class="field-error form-error" role="alert">{rollError}</p>{/if}
    {#if rollPlan}
      <div class="plan" role="region" aria-label="Rollover preview">
        <p><strong>{countOf(rollPlan.contests.length, 'contest')}</strong> to archive and <strong>{countOf(rollPlan.accounts.length, 'student account')}</strong> to disable for {rollPlan.label}.</p>
        {#if rollPlan.contests.length > 0}
          <details>
            <summary>Contests</summary>
            <ul class="plan-list small">{#each rollPlan.contests as c (c.id)}<li>{c.title}, ended <MonoDigits text={formatDateTime(c.ends_at)} /></li>{/each}</ul>
          </details>
        {/if}
        {#if rollPlan.accounts.length > 0}
          <details>
            <summary>Accounts</summary>
            <ul class="plan-list small">
              {#each rollPlan.accounts as a (a.id)}<li>{a.email}{a.name ? ` (${a.name})` : ''}, {#if a.last_login_at !== null}last signed in <MonoDigits text={formatDateTime(a.last_login_at)} />{:else}never signed in{/if}</li>{/each}
            </ul>
          </details>
        {/if}
        {#if rollPlan.contests.length + rollPlan.accounts.length > 0}
          <button type="button" class="btn btn-secondary btn-danger-secondary" disabled={rollBusy} onclick={() => (rollConfirm = true)}>Archive and disable</button>
        {:else}
          <p class="muted small">Nothing to do for this term.</p>
        {/if}
      </div>
    {/if}
    {#if rollDone}
      <p class="muted small history-state" role="status">
        {rollDone.label}: archived {countOf(rollDone.contests.length, 'contest')} and disabled {countOf(rollDone.accounts.length, 'account')}.
      </p>
    {/if}
  </section>

  <section class="section" aria-labelledby="reviewers-heading">
    <h2 id="reviewers-heading">Integrity reviewers</h2>
    <p class="intro muted">What each owner and admin has done in integrity review, so the work is shared and nothing waits too long. Only you see this.</p>
    {#if reviewersError}
      <p class="muted small history-state">{reviewersError} <button type="button" class="btn btn-quiet btn-compact" onclick={loadReviewers}>Try again</button></p>
    {:else if reviewers === null}
      <p class="muted small history-state" role="status">Loading the reviewers</p>
    {:else if reviewers.length === 0}
      <p class="muted small history-state">Nobody has reviewed a case yet.</p>
    {:else}
      <div class="table-wrap reviewers">
        <table class="table table-dense table-stack">
          <caption class="visually-hidden">Integrity work by reviewer</caption>
          <thead>
            <tr>
              <th scope="col">Reviewer</th>
              <th scope="col" class="right">Opened by hand</th>
              <th scope="col" class="right">Cleared</th>
              <th scope="col" class="right">Outcomes</th>
              <th scope="col" class="right">Proposals</th>
              <th scope="col" class="right">Confirmations</th>
              <th scope="col" class="right">Appeal rulings</th>
              <th scope="col" class="right">Overdue</th>
            </tr>
          </thead>
          <tbody>
            {#each reviewers as r (r.user_id)}
              <tr>
                <th scope="row" class="r-name">{r.name || r.email}{#if r.name}<span class="r-email muted">{r.email}</span>{/if}</th>
                <td class="num right"><span class="cell-label">Opened by hand</span>{r.manual_opened}</td>
                <td class="num right"><span class="cell-label">Cleared</span>{r.cleared}</td>
                <td class="num right"><span class="cell-label">Outcomes</span>{r.actioned}</td>
                <td class="num right"><span class="cell-label">Proposals</span>{r.proposals}</td>
                <td class="num right"><span class="cell-label">Confirmations</span>{r.confirmations}</td>
                <td class="num right"><span class="cell-label">Appeal rulings</span>{r.rulings}</td>
                <td class="num right" class:late={r.overdue > 0}><span class="cell-label">Overdue</span>{r.overdue}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <h3 class="subhead">Record views</h3>
    <p class="intro muted">Every time an organiser read a student's record page, a case or the notes on a submission, newest first.</p>
    {#if viewsState === 'loading'}
      <p class="muted small history-state" role="status">Loading the record views</p>
    {:else if viewsState === 'error'}
      <p class="muted small history-state">The record views could not be loaded. <button type="button" class="btn btn-quiet btn-compact" onclick={() => loadViews()}>Try again</button></p>
    {:else if views.length === 0}
      <p class="muted small history-state">Nobody has read a record yet.</p>
    {:else}
      <ul class="history" aria-label="Record views, newest first">
        {#each views as v (v.id)}
          <li>
            <span class="when"><MonoDigits text={formatDateTime(v.at)} /></span>
            <span class="what">{v.viewer.name || v.viewer.email} read {v.subject.name ? `${v.subject.name}'s` : "a student's"} {viewKindWord(v.kind).toLowerCase()}</span>
            <span class="who muted">{v.viewer.email}</span>
          </li>
        {/each}
      </ul>
      {#if viewsMore}
        <button type="button" class="btn btn-secondary more" disabled={viewsBusy} onclick={() => loadViews(true)}>Show older views</button>
      {/if}
    {/if}
  </section>

  <section class="section" aria-labelledby="history-heading">
    <h2 id="history-heading">History</h2>
    <p class="intro muted">Every change to people, domains and the site, newest first. Nothing here can be edited or removed.</p>

    <form class="form filters" novalidate onsubmit={filterHistory}>
      <div class="field">
        <label for="history-action">Kind of change</label>
        <select id="history-action" class="select" bind:value={historyAction} onchange={() => filterHistory()}>
          <option value="">All changes</option>
          {#each SITE_ACTIONS as a (a.id)}<option value={a.id}>{a.label}</option>{/each}
        </select>
      </div>
      <div class="field grow">
        <label for="history-search">Email contains</label>
        <input id="history-search" class="input" type="search" autocomplete="off" spellcheck="false" bind:value={historySearch} />
      </div>
      <button type="submit" class="btn btn-secondary submit" disabled={historyBusy}>Search</button>
    </form>

    {#if historyState === 'loading'}
      <p class="muted small history-state" role="status">Loading the history</p>
    {:else if historyState === 'error'}
      <p class="muted small history-state">
        The history could not be loaded. <button type="button" class="btn btn-quiet btn-compact" onclick={() => loadHistory(1)}>Try again</button>
      </p>
    {:else if history.length === 0}
      <p class="muted small history-state">
        {historyAction !== '' || historySearch.trim() !== '' ? 'No change matches. Clear the filter to see everything.' : 'No changes yet.'}
      </p>
    {:else}
      <ul class="history">
        {#each history as e (e.id)}
          <li>
            <span class="when"><MonoDigits text={formatDateTime(e.at)} /></span>
            <span class="what">{siteAuditSummary(e)}</span>
            <span class="who muted">{actorText(e.actor)}</span>
          </li>
        {/each}
      </ul>
      {#if historyMore}
        <button type="button" class="btn btn-secondary more" disabled={historyBusy} onclick={() => loadHistory(historyPage + 1)}>
          Show older changes
        </button>
      {/if}
    {/if}
  </section>
    </div>
    <aside class="page-split-aside people-aside" aria-label="On this page">
      <nav class="panel" aria-labelledby="toc-title">
        <h2 id="toc-title" class="toc-title">On this page</h2>
        <ul class="toc">
          <li><a href="#organisers-heading">Organisers</a></li>
          <li><a href="#domains-heading">Allowed email domains</a></li>
          <li><a href="#org-heading">Organisation details</a></li>
          <li><a href="#help-heading">Account help</a></li>
          <li><a href="#rollover-heading">Semester rollover</a></li>
          <li><a href="#reviewers-heading">Integrity reviewers</a></li>
          <li><a href="#history-heading">History</a></li>
        </ul>
      </nav>
    </aside>
  </div>
{/if}

<ConfirmDialog
  open={rollConfirm}
  title="Archive {countOf(rollPlan?.contests.length ?? 0, 'contest')} and disable {countOf(rollPlan?.accounts.length ?? 0, 'account')}?"
  body="For {rollPlan?.label ?? 'this term'}. Archived contests leave the lists; disabled students cannot sign in until an owner enables them again."
  items={['Nothing is deleted.', 'Organisers and the configured owner are never disabled.']}
  confirmLabel="Archive and disable"
  danger
  busy={rollBusy}
  onconfirm={applyRollover}
  oncancel={() => (rollConfirm = false)}
/>

<ConfirmDialog
  open={pending !== null}
  title={confirmCopy.title}
  body={confirmCopy.body}
  confirmLabel={confirmCopy.label}
  danger={pending?.kind !== 'enable' && pending?.kind !== 'role'}
  busy={removing}
  onconfirm={confirmAction}
  oncancel={() => (pending = null)}
/>

<style>
  .people-split {
    margin-top: var(--space-7);
  }

  /* Tables need the room: the work column takes what the jump list leaves. */
  @media (min-width: 1280px) {
    .people-split {
      grid-template-columns: minmax(0, var(--measure-wide)) minmax(0, 16rem);
    }
  }

  /* The jump list is a convenience beside a long page; below 1280 px the page reads top to bottom. */
  @media (width < 1280px) {
    .people-aside {
      display: none;
    }
  }

  .toc-title {
    font-size: var(--fs-sub);
  }

  .toc {
    display: grid;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  .toc a {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-h);
  }

  .section {
    max-width: var(--measure-wide);
  }

  .section:first-child {
    margin-top: 0;
  }

  .section + .section {
    margin-top: var(--space-7);
    padding-top: var(--space-6);
    border-top: var(--rule) solid var(--line);
  }

  h2 {
    font-size: var(--fs-section);
  }

  .subhead {
    margin-top: var(--space-6);
    font-size: var(--fs-sub);
  }

  .intro {
    max-width: var(--measure-prose);
    margin-top: var(--space-2);
  }

  .steps {
    display: grid;
    max-width: var(--measure-form);
    margin-top: var(--space-4);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .steps li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    min-height: var(--control-h-touch);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .step {
    flex: 1 1 20rem;
    min-width: 0;
  }

  .address {
    overflow-wrap: anywhere;
    font-weight: 500;
  }

  .organisers {
    margin-top: var(--space-4);
  }

  .name {
    font-weight: 500;
    white-space: nowrap;
  }

  .email {
    white-space: nowrap;
  }

  .role-select {
    width: auto;
    min-width: 7rem;
  }

  .actions {
    width: 1%;
    white-space: nowrap;
    text-align: right;
  }

  /* Wraps rather than pushing the table past the page from 768 px. */
  .locked {
    display: inline-block;
    max-width: 9rem;
    font-size: var(--fs-0);
    white-space: normal;
    text-align: left;
  }

  .cell-label {
    display: none;
  }

  /* A word in a numeric column reads in the interface face. */
  .word,
  .cell-label {
    font-family: var(--font-ui);
  }

  .invite {
    display: grid;
    gap: var(--space-2);
    max-width: var(--measure-form);
    margin-top: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--act);
    background: var(--act-tint);
  }

  .invite-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .domains {
    margin-top: var(--space-4);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
    max-width: 32rem;
  }

  .domains li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding-block: var(--space-1);
    border-bottom: var(--rule) solid var(--line);
  }

  .domain {
    overflow-wrap: anywhere;
  }

  .empty {
    margin-top: var(--space-4);
  }

  .who {
    max-width: var(--measure-form);
  }

  .form {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-3);
    margin-top: var(--space-5);
  }

  .form .grow {
    flex: 1 1 16rem;
    max-width: 24rem;
  }

  .form-error {
    margin-top: var(--space-2);
  }

  .history-state {
    margin-top: var(--space-4);
  }

  .history {
    margin-top: var(--space-4);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
    font-size: var(--fs-0);
  }

  .history li {
    display: grid;
    grid-template-columns: 11rem minmax(0, 1fr) minmax(0, 16rem);
    gap: var(--space-1) var(--space-4);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .when {
    color: var(--ink-muted);
  }

  .what,
  .who {
    overflow-wrap: anywhere;
  }

  .more {
    margin-top: var(--space-3);
  }

  .reviewers {
    margin-top: var(--space-4);
  }

  .org {
    display: grid;
    gap: var(--space-4);
    justify-items: start;
    max-width: var(--measure-form);
    margin-top: var(--space-5);
  }

  .org .field {
    width: 100%;
  }

  .plan {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
    max-width: var(--measure-form);
    margin-top: var(--space-4);
    padding: var(--space-4);
    border: var(--rule) solid var(--line-strong);
    background: var(--surface);
  }

  .plan summary {
    border-radius: var(--radius);
    font-weight: 500;
    cursor: pointer;
  }

  .plan-list {
    display: grid;
    gap: var(--space-1);
    margin-top: var(--space-2);
    padding-left: var(--space-5);
  }

  .right,
  .table .right {
    text-align: right;
  }

  .r-name {
    font-weight: 500;
  }

  .r-email {
    display: block;
    font-size: var(--fs-0);
    font-weight: 400;
  }

  .late {
    color: var(--bad);
    font-weight: 600;
  }

  /* Tablets: the sign-in time and the locked note wrap, so the table fits 720 px. */
  @media (768px <= width < 1024px) {
    .organisers .last {
      white-space: normal;
    }

    .role-select {
      min-width: 0;
    }
  }

  @media (width < 768px) {
    .form .grow {
      max-width: none;
    }

    .submit {
      width: 100%;
    }

    /* Stacked organiser rows (F7): name and role, then email, then the last sign-in and Remove. */
    .organisers tbody tr {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'name role'
        'email email'
        'last actions';
      align-items: center;
    }

    .name {
      grid-area: name;
      white-space: normal;
    }

    .role {
      grid-area: role;
    }

    .email {
      grid-area: email;
      overflow-wrap: anywhere;
      white-space: normal;
      color: var(--ink-muted);
      font-size: var(--fs-0);
    }

    .last {
      grid-area: last;
      color: var(--ink-muted);
      font-size: var(--fs-0);
    }

    .cell-label {
      display: inline;
      margin-inline-end: var(--space-1);
    }

    .last {
      white-space: normal;
    }

    .actions {
      grid-area: actions;
      width: auto;
      align-self: center;
    }

    .locked {
      white-space: normal;
    }

    .history li {
      grid-template-columns: minmax(0, 1fr);
      gap: 0;
    }

    .reviewers tbody td {
      color: var(--ink-muted);
      font-size: var(--fs-0);
      text-align: left;
    }
  }
</style>
