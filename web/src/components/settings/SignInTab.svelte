<!--
  SignInTab: the Sign-in tab of Settings, help for one account (who may
  sign in is the list of people, V4-15). Account help: one email field for "Reset
  Google link", "End sessions" and "Disable account", then the disabled
  accounts with Enable. As an address is typed the field looks the account
  up (GET /api/admin/accounts?email=, students included) and says whose it
  is ("Asha Rao, student, last signed in ..."); until it has named one, or
  when there is none, the three actions stay disabled, and each
  confirmation names the person. Successes are toasts; form errors sit
  under their form, tied to the inputs. Props: `disabled` and
  `people` (all bindable; a disable or enable marks the organiser's row
  too) and `onforbidden` (the person is no longer an owner).
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../../lib/api';
  import { countOf, formatDateTime } from '../../lib/format';
  import { EMAIL, personLabel, roleWord, settingsRefused, type Account, type Person } from '../../lib/people';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    disabled: Account[];
    people: Person[];
    onforbidden: () => void;
  }

  let { disabled = $bindable(), people = $bindable(), onforbidden }: Props = $props();

  type AccountAction = 'reset' | 'end-sessions' | 'disable' | 'enable';
  type Pending = { kind: AccountAction; email: string } | null;

  let accountEmail = $state('');
  let accountError = $state('');
  let pending = $state<Pending>(null);
  let busy = $state(false);

  // The account the help field names, looked up as the address is typed.
  let lookup = $state<{ email: string; status: 'loading' | 'found' | 'none' | 'error'; account?: Account } | null>(null);
  let lookupTimer: ReturnType<typeof setTimeout> | undefined;

  /** The account the help field names: looked up, or already on this page. */
  const accountMatch = $derived.by(() => {
    const e = accountEmail.trim().toLowerCase();
    if (e === '') return null;
    if (lookup && lookup.email === e && lookup.status === 'found' && lookup.account) {
      const a = lookup.account;
      return { name: a.name.trim() || a.email, role: roleWord(a.role), last: a.last_login_at, disabled: a.disabled };
    }
    const person = people.find((x) => x.email.toLowerCase() === e);
    if (person) return { name: personLabel(person), role: roleWord(person.role), last: person.last_login_at, disabled: !!person.disabled };
    const account = disabled.find((x) => x.email.toLowerCase() === e);
    if (account) return { name: account.name.trim() || account.email, role: roleWord(account.role), last: account.last_login_at, disabled: true };
    return null;
  });
  const accountLooksLikeEmail = $derived(EMAIL.test(accountEmail.trim()));
  /** The lookup found no account for the address typed: the actions would only be refused. */
  const noAccount = $derived(lookup !== null && lookup.email === accountEmail.trim().toLowerCase() && lookup.status === 'none');
  /** An address typed but not yet named above (lookup running, failed or found nothing): never act on it. */
  const unseen = $derived(accountEmail.trim() !== '' && accountMatch === null);

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
      if (settingsRefused(err, onforbidden) || lookup?.email !== e) return;
      lookup = { email: e, status: err instanceof ApiError && err.status === 404 ? 'none' : 'error' };
    }
  }

  /** "Asha Verma (asha@college.edu)" when the page knows the name, else the address. */
  function accountName(target: string): string {
    const m = accountMatch;
    return m && m.name !== target ? `${m.name} (${target})` : target;
  }

  const confirmCopy = $derived.by(() => {
    switch (pending?.kind) {
      case 'reset':
        return {
          title: `Reset the Google account link for ${accountName(pending.email)}?`,
          body: 'Their current sessions end. The next Google account that signs in with this address is linked to it.',
          label: 'Reset Google link',
        };
      case 'end-sessions':
        return {
          title: `End every session of ${accountName(pending.email)}?`,
          body: 'They are signed out on every device at once, open contest pages included. They can sign in again.',
          label: 'End sessions',
        };
      case 'disable':
        return {
          title: `Disable ${accountName(pending.email)}?`,
          body: 'They are signed out on every device at once and cannot sign in until an owner enables the account again.',
          label: 'Disable account',
        };
      case 'enable':
        return { title: `Enable ${pending.email}?`, body: 'They can sign in again with their Google account.', label: 'Enable account' };
    }
    return { title: '', body: '', label: 'Remove' };
  });

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

  async function runAccount(kind: AccountAction, target: string) {
    try {
      if (kind === 'reset') {
        await sendJSON<void>('POST', '/api/admin/accounts/reset-google', { email: target });
        accountEmail = '';
        showToast(`Reset the Google account link for ${target}`);
        return;
      }
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
    } catch (err) {
      if (settingsRefused(err, onforbidden)) return;
      if (kind === 'enable') showToast(errorText(err), 'error');
      else accountError = errorText(err);
    }
  }

  async function confirm() {
    const target = pending;
    if (!target) return;
    busy = true;
    try {
      await runAccount(target.kind, target.email);
    } finally {
      busy = false;
      pending = null;
    }
  }
</script>

<section class="section" aria-labelledby="help-heading">
  <h2 id="help-heading">Account help</h2>
  <p class="intro muted">
    Reset Google link when someone's address moved to a new Google account, so their next sign-in is accepted. End sessions signs
    someone out on every device, for a lost laptop or a lab PC left signed in. Disable account also stops them signing in until you
    enable it again.
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
    <button type="submit" class="btn btn-secondary submit" disabled={unseen || (busy && pending?.kind === 'reset')} aria-describedby={unseen ? 'account-who' : undefined}>Reset Google link</button>
    <button
      type="button"
      class="btn btn-secondary submit"
      disabled={unseen || (busy && pending?.kind === 'end-sessions')}
      aria-describedby={unseen ? 'account-who' : undefined}
      onclick={() => askAccount('end-sessions')}
    >
      End sessions
    </button>
    <button
      type="button"
      class="btn btn-secondary btn-danger-secondary submit"
      disabled={unseen || (busy && pending?.kind === 'disable')}
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
    <ul class="list">
      {#each disabled as a (a.id)}
        <li>
          <span class="item">{a.email}{#if a.name.trim() !== ''}<span class="muted"> ({a.name})</span>{/if}</span>
          <button type="button" class="btn btn-quiet btn-compact" aria-label="Enable {a.email}" onclick={() => (pending = { kind: 'enable', email: a.email })}>
            Enable
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="empty muted">No account is disabled.</p>
  {/if}
</section>

<ConfirmDialog
  open={pending !== null}
  title={confirmCopy.title}
  body={confirmCopy.body}
  confirmLabel={confirmCopy.label}
  danger={pending?.kind !== 'enable'}
  {busy}
  onconfirm={confirm}
  oncancel={() => (pending = null)}
/>

<style>
  .list {
    max-width: 32rem;
    margin-top: var(--space-3);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding-block: var(--space-1);
    border-bottom: var(--rule) solid var(--line);
  }

  .item {
    overflow-wrap: anywhere;
  }

  .empty {
    margin-top: var(--space-3);
  }

  .who {
    max-width: var(--measure-form);
  }

  .subhead {
    margin-top: var(--space-5);
    font-size: var(--fs-sub);
  }
</style>
