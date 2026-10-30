<!--
  StandingsPage: a contest's board for students, at
  /contests/{slug}/standings: the page header and
  StandingsView. During the contest the board opens as a panel over the
  editor instead; this page is for after the end, and for a link.
  Back returns to the problem the student came from when the address
  names one (?from=B). Props: `slug`.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import StandingsView from '../components/StandingsView.svelte';
  import type { Contest } from '../lib/contests';
  import { setTitle } from '../lib/head';
  import { router } from '../lib/router.svelte';
  import { routeTitle } from '../lib/routing';

  let { slug }: { slug: string } = $props();

  // A label is one or two capital letters.
  const from = router.query.get('from') ?? '';
  const backHref = $derived(`/contests/${encodeURIComponent(slug)}${/^[A-Z]{1,2}$/.test(from) ? `/problems/${from}` : ''}`);
  let contest = $state.raw<Contest | null>(null);

  onMount(() => setTitle(routeTitle('standings')));

  function onContest(c: Contest) {
    contest = c;
    setTitle(routeTitle('standings'), c.title);
  }
</script>

<PageHeader title="Standings" back={{ href: backHref, label: contest?.title ?? 'Contest' }} />
<StandingsView {slug} oncontest={onContest} />
