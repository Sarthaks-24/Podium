<!--
  KeepSignedIn: the "Staying signed in" section of the Sign-in tab, owner
  only. One radio group for each role (students, organisers, admins, the
  owner): Nobody (signed out when the browser closes or after 12 hours, the
  default), Each person chooses (a checkbox on the sign-in page, or in the
  account menu for organisers and above) and Everyone (every sign-in is
  remembered). Below, "Sign out after": a number of idle days from 1 to 365,
  or only when the person signs out; it is disabled while every role is on
  Nobody. A shared-computer banner shows for Everyone and for "only when
  they sign out". The setting is read and saved with GET and PUT
  /api/admin/keep-signed-in; a save that switches a role to Nobody asks
  first and says how many people it signs out, the owner included when the
  owner's own session is one of them. A refused field says what to fix
  under itself. Props: `onforbidden` (the person is no longer an owner).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import { ApiError, errorText, getJSON, sendJSON } from '../../lib/api';
  import { focusFirstInvalid } from '../../lib/focus';
  import {
    ADMIN_EVERYONE_WARNING,
    anyKept,
    DAYS_ERROR,
    KEEP_ROLES,
    savedToast,
    SHARED_COMPUTER_WARNING,
    showsSharedWarning,
    signOutConfirm,
    validDays,
    type KeepPolicy,
    type KeepRole,
    type KeepSettings,
  } from '../../lib/keep';
  import { settingsRefused } from '../../lib/people';
  import { showToast } from '../../lib/toast.svelte';

  let { onforbidden }: { onforbidden: () => void } = $props();

  const MODES = [
    { value: 'nobody', label: 'Nobody' },
    { value: 'choose', label: 'Each person chooses' },
    { value: 'everyone', label: 'Everyone' },
  ] as const;

  let status = $state<'loading' | 'ready' | 'error'>('loading');
  let loadError = $state('');
  let saved = $state<KeepPolicy | null>(null);
  let draft = $state<KeepPolicy | null>(null);
  let counts = $state<Record<KeepRole, number>>({ students: 0, organisers: 0, admins: 0, owner: 0 });
  let youRemembered = $state(false);
  let daysText = $state('30');
  let errors = $state<Record<string, string>>({});
  let formError = $state('');
  let busy = $state(false);
  let pending = $state<{ body: KeepPolicy; copy: { title: string; body: string; label: string } } | null>(null);

  const used = $derived(draft !== null && anyKept(draft));

  async function load() {
    status = 'loading';
    try {
      const s = await getJSON<KeepSettings>('/api/admin/keep-signed-in');
      saved = s.policy;
      draft = { ...s.policy };
      daysText = String(s.policy.days);
      counts = s.remembered;
      youRemembered = s.you_remembered;
      status = 'ready';
    } catch (err) {
      if (settingsRefused(err, onforbidden)) return;
      loadError = errorText(err);
      status = 'error';
    }
  }

  onMount(() => void load());

  /** What the form would save, or null when a field is wrong. */
  function body(): KeepPolicy | null {
    if (!draft || !saved) return null;
    errors = {};
    if (used && !draft.until_sign_out && !validDays(daysText)) {
      errors = { days: DAYS_ERROR };
      return null;
    }
    // The days are not used while nobody is kept or while "only when they sign out" is chosen.
    const days = used && !draft.until_sign_out ? Number(daysText) : saved.days;
    return { ...draft, days };
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = '';
    const next = body();
    if (!next || !saved) {
      await focusFirstInvalid('#keep-form');
      return;
    }
    const copy = signOutConfirm(saved, next, counts, youRemembered);
    if (copy) pending = { body: next, copy };
    else await save(next);
  }

  async function save(next: KeepPolicy) {
    busy = true;
    try {
      const out = await sendJSON<{ policy: KeepPolicy; signed_out: number }>('PUT', '/api/admin/keep-signed-in', next);
      saved = out.policy;
      draft = { ...out.policy };
      daysText = String(out.policy.days);
      showToast(savedToast(out.signed_out));
      // The counts changed with the save; the owner's own session may have ended with it.
      const s = await getJSON<KeepSettings>('/api/admin/keep-signed-in').catch(() => null);
      if (s) {
        counts = s.remembered;
        youRemembered = s.you_remembered;
      }
    } catch (err) {
      if (settingsRefused(err, onforbidden)) return;
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        errors = { ...err.fields };
        await focusFirstInvalid('#keep-form');
      } else {
        formError = errorText(err);
      }
    } finally {
      busy = false;
      pending = null;
    }
  }
</script>

