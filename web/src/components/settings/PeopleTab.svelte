<!--
  PeopleTab: the People tab of Settings, who can organise. A table of
  everyone with an organiser role (stacked rows on phones), with the two
  roles on screen, organiser and owner; a form to add one by email; and
  after an add, a note with the address to send them and a Copy button.
  Someone who has not signed in yet reads "Invited"; someone who has but
  gave no name (testing sign-in) reads "No name". The role is a select on
  each row that asks before it changes (the same POST upserts the role),
  and Remove asks too; the owner named in the server configuration is
  shown and cannot be changed. Successes are toasts; the form's error is
  tied to its inputs. Props: `people` (bindable), `origin` (the site's
  address) and `onforbidden` (the person is no longer an owner).
-->
<script lang="ts">
  import { tick } from 'svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { polite } from '../../lib/announce.svelte';
  import { errorText, sendJSON, ApiError } from '../../lib/api';
  import { formatDateTime, roleLabel } from '../../lib/format';
  import { personLabel, roleWord, settingsRefused, withArticle, type OrganiserRole, type Person } from '../../lib/people';
  import { runMode } from '../../lib/mode.svelte';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    people: Person[];
    origin: string;
    onforbidden: () => void;
  }

  let { people = $bindable(), origin, onforbidden }: Props = $props();

  type Pending = { kind: 'remove'; person: Person } | { kind: 'role'; person: Person; role: OrganiserRole } | null;

  let email = $state('');
  let role = $state<OrganiserRole>('admin');
  let adding = $state(false);
  let error = $state('');
  /** The organiser just added, for the "send them this address" note. */
  let invited = $state<string | null>(null);
  let pending = $state<Pending>(null);
  let busy = $state(false);
  let heading: HTMLHeadingElement | undefined = $state();

  const confirmCopy = $derived.by(() => {
    if (pending?.kind === 'remove') {
      return {
        title: `Remove ${personLabel(pending.person)} as ${roleWord(pending.person.role)}?`,
        body: 'They keep their account and become a student.',
        label: 'Remove',
      };
    }
    if (pending?.kind === 'role') {
      return {
        title: `Make ${personLabel(pending.person)} ${withArticle(pending.role)}?`,
        body:
          pending.role === 'owner'
            ? 'Owners can also change the settings: people, sign-in and the organisation.'
            : 'They keep contests and problems, and can no longer open Settings.',
        label: 'Change role',
      };
    }
    return { title: '', body: '', label: 'Remove' };
  });

  async function add(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    const value = email.trim();
    if (value === '') {
      error = 'Enter an email address.';
      document.getElementById('person-email')?.focus();
      return;
    }
    adding = true;
    try {
      const person = await sendJSON<Person>('POST', '/api/admin/people', { email: value, role });
      const index = people.findIndex((p) => p.id === person.id);
      people = index === -1 ? [...people, person] : people.map((p) => (p.id === person.id ? person : p));
      email = '';
      invited = person.last_login_at === null ? person.email : null;
      showToast(`Added ${person.email} as ${roleWord(person.role)}`);
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) error = errorText(err);
    } finally {
      adding = false;
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

  async function confirm() {
    const target = pending;
    if (!target) return;
    busy = true;
    try {
      if (target.kind === 'role') {
        const updated = await sendJSON<Person>('POST', '/api/admin/people', { email: target.person.email, role: target.role });
        people = people.map((p) => (p.id === target.person.id ? { ...p, ...updated } : p));
        showToast(`${personLabel(target.person)} is now ${withArticle(target.role)}`);
      } else {
        await sendJSON<void>('DELETE', `/api/admin/people/${target.person.id}`);
        people = people.filter((p) => p.id !== target.person.id);
        if (invited === target.person.email) invited = null;
        showToast(`Removed ${personLabel(target.person)}`);
      }
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) {
        showToast(errorText(err), 'error');
        if (target.kind === 'remove' && err instanceof ApiError && err.status === 404) people = people.filter((p) => p.id !== target.person.id);
      }
    } finally {
      busy = false;
      pending = null;
    }
    // The button that opened the dialog may be gone; start again from the heading.
    if (target.kind === 'remove') {
      await tick();
      heading?.focus();
    }
  }
</script>

<section class="section" aria-labelledby="organisers-heading">
  <h2 id="organisers-heading" tabindex="-1" bind:this={heading}>Organisers</h2>
  <p class="intro muted">Organisers write problems and run contests. Owners can also change these settings.</p>

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
              {#if person.name.trim() !== ''}{person.name}{:else if person.last_login_at === null}<span class="muted">Invited</span>{:else}<span class="muted">No name</span>{/if}
            </th>
            <td class="email">{person.email}</td>
            <td class="role">
              {#if person.locked}
                {roleLabel(person.role)}
              {:else}
                <select id="role-{person.id}" class="select role-select" aria-label="Role for {personLabel(person)}" value={person.role} onchange={(e) => askRole(e, person)}>
                  <option value="admin">Organiser</option>
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
                  onclick={() => (pending = { kind: 'remove', person })}
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

  <form class="form" novalidate onsubmit={add}>
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
        aria-invalid={error !== '' ? 'true' : undefined}
        aria-describedby={error !== '' ? 'person-error' : undefined}
      />
    </div>
    <div class="field">
      <label for="person-role">Role</label>
      <select id="person-role" class="select" bind:value={role} aria-describedby={error !== '' ? 'person-error' : undefined}>
        <option value="admin">Organiser</option>
        <option value="owner">Owner</option>
      </select>
    </div>
    <button type="submit" class="btn btn-secondary submit" disabled={adding}>Add organiser</button>
  </form>
  {#if error !== ''}
    <p id="person-error" class="field-error form-error" role="alert">{error}</p>
  {/if}
  {#if invited}
    <div class="invite" role="status">
      <p>Send {invited} this address: <span class="address">{origin}</span>. {runMode.mode === 'testing' ? 'They sign in with that email on the sign-in form.' : 'They sign in with Google using that email.'}</p>
      <div class="invite-actions">
        <button type="button" class="btn btn-secondary" onclick={copyAddress}>Copy address</button>
        <button type="button" class="btn btn-quiet btn-compact" onclick={() => (invited = null)}>Dismiss</button>
      </div>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={pending !== null}
  title={confirmCopy.title}
  body={confirmCopy.body}
  confirmLabel={confirmCopy.label}
  danger={pending?.kind === 'remove'}
  {busy}
  onconfirm={confirm}
  oncancel={() => (pending = null)}
/>

<style>
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

  .address {
    overflow-wrap: anywhere;
    font-weight: 500;
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
    /* Stacked organiser rows: name and role, then email, then the last sign-in and Remove. */
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
      white-space: normal;
    }

    .cell-label {
      display: inline;
      margin-inline-end: var(--space-1);
    }

    .actions {
      grid-area: actions;
      width: auto;
      align-self: center;
    }
  }
</style>
