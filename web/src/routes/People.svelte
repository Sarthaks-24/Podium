<!--
  People: the owner's page for organisers, allowed email domains and Google
  account links (/api/admin/people, /api/admin/domains and
  /api/admin/accounts/reset-google). The first section is a table of
  everyone with the admin or owner role and a form to add one; the second
  lists the domains whose addresses may sign in and a form to add one; the
  third resets the Google account tied to an email. Every removal and every
  reset asks first in a ConfirmDialog. Successes are toasts; form errors
  appear under their form and are tied to the inputs with aria-describedby;
  removal errors are error toasts. Anyone who is not an owner sees a short
  explanation instead. Takes no props.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import { ApiError, getJSON, sendJSON } from '../lib/api';
  import { formatDateTime, roleLabel } from '../lib/format';
  import { setTitle } from '../lib/head';
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
  }

  interface Domain {
    domain: string;
    created_at: number;
  }

  type Pending =
    | { kind: 'person'; person: Person }
    | { kind: 'domain'; domain: string }
    | { kind: 'reset'; email: string }
    | null;

  let loadState = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  let loadError = $state('');
  let people = $state<Person[]>([]);
  let domains = $state<Domain[]>([]);

  let email = $state('');
  let role = $state<OrganiserRole>('admin');
  let addingPerson = $state(false);
  let personError = $state('');

  let domainInput = $state('');
  let addingDomain = $state(false);
  let domainError = $state('');

  let resetEmail = $state('');
  let resetError = $state('');

  let pending = $state<Pending>(null);
  let removing = $state(false);

  let organisersHeading: HTMLHeadingElement | undefined = $state();
  let domainsHeading: HTMLHeadingElement | undefined = $state();

  const isOwner = $derived(session.user?.role === 'owner');

  const confirmCopy = $derived.by(() => {
    if (pending?.kind === 'person') {
      const who = pending.person.name.trim() !== '' ? pending.person.name : pending.person.email;
      return {
        title: `Remove ${who} as ${pending.person.role}?`,
        body: 'They keep their account and become a student.',
        label: 'Remove',
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
        label: 'Reset link',
      };
    }
    return { title: '', body: '' };
  });

  function personLabel(p: Person): string {
    return p.name.trim() !== '' ? p.name : p.email;
  }

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
      const data = await getJSON<{ people: Person[]; domains: Domain[] }>('/api/admin/people');
      people = data.people ?? [];
      domains = data.domains ?? [];
      loadState = 'ready';
    } catch (err) {
      if (handleSessionError(err)) return;
      loadError = messageOf(err);
      loadState = 'error';
    }
  }

  onMount(() => {
    setTitle('People');
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
      return;
    }
    addingPerson = true;
    try {
      const person = await sendJSON<Person>('POST', '/api/admin/people', { email: value, role });
      const index = people.findIndex((p) => p.id === person.id);
      people = index === -1 ? [...people, person] : people.map((p) => (p.id === person.id ? person : p));
      email = '';
      showToast(`Added ${person.email} as ${person.role}`);
    } catch (err) {
      if (!handleSessionError(err)) personError = messageOf(err);
    } finally {
      addingPerson = false;
    }
  }

  async function addDomain(event: SubmitEvent) {
    event.preventDefault();
    domainError = '';
    const value = domainInput.trim();
    if (value === '' || value === '@') {
      domainError = 'Enter a domain, for example college.edu.';
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
      }
    } catch (err) {
      if (!handleSessionError(err)) domainError = messageOf(err);
    } finally {
      addingDomain = false;
    }
  }

  function askReset(event: SubmitEvent) {
    event.preventDefault();
    resetError = '';
    const value = resetEmail.trim();
    if (value === '') {
      resetError = 'Enter an email address.';
      return;
    }
    pending = { kind: 'reset', email: value };
  }

  async function confirmReset(target: string) {
    removing = true;
    try {
      await sendJSON<void>('POST', '/api/admin/accounts/reset-google', { email: target });
      resetEmail = '';
      showToast(`Reset the Google account link for ${target}`);
    } catch (err) {
      if (!handleSessionError(err)) resetError = messageOf(err);
    } finally {
      removing = false;
      pending = null;
    }
  }

  async function confirmAction() {
    const target = pending;
    if (!target) return;
    if (target.kind === 'reset') return confirmReset(target.email);
    removing = true;
    try {
      if (target.kind === 'person') {
        await sendJSON<void>('DELETE', `/api/admin/people/${target.person.id}`);
        people = people.filter((p) => p.id !== target.person.id);
        showToast('Removed');
      } else {
        await sendJSON<void>('DELETE', `/api/admin/domains/${encodeURIComponent(target.domain)}`);
        domains = domains.filter((d) => d.domain !== target.domain);
        showToast(`Removed @${target.domain}`);
      }
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

<h1 tabindex="-1">People</h1>

{#if loadState === 'forbidden'}
  <p class="lead">Only owners can manage people. Ask an owner if you need access.</p>
{:else if loadState === 'loading'}
  <p class="lead muted" role="status">Loading people</p>
{:else if loadState === 'error'}
  <div class="banner banner-error lead" role="alert">
    <p>{loadError}</p>
  </div>
  <p class="retry"><button type="button" class="btn btn-secondary" onclick={load}>Try again</button></p>
{:else}
  <section class="section" aria-labelledby="organisers-heading">
    <h2 id="organisers-heading" tabindex="-1" bind:this={organisersHeading}>Organisers</h2>
    <p class="intro muted">Admins write problems and run contests. Owners can also manage people.</p>

    <div class="table-wrap organisers">
      <table class="table">
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
                {#if person.name.trim() !== ''}{person.name}{:else}<span class="muted">Not signed in yet</span>{/if}
              </th>
              <td class="email">{person.email}</td>
              <td>{roleLabel(person.role)}</td>
              <td class="num">
                {#if person.last_login_at !== null}{formatDateTime(person.last_login_at)}{:else}<span class="muted word">Never</span>{/if}
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
        <select
          id="person-role"
          class="select"
          bind:value={role}
          aria-describedby={personError !== '' ? 'person-error' : undefined}
        >
          <option value="admin">Admin</option>
          <option value="owner">Owner</option>
        </select>
      </div>
      <button type="submit" class="btn btn-primary submit" disabled={addingPerson}>Add organiser</button>
    </form>
    {#if personError !== ''}
      <p id="person-error" class="field-error form-error" role="alert">{personError}</p>
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
              class="btn btn-quiet btn-compact"
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

  <section class="section" aria-labelledby="links-heading">
    <h2 id="links-heading">Google account links</h2>
    <p class="intro muted">
      Each account is tied to the Google account that first signed in with it. If someone's address moved to a new
      Google account, reset the link so their next sign-in is accepted.
    </p>

    <form class="form" novalidate onsubmit={askReset}>
      <div class="field grow">
        <label for="reset-email">Email</label>
        <input
          id="reset-email"
          class="input"
          type="email"
          autocomplete="off"
          spellcheck="false"
          bind:value={resetEmail}
          aria-invalid={resetError !== '' ? 'true' : undefined}
          aria-describedby={resetError !== '' ? 'reset-error' : undefined}
        />
      </div>
      <button type="submit" class="btn btn-primary submit" disabled={removing && pending?.kind === 'reset'}>
        Reset link
      </button>
    </form>
    {#if resetError !== ''}
      <p id="reset-error" class="field-error form-error" role="alert">{resetError}</p>
    {/if}
  </section>
{/if}

<ConfirmDialog
  open={pending !== null}
  title={confirmCopy.title}
  body={confirmCopy.body}
  confirmLabel={confirmCopy.label ?? 'Remove'}
  danger
  busy={removing}
  onconfirm={confirmAction}
  oncancel={() => (pending = null)}
/>

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: 40rem;
  }

  .retry {
    margin-top: var(--space-4);
  }

  .section {
    max-width: 64rem;
    margin-top: var(--space-7);
  }

  .section + .section {
    margin-top: var(--space-7);
    padding-top: var(--space-6);
    border-top: var(--rule) solid var(--line);
  }

  h2 {
    font-size: var(--fs-3);
  }

  .intro {
    max-width: 72ch;
    margin-top: var(--space-2);
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

  .actions {
    width: 1%;
    white-space: nowrap;
    text-align: right;
  }

  .locked {
    font-size: var(--fs-0);
  }

  /* A word in a numeric column reads in the interface face. */
  .word {
    font-family: var(--font-ui);
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

  @media (max-width: 767px) {
    .form .grow {
      max-width: none;
    }

    .submit {
      width: 100%;
    }
  }
</style>