<section class="section" aria-labelledby="keep-heading">
  <h2 id="keep-heading">Staying signed in</h2>
  <p class="intro muted">
    Choose, for each role, whether people stay signed in on a device after the browser closes. Nothing changes until you save, and
    Nobody is how Podium has always worked.
  </p>

  {#if status === 'loading'}
    <p class="muted status" role="status">Loading the setting</p>
  {:else if status === 'error' || !draft}
    <p class="field-error status" role="alert">{loadError}</p>
    <button type="button" class="btn btn-secondary" onclick={() => void load()}>Try again</button>
  {:else}
    <form id="keep-form" class="keep" novalidate onsubmit={submit}>
      <h3 class="subhead">Keep people signed in</h3>
      <div class="roles">
        {#each KEEP_ROLES as r (r.key)}
          <fieldset class="role">
            <legend>{r.label}</legend>
            <div class="options">
              {#each MODES as m (m.value)}
                <label class="choice"><input type="radio" name="keep-{r.key}" value={m.value} bind:group={draft[r.key]} /> {m.label}</label>
              {/each}
            </div>
            {#if (r.key === 'admins' || r.key === 'owner') && draft[r.key] === 'everyone'}
              <p class="field-help">{ADMIN_EVERYONE_WARNING}</p>
            {/if}
          </fieldset>
        {/each}
      </div>
      <ul class="field-help modes">
        <li>Nobody: signed out when the browser closes, or after 12 hours.</li>
        <li>Each person chooses: a checkbox on the sign-in page.</li>
        <li>Everyone: every sign-in is remembered.</li>
      </ul>

      <fieldset class="after" disabled={!used}>
        <legend>Sign out after</legend>
        {#if !used}<p class="field-help">Not used while nobody is kept signed in.</p>{/if}
        <label class="choice">
          <input type="radio" name="keep-after" checked={!draft.until_sign_out} onchange={() => (draft!.until_sign_out = false)} />
          A number of days without use
        </label>
        <div class="field days">
          <label for="keep-days">Days</label>
          <input
            id="keep-days"
            class="input"
            type="text"
            inputmode="numeric"
            autocomplete="off"
            bind:value={daysText}
            disabled={draft.until_sign_out}
            aria-invalid={errors.days ? 'true' : undefined}
            aria-describedby="keep-days-help{errors.days ? ' keep-days-error' : ''}"
          />
          <p id="keep-days-help" class="field-help">1 to 365.</p>
          {#if errors.days}<p id="keep-days-error" class="field-error">{errors.days}</p>{/if}
        </div>
        <label class="choice">
          <input type="radio" name="keep-after" checked={draft.until_sign_out} onchange={() => (draft!.until_sign_out = true)} />
          Only when they sign out
        </label>
        <p class="field-help">People stay signed in until they press Sign out. A browser forgets the sign-in after about 13 months without a visit.</p>
      </fieldset>

      {#if showsSharedWarning(draft)}
        <p class="banner banner-info warning">{SHARED_COMPUTER_WARNING}</p>
      {/if}
      {#if formError}<p class="field-error" role="alert">{formError}</p>{/if}
      <button type="submit" class="btn btn-secondary" disabled={busy}>Save</button>
    </form>
  {/if}

  <ConfirmDialog
    open={pending !== null}
    title={pending?.copy.title ?? ''}
    body={pending?.copy.body ?? ''}
    confirmLabel={pending?.copy.label ?? 'Save'}
    danger
    {busy}
    onconfirm={() => pending && void save(pending.body)}
    oncancel={() => (pending = null)}
  />
</section>

<style>
  .status {
    margin-top: var(--space-3);
  }

  .keep {
    display: grid;
    gap: var(--space-4);
    justify-items: start;
    max-width: var(--measure-prose);
    margin-top: var(--space-4);
  }

  .subhead {
    font-size: var(--fs-sub);
  }

  .roles {
    display: grid;
    width: 100%;
    border-top: var(--rule) solid var(--line);
  }

  fieldset {
    min-width: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }

  .role {
    padding-block: var(--space-1);
    border-bottom: var(--rule) solid var(--line);
  }

  legend {
    padding: 0;
    font-weight: 500;
  }

  .options {
    display: grid;
  }

  .choice {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-weight: 400;
    cursor: pointer;
  }

  .choice input {
    flex: none;
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  /* From 768 px a role is one row: its name, then the three choices in columns. */
  @media (min-width: 768px) {
    .role legend {
      float: left;
      width: 8rem;
      line-height: var(--control-h);
    }

    .options {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      column-gap: var(--space-4);
      overflow: hidden;
    }
  }

  .modes {
    display: grid;
    gap: var(--space-1);
    padding: 0;
    list-style: none;
  }

  .after {
    display: grid;
    gap: var(--space-1);
    justify-items: start;
  }

  .after legend {
    margin-bottom: var(--space-1);
  }

  .after:disabled .choice {
    color: var(--ink-muted);
    cursor: default;
  }

  .days {
    margin-inline-start: var(--space-6);
  }

  .days .input {
    width: 6rem;
  }

  .warning {
    width: 100%;
  }
</style>
