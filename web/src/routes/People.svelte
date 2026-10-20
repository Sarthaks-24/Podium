<!--
  People: the owner's page for organisers, allowed email domains, account
  help and the site's history (v2 O1 and F10.1; /api/admin/people,
  /api/admin/domains, /api/admin/accounts/* and /api/admin/site-audit).

  - On a fresh install (no domain yet, or nobody but the configured owner),
    a short checklist comes first: add the college's domain, add a
    co-teacher, share the address.
  - Organisers: a table of everyone with the admin or owner role (stacked
    rows on phones), a form to add one, and after an add an inline note
    with the address to send them and a Copy button. Someone who has not
    signed in yet reads "Invited". A role is a select on each row that
    asks before it changes (the same POST upserts the role).
  - Allowed email domains: the list and a form to add one.
  - Account help: one email field for "Reset Google link", "End sessions"
    and "Disable account", then the disabled accounts with Enable.
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
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import { polite } from '../lib/announce.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { countOf, formatDateTime, roleLabel } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { SITE_ACTIONS, actorText, feedQuery, siteAuditSummary, type SiteAuditEntry, type SiteAuditPage } from '../lib/history';
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

  let organisersHeading: HTMLHeadingElement | undefined = $state();
  let domainsHeading: HTMLHeadingElement | undefined = $state();

  const isOwner = $derived(session.user?.role === 'owner');
  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  const coTeachers = $derived(people.filter((p) => !p.locked).length);
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
        title: `Reset the Google account link for ${pending.email}?`,
        body: 'Their current sessions end. The next Google account that signs in with this address is linked to it.',
        label: 'Reset Google link',
      };
    }
    if (pending?.kind === 'end-sessions') {
      return {
        title: `End every session of ${pending.email}?`,
        body: 'They are signed out on every device at once, open contest pages included. They can sign in again.',
        label: 'End sessions',
      };
    }
    if (pending?.kind === 'disable') {
      return {
        title: `Disable ${pending.email}?`,
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

  function messageOf(err: unknown): string {
    return err instanceof ApiError ? err.message : 'Something went wrong. Try again in a moment.';
  }

  async function load() {
    loadState = 'loading';
    try {
      const data = await getJSON<{ people: Person[]; domains: Domain[]; disabled?: Account[] }>('/api/admin/people');
      people = data.people ?? [];
      domains = data.domains ?? [];
      disabled = data.disabled ?? [];
      loadState = 'ready';
      void loadHistory(1);
    } catch (err) {
      if (handleSessionError(err)) return;
      loadError = messageOf(err);
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
      else showToast(messageOf(err), 'error');
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
      if (!handleSessionError(err)) personError = messageOf(err);
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
      if (!handleSessionError(err)) showToast(messageOf(err), 'error');
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
      if (!handleSessionError(err)) domainError = messageOf(err);
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
      if (!handleSessionError(err)) accountError = messageOf(err);
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
        if (kind === 'enable') showToast(messageOf(err), 'error');
        else accountError = messageOf(err);
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
        showToast(messageOf(err), 'error');
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
  {#if fresh}
    <section class="section start" aria-labelledby="start-heading">
      <h2 id="start-heading">Getting started</h2>
      <ul class="steps">
        <li>
          <span class="step">Add your college's email domain, so students can sign in.</span>
          <span class="state-word" data-state={domains.length > 0 ? 'ended' : 'upcoming'}>{domains.length > 0 ? 'Done' : 'To do'}</span>
        </li>
        <li>
          <span class="step">Add a co-teacher as an admin, so someone else can run a contest.</span>
          <span class="state-word" data-state={coTeachers > 0 ? 'ended' : 'upcoming'}>{coTeachers > 0 ? 'Done' : 'To do'}</span>
        </li>
        <li>
          <span class="step">Share the address with your students: <span class="address">{origin}</span></span>
          <button type="button" class="btn btn-secondary btn-compact" onclick={copyAddress}>Copy address</button>
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
                <span class="cell-label">Last sign-in</span>{#if person.last_login_at !== null}{formatDateTime(person.last_login_at)}{:else}<span class="muted word">Never</span>{/if}
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
          <button type="button" class="btn btn-secondary btn-compact" onclick={copyAddress}>Copy address</button>
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
          class="input"
          type="email"
          autocomplete="off"
          spellcheck="false"
          bind:value={accountEmail}
          aria-invalid={accountError !== '' ? 'true' : undefined}
          aria-describedby={accountError !== '' ? 'account-error' : undefined}
        />
      </div>
      <button type="submit" class="btn btn-secondary submit" disabled={removing && pending?.kind === 'reset'}>Reset Google link</button>
      <button type="button" class="btn btn-secondary submit" disabled={removing && pending?.kind === 'end-sessions'} onclick={() => askAccount('end-sessions')}>
        End sessions
      </button>
      <button type="button" class="btn btn-danger submit" disabled={removing && pending?.kind === 'disable'} onclick={() => askAccount('disable')}>
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
            <span class="mono when">{formatDateTime(e.at)}</span>
            <span class="what">{siteAuditSummary(e)}</span>
            <span class="who muted">{actorText(e.actor)}</span>
          </li>
        {/each}
      </ul>
      {#if historyMore}
        <button type="button" class="btn btn-secondary btn-compact more" disabled={historyBusy} onclick={() => loadHistory(historyPage + 1)}>
          Show older changes
        </button>
      {/if}
    {/if}
  </section>
{/if}

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
  .section {
    max-width: var(--measure-wide);
    margin-top: var(--space-7);
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

  .small {
    font-size: var(--fs-0);
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
  }
</style>
