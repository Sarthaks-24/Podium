<!--
  OrganisationTab: the Organisation tab of Settings. The name, contact
  email, phone and privacy contact the public pages show, saved with PUT
  /api/admin/site-settings ("Save organisation details"); each refused
  field says what to fix under itself, and the footer and sign-in page pick
  the new values up at once. Under the form, "History of changes" opens
  the site's history (HistoryDialog), which is needed rarely and so is not
  on the page. From 1024 px the four fields sit in two columns. Props:
  `org` (bindable) and `onforbidden` (the person is no longer an owner).
-->
<script lang="ts">
  import HistoryDialog from './HistoryDialog.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { focusFirstInvalid } from '../../lib/focus';
  import { runMode, type Organisation } from '../../lib/mode.svelte';
  import { settingsRefused } from '../../lib/people';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    org: Organisation;
    onforbidden: () => void;
  }

  let { org = $bindable(), onforbidden }: Props = $props();

  const FIELDS = [
    { key: 'name', label: 'Organisation name', type: 'text', help: 'For example Example College of Engineering.' },
    { key: 'contact_email', label: 'Contact email', type: 'email', help: 'Shown to everyone, signed in or not.' },
    { key: 'phone', label: 'Phone', type: 'tel', help: 'Optional. Digits, spaces and + - ( ) only.' },
    { key: 'privacy_contact', label: 'Privacy contact', type: 'text', help: 'Who to ask about personal data: a name or an address.' },
  ] as const;

  let errors = $state<Record<string, string>>({});
  let error = $state('');
  let busy = $state(false);
  let historyOpen = $state(false);

  async function save(event: SubmitEvent) {
    event.preventDefault();
    errors = {};
    error = '';
    busy = true;
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
    } catch (err) {
      if (settingsRefused(err, onforbidden)) return;
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        errors = { ...err.fields };
        await focusFirstInvalid('#org-form');
      } else {
        error = errorText(err);
      }
    } finally {
      busy = false;
    }
  }
</script>

<section class="section" aria-labelledby="org-heading">
  <h2 id="org-heading">Organisation details</h2>
  <p class="intro muted">
    Shown on the sign-in page, in the footer and on the privacy page. Use a role address as the contact email, so your own address stops
    appearing on public pages.
  </p>
  <form id="org-form" class="org" novalidate onsubmit={save}>
    <div class="fields">
      {#each FIELDS as f (f.key)}
        <div class="field">
          <label for="org-{f.key}">{f.label}</label>
          <input
            id="org-{f.key}"
            class="input"
            type={f.type}
            autocomplete="off"
            spellcheck="false"
            bind:value={org[f.key]}
            aria-invalid={errors[f.key] ? 'true' : undefined}
            aria-describedby="org-{f.key}-help{errors[f.key] ? ` org-${f.key}-error` : ''}"
          />
          <p id="org-{f.key}-help" class="field-help">{f.help}</p>
          {#if errors[f.key]}<p id="org-{f.key}-error" class="field-error">{errors[f.key]}</p>{/if}
        </div>
      {/each}
    </div>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <button type="submit" class="btn btn-secondary" disabled={busy}>Save organisation details</button>
  </form>
  <p class="history">
    <button type="button" class="btn btn-link btn-compact" onclick={() => (historyOpen = true)}>History of changes</button>
  </p>
</section>

<HistoryDialog open={historyOpen} onclose={() => (historyOpen = false)} />

<style>
  .org {
    display: grid;
    gap: var(--space-4);
    justify-items: start;
    max-width: var(--measure-wide);
    margin-top: var(--space-4);
  }

  .fields {
    display: grid;
    gap: var(--space-4);
    width: 100%;
    max-width: var(--measure-form);
  }

  @media (min-width: 1024px) {
    .fields {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: var(--space-4) var(--space-5);
      max-width: none;
    }
  }

  .history {
    margin-top: var(--space-5);
    padding-top: var(--space-4);
    border-top: var(--rule) solid var(--line);
  }
</style>
