<!--
  PeopleTab: the People tab of the Dashboard, the list of people who may
  sign in (V4-15). Every person is one row: students, organisers, admins
  and owners. The counts line says how many are on the list, how many
  signed in and how many never came. A filter narrows a long list. Each row
  shows the role (a select when the viewer may change it, with only the
  roles they may give: an admin sees student and organiser, V4-16) and
  Remove, which deletes someone who never signed in and disables and signs
  out someone who did; both ask first. "Add people" takes one address or a
  pasted list: Check runs a dry run and shows the three counts, then "Add
  N people" saves. Refused sign-ins list the Google addresses that tried
  and were not on the list, each with Let in. The rules are the server's
  (server/internal/auth/rules.go); this only hides what it would refuse.
  Props: `list`, `counts`, `refused` (from GET /api/admin/people),
  `viewer` (the person signed in), `origin` (the site's address),
  `onreload` (fetch the list again after a change) and `onforbidden` (the
  person lost the Dashboard).
-->
<script lang="ts">
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { polite } from '../../lib/announce.svelte';
  import { errorText, sendJSON } from '../../lib/api';
  import { countOf, formatDateTime, roleLabel } from '../../lib/format';
  import {
    mayChange,
    personLabel,
    roleWord,
    rolesFor,
    settingsRefused,
    type ImportResult,
    type ListCounts,
    type ListedPerson,
    type Refusal,
    type Viewer,
  } from '../../lib/people';
  import { runMode } from '../../lib/mode.svelte';
  import type { Role } from '../../lib/session.svelte';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    list: ListedPerson[];
    counts: ListCounts;
    refused: Refusal[];
    viewer: Viewer;
    origin: string;
    onreload: () => Promise<void>;
    onforbidden: () => void;
  }

  let { list, counts, refused, viewer, origin, onreload, onforbidden }: Props = $props();

  type Pending = { kind: 'remove'; person: ListedPerson } | { kind: 'role'; person: ListedPerson; role: Role } | null;

  let filter = $state('');
  let paste = $state('');
  let preview = $state<ImportResult | null>(null);
  let checking = $state(false);
  let adding = $state(false);
  let addError = $state('');
  let pending = $state<Pending>(null);
  let busy = $state(false);

  const shown = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    return q === '' ? list : list.filter((p) => p.email.toLowerCase().includes(q) || p.name.toLowerCase().includes(q));
  });
  const roles = $derived(rolesFor(viewer));

  const confirmCopy = $derived.by(() => {
    if (pending?.kind === 'remove') {
      const came = pending.person.last_login_at !== null;
      return {
        title: `Remove ${personLabel(pending.person)} from the list?`,
        body: came
          ? 'They are signed out on every device and cannot sign in again. Their submissions and results stay.'
          : 'They have not signed in yet, so their entry is deleted.',
        label: came ? 'Remove and sign out' : 'Remove',
      };
    }
    if (pending?.kind === 'role') {
      const to = pending.role;
      return {
        title: to === 'student' ? `Make ${personLabel(pending.person)} a student?` : `Make ${personLabel(pending.person)} an ${roleWord(to)}?`,
        body:
          to === 'student'
            ? 'They are signed out everywhere and keep only a student\'s view when they sign in again.'
            : to === 'organiser'
              ? 'They can write problems and run contests.'
              : to === 'admin'
                ? 'They can also manage students and organisers on this Dashboard.'
                : 'They can do everything, including the organisation details and other admins.',
        label: 'Change role',
      };
    }
    return { title: '', body: '', label: 'Remove' };
  });

  function askRole(event: Event, person: ListedPerson) {
    const select = event.currentTarget as HTMLSelectElement;
    const next = select.value as Role;
    // The select keeps showing the current role until the change is confirmed.
    select.value = person.role;
    if (next !== person.role) pending = { kind: 'role', person, role: next };
  }

  async function check(event: SubmitEvent) {
    event.preventDefault();
    addError = '';
    preview = null;
    if (paste.trim() === '') {
      addError = 'Enter an email address, or paste a list of them.';
      document.getElementById('people-paste')?.focus();
      return;
    }
    checking = true;
    try {
      preview = await sendJSON<ImportResult>('POST', '/api/admin/people/import', { text: paste, dry_run: true });
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) addError = errorText(err);
    } finally {
      checking = false;
    }
  }

  async function addChecked() {
    adding = true;
    addError = '';
    try {
      const done = await sendJSON<ImportResult>('POST', '/api/admin/people/import', { text: paste, dry_run: false });
      paste = '';
      preview = null;
      showToast(done.added.length === 1 ? `Added ${done.added[0]} to the list` : `Added ${countOf(done.added.length, 'person', 'people')} to the list`);
      await onreload();
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) addError = errorText(err);
    } finally {
      adding = false;
    }
  }

  async function letIn(r: Refusal) {
    try {
      await sendJSON<void>('POST', '/api/admin/people/let-in', { email: r.email });
      showToast(`Let ${r.email} in. They can sign in now.`);
      await onreload();
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) showToast(errorText(err), 'error');
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

  async function confirm() {
    const target = pending;
    if (!target) return;
    busy = true;
    try {
      if (target.kind === 'remove') {
        await sendJSON<{ deleted: boolean }>('POST', '/api/admin/people/remove', { email: target.person.email });
        showToast(`Removed ${personLabel(target.person)} from the list`);
      } else if (target.role === 'student') {
        await sendJSON<void>('DELETE', `/api/admin/people/${target.person.id}`);
        showToast(`${personLabel(target.person)} is now a student`);
      } else {
        await sendJSON<unknown>('POST', '/api/admin/people', { email: target.person.email, role: target.role });
        showToast(`${personLabel(target.person)} is now an ${roleWord(target.role)}`);
      }
      await onreload();
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) showToast(errorText(err), 'error');
    } finally {
      busy = false;
      pending = null;
    }
  }
