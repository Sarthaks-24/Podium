<!--
  Settings: the owner's page at /admin/people (the address the People page
  had), in three tabs so only one part shows at a time:
  - People (components/settings/PeopleTab): who can organise, with two
    roles on screen, organiser and owner.
  - Sign-in (SignInTab): account help for
    one address (reset the Google link, end sessions, disable or enable).
  - Organisation (OrganisationTab): the details the public pages show, and
    "History of changes" for the site's history.
  The tab is in the address (?tab=sign-in, ?tab=organisation; People when
  none), so a link can open a tab and a reload keeps it. One request (GET
  /api/admin/people) loads all three; the tabs keep their typed values
  while another is shown. The shell lets only owners in; a 403 later (the
  role taken away meanwhile) shows the same sentence. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import Tabs from '../components/Tabs.svelte';
  import OrganisationTab from '../components/settings/OrganisationTab.svelte';
  import PeopleTab from '../components/settings/PeopleTab.svelte';
  import SignInTab from '../components/settings/SignInTab.svelte';
  import { UNEXPECTED_MESSAGE, getJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import type { Organisation } from '../lib/mode.svelte';
  import type { Account, PeopleData, Person } from '../lib/people';
  import { navigate, router } from '../lib/router.svelte';
  import { forbiddenText, routeTitle } from '../lib/routing';

  type Tab = 'people' | 'sign-in' | 'organisation';
  const TABS: readonly { id: Tab; label: string }[] = [
    { id: 'people', label: 'People' },
    { id: 'sign-in', label: 'Sign-in' },
    { id: 'organisation', label: 'Organisation' },
  ];

  const tab = $derived(TABS.find((t) => t.id === router.query.get('tab'))?.id ?? 'people');

  function select(id: Tab) {
    navigate(id === 'people' ? '/admin/people' : `/admin/people?tab=${id}`, { replace: true });
  }

  let people = $state<Person[]>([]);
  let disabled = $state<Account[]>([]);
  let org = $state<Organisation>({ name: '', contact_email: '', phone: '', privacy_contact: '' });

  const page = loader(
    () => getJSON<PeopleData>('/api/admin/people'),
    (data) => {
      people = data.people ?? [];
      disabled = data.disabled ?? [];
      if (data.organisation) org = { ...org, ...data.organisation };
    },
  );

  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  const forbid = () => page.forbid();

  onMount(() => {
    setTitle(routeTitle('settings'));
    void page.load();
    return () => page.stop();
  });
</script>

<PageHeader title="Settings">
  {#snippet nav()}
    {#if page.state === 'ready'}
      <div class="tab-row">
        <Tabs tabs={TABS} selected={tab} onselect={select} label="Settings" idPrefix="settings-tab" controls={(t) => `settings-panel-${t}`} />
      </div>
    {/if}
  {/snippet}
</PageHeader>

{#if page.state === 'forbidden'}
  <PageState kind="forbidden" message={forbiddenText({ role: 'owner' })} />
{:else if page.state === 'loading'}
  <PageState kind="loading" message="Loading the settings" />
{:else if page.state === 'error' || page.state === 'missing'}
  <PageState kind="error" message={page.error || UNEXPECTED_MESSAGE} onretry={() => void page.load()} />
{:else}
  <div class="panels">
    <div id="settings-panel-people" role="tabpanel" aria-labelledby="settings-tab-people" hidden={tab !== 'people'}>
      <PeopleTab bind:people {origin} onforbidden={forbid} />
    </div>
    <div id="settings-panel-sign-in" role="tabpanel" aria-labelledby="settings-tab-sign-in" hidden={tab !== 'sign-in'}>
      <SignInTab bind:disabled bind:people onforbidden={forbid} />
    </div>
    <div id="settings-panel-organisation" role="tabpanel" aria-labelledby="settings-tab-organisation" hidden={tab !== 'organisation'}>
      <OrganisationTab bind:org onforbidden={forbid} />
    </div>
  </div>
{/if}

<style>
  .tab-row {
    border-bottom: var(--rule) solid var(--line);
  }

  .panels {
    max-width: var(--measure-wide);
    margin-top: var(--space-5);
  }

  /* What the three tabs share: their sections, intros and the one-row forms. */
  .panels :global(.section + .section) {
    margin-top: var(--space-6);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .panels :global(h2) {
    font-size: var(--fs-section);
  }

  .panels :global(.intro) {
    max-width: var(--measure-prose);
    margin-top: var(--space-2);
  }

  .panels :global(.form) {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  .panels :global(.form .grow) {
    flex: 1 1 16rem;
    max-width: 24rem;
  }

  .panels :global(.form-error) {
    margin-top: var(--space-2);
  }

  @media (width < 768px) {
    .panels :global(.form .grow) {
      max-width: none;
    }

    .panels :global(.submit) {
      width: 100%;
    }
  }
</style>
