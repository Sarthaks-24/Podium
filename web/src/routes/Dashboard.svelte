<!--
  Dashboard: the page for admins and owners at /admin/dashboard (V4-17;
  /admin/people, the old Settings address, opens it too), in tabs so only
  one part shows at a time:
  - People (components/settings/PeopleTab): the list of people who may
    sign in, with roles, Remove, Add people and the refused sign-ins.
  - Live (LiveTab): signed in now, the judge, the queue, disk and the
    last backup, refreshed every 5 s while shown.
  - Sign-in (SignInTab): account help for one address (end sessions,
    disable or enable; resetting a Google link is for owners).
  - Organisation (OrganisationTab, owners only): the details the public
    pages show, and "History of changes" for the site's history.
  The tab is in the address (?tab=live, ?tab=sign-in, ?tab=organisation; People when
  none), so a link can open a tab and a reload keeps it. One request (GET
  /api/admin/people) loads them all, and again after each change to the
  list. The shell lets only admins and owners in; a 403 later (the role
  taken away meanwhile) shows the same sentence. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import Tabs from '../components/Tabs.svelte';
  import LiveTab from '../components/settings/LiveTab.svelte';
  import OrganisationTab from '../components/settings/OrganisationTab.svelte';
  import PeopleTab from '../components/settings/PeopleTab.svelte';
  import SignInTab from '../components/settings/SignInTab.svelte';
  import { UNEXPECTED_MESSAGE, getJSON } from '../lib/api';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import type { Organisation } from '../lib/mode.svelte';
  import type { Account, ListCounts, ListedPerson, PeopleData, Person, Refusal } from '../lib/people';
  import { navigate, router } from '../lib/router.svelte';
  import { forbiddenText, routeTitle } from '../lib/routing';
  import { session } from '../lib/session.svelte';

  type Tab = 'people' | 'live' | 'sign-in' | 'organisation';
  const isOwner = $derived(session.user?.role === 'owner');
  const tabs = $derived<{ id: Tab; label: string }[]>([
    { id: 'people', label: 'People' },
    { id: 'live', label: 'Live' },
    { id: 'sign-in', label: 'Sign-in' },
    ...(isOwner ? [{ id: 'organisation' as Tab, label: 'Organisation' }] : []),
  ]);

  const tab = $derived(tabs.find((t) => t.id === router.query.get('tab'))?.id ?? 'people');

  function select(id: Tab) {
    // The address the page was opened at, the old /admin/people too, so a tab never remounts the page.
    navigate(id === 'people' ? router.path : `${router.path}?tab=${id}`, { replace: true });
  }

  let people = $state<Person[]>([]);
  let list = $state<ListedPerson[]>([]);
  let counts = $state<ListCounts>({ on_list: 0, signed_in: 0, never_came: 0 });
  let refused = $state<Refusal[]>([]);
  let disabled = $state<Account[]>([]);
  let signInOpen = $state(true);
  let org = $state<Organisation>({ name: '', contact_email: '', phone: '', privacy_contact: '' });

  function apply(data: PeopleData) {
    people = data.people ?? [];
    list = data.list ?? [];
    counts = data.counts ?? { on_list: 0, signed_in: 0, never_came: 0 };
    refused = data.refused ?? [];
    disabled = data.disabled ?? [];
    signInOpen = data.signin_open !== false;
    if (data.organisation) org = { ...org, ...data.organisation };
  }

  const page = loader(() => getJSON<PeopleData>('/api/admin/people'), apply);

  /** After a change to the list: the counts and the refused list move too. */
  async function reload() {
    try {
      apply(await getJSON<PeopleData>('/api/admin/people'));
    } catch {
      // The change itself was confirmed; a failed refresh shows on the next load.
    }
  }

  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  const forbid = () => page.forbid();
  const viewer = $derived({ id: session.user?.id ?? 0, role: session.user?.role ?? 'student' });

  onMount(() => {
    setTitle(routeTitle('dashboard'));
    void page.load();
    return () => page.stop();
  });
</script>

<PageHeader title="Dashboard">
  {#snippet nav()}
    {#if page.state === 'ready'}
      <div class="tab-row">
        <Tabs {tabs} selected={tab} onselect={select} label="Dashboard" idPrefix="settings-tab" controls={(t) => `settings-panel-${t}`} />
      </div>
    {/if}
  {/snippet}
</PageHeader>

{#if page.state === 'forbidden'}
  <PageState kind="forbidden" message={forbiddenText({ role: 'admin' })} />
{:else if page.state === 'loading'}
  <PageState kind="loading" message="Loading the Dashboard" />
{:else if page.state === 'error' || page.state === 'missing'}
  <PageState kind="error" message={page.error || UNEXPECTED_MESSAGE} onretry={() => void page.load()} />
{:else}
  <div class="panels">
    <div id="settings-panel-people" role="tabpanel" aria-labelledby="settings-tab-people" hidden={tab !== 'people'}>
      <PeopleTab {list} {counts} {refused} {viewer} {origin} onreload={reload} onforbidden={forbid} />
    </div>
    <div id="settings-panel-live" role="tabpanel" aria-labelledby="settings-tab-live" hidden={tab !== 'live'}>
      <LiveTab active={tab === 'live'} onforbidden={forbid} />
    </div>
    <div id="settings-panel-sign-in" role="tabpanel" aria-labelledby="settings-tab-sign-in" hidden={tab !== 'sign-in'}>
      <SignInTab bind:open={signInOpen} bind:disabled bind:people owner={isOwner} onforbidden={forbid} />
    </div>
    {#if isOwner}
      <div id="settings-panel-organisation" role="tabpanel" aria-labelledby="settings-tab-organisation" hidden={tab !== 'organisation'}>
        <OrganisationTab bind:org onforbidden={forbid} />
      </div>
    {/if}
  </div>
{/if}

<style>
  .tab-row {
    border-bottom: var(--rule) solid var(--line);
  }

  /* Four tabs do not fit a phone in one row: they wrap rather than scroll the page. */
  @media (width < 768px) {
    .tab-row :global(.tabs) {
      flex-wrap: wrap;
    }
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