</script>

<section class="section" aria-labelledby="list-heading">
  <h2 id="list-heading" tabindex="-1">The list</h2>
  <p class="intro muted">Only people on this list can sign in: students, organisers, admins and owners alike.</p>
  <p class="counts" aria-live="polite">
    <MonoDigits text={`${countOf(counts.on_list, 'person', 'people')} on the list, ${counts.signed_in} signed in, ${counts.never_came} never came.`} />
  </p>

  <div class="field filter">
    <label for="people-filter">Find a person</label>
    <input id="people-filter" class="input" type="search" autocomplete="off" spellcheck="false" bind:value={filter} />
  </div>

  <div class="table-wrap people">
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
        {#each shown as person (person.id)}
          {@const changeable = mayChange(viewer, person)}
          <tr>
            <th scope="row" class="name">
              {#if person.name.trim() !== ''}{person.name}{:else if person.last_login_at === null}<span class="muted">Not signed in yet</span>{:else}<span class="muted">No name</span>{/if}
            </th>
            <td class="email">{person.email}</td>
            <td class="role">
              {#if changeable && roles.includes(person.role)}
                <select class="select role-select" aria-label="Role for {personLabel(person)}" value={person.role} onchange={(e) => askRole(e, person)}>
                  {#each roles as r (r)}<option value={r}>{roleLabel(r)}</option>{/each}
                </select>
              {:else}
                {roleLabel(person.role)}
              {/if}
              {#if person.disabled}<span class="muted">, disabled</span>{/if}
            </td>
            <td class="num last">
              <span class="cell-label">Last sign-in</span>{#if person.last_login_at !== null}<MonoDigits text={formatDateTime(person.last_login_at)} />{:else}<span class="muted word">Never</span>{/if}
            </td>
            <td class="actions">
              {#if person.locked}
                <span class="muted locked">Set in server configuration</span>
              {:else if changeable && !person.disabled}
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
            <td colspan="5" class="muted">{filter.trim() === '' ? 'Nobody is on the list yet. Add people below.' : 'Nobody matches. Clear the search to see everyone.'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</section>

<section class="section" aria-labelledby="add-heading">
  <h2 id="add-heading">Add people</h2>
  <p class="intro muted">
    One email address, or a list pasted from a form or a spreadsheet: commas, spaces and new lines all separate addresses. Everyone added
    is a student; give a role on the list above.
  </p>
  <form class="add" novalidate onsubmit={check}>
    <div class="field">
      <label for="people-paste">Email addresses</label>
      <textarea
        id="people-paste"
        class="input paste"
        rows="4"
        spellcheck="false"
        bind:value={paste}
        oninput={() => (preview = null)}
        aria-required="true"
        aria-invalid={addError !== '' ? 'true' : undefined}
        aria-describedby={addError !== '' ? 'people-add-error' : undefined}
      ></textarea>
    </div>
    <button type="submit" class="btn btn-secondary submit" disabled={checking}>Check the addresses</button>
  </form>
  {#if addError !== ''}
    <p id="people-add-error" class="field-error form-error" role="alert">{addError}</p>
  {/if}
  {#if preview}
    <div class="preview" role="status">
      <p>
        <MonoDigits
          text={`${countOf(preview.added.length, 'new address', 'new addresses')}, ${preview.duplicates.length} already on the list, ${countOf(preview.invalid.length, 'is not an address', 'are not addresses')}.`}
        />
      </p>
      {#if preview.invalid.length > 0}<p class="muted small">Not an address: {preview.invalid.join(', ')}</p>{/if}
      {#if preview.duplicates.length > 0}<p class="muted small">Already on the list: {preview.duplicates.join(', ')}</p>{/if}
      {#if preview.added.length > 0}
        <button type="button" class="btn btn-primary" disabled={adding} onclick={addChecked}>
          Add {countOf(preview.added.length, 'person', 'people')}
        </button>
        <p class="muted small">
          Then send them this address: <span class="address">{origin}</span>.
          {runMode.mode === 'testing' ? 'They sign in with their email on the sign-in form.' : 'They sign in with Google using the email you added.'}
          <button type="button" class="btn btn-quiet btn-compact" onclick={copyAddress}>Copy address</button>
        </p>
      {/if}
    </div>
  {/if}
</section>

<section class="section" aria-labelledby="refused-heading">
  <h2 id="refused-heading">Refused sign-ins</h2>
  <p class="intro muted">Google accounts that tried to sign in and are not on the list, newest first. The most recent 500 are kept.</p>
  {#if refused.length > 0}
    <ul class="refused">
      {#each refused as r (r.email)}
        <li>
          <span class="item">
            {r.email}
            <span class="muted small"><MonoDigits text={`${countOf(r.count, 'time')}, last ${formatDateTime(r.last_at)}`} /></span>
          </span>
          <button type="button" class="btn btn-secondary btn-compact" aria-label="Let {r.email} in" onclick={() => letIn(r)}>Let in</button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="empty muted">Nobody has been refused.</p>
  {/if}
</section>

<ConfirmDialog
  open={pending !== null}
  title={confirmCopy.title}
  body={confirmCopy.body}
  confirmLabel={confirmCopy.label}
  danger={pending?.kind === 'remove' || (pending?.kind === 'role' && pending.role === 'student')}
  {busy}
  onconfirm={confirm}
  oncancel={() => (pending = null)}
/>

<style>
  .counts {
    margin-top: var(--space-3);
  }

  .filter {
    max-width: 24rem;
    margin-top: var(--space-4);
  }

  .people {
    margin-top: var(--space-3);
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

  .add {
    display: grid;
    gap: var(--space-3);
    max-width: var(--measure-form);
    margin-top: var(--space-4);
  }

  .add .submit {
    justify-self: start;
  }

  .paste {
    min-height: 6rem;
    resize: vertical;
  }

  .preview {
    display: grid;
    gap: var(--space-2);
    justify-items: start;
    max-width: var(--measure-form);
    margin-top: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border: var(--rule) solid var(--act);
    background: var(--act-tint);
  }

  .address {
    overflow-wrap: anywhere;
    font-weight: 500;
  }

  .refused {
    max-width: var(--measure-narrow);
    margin-top: var(--space-3);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .refused li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding-block: var(--space-1);
    border-bottom: var(--rule) solid var(--line);
  }

  .item {
    display: grid;
    overflow-wrap: anywhere;
  }

  .empty {
    margin-top: var(--space-3);
  }

  /* Tablets: the sign-in time and the locked note wrap, so the table fits 720 px. */
  @media (768px <= width < 1024px) {
    .people .last {
      white-space: normal;
    }

    .role-select {
      min-width: 0;
    }
  }

  @media (width < 768px) {
    /* Stacked rows: name and role, then email, then the last sign-in and Remove. */
    .people tbody tr {
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
