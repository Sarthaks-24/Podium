<!--
  LiveTab: the Live tab of the Dashboard (V4-17), the five lines an admin
  watches during a test run: how many people have a page open, when the
  judge last asked for work, the queue and its median wait, disk free and
  the database's size, and the newest nightly backup. It reads GET
  /api/admin/live every 5 seconds while the tab is shown and the browser
  tab is visible, and not at all otherwise. A failed read keeps the last
  answer and says it may be out of date. Props: `active` (the tab is
  shown) and `onforbidden` (the person lost the Dashboard).
-->
<script lang="ts">
  import MonoDigits from '../MonoDigits.svelte';
  import { errorText, getJSON } from '../../lib/api';
  import { countOf, formatDateTime } from '../../lib/format';
  import { settingsRefused } from '../../lib/people';

  interface Props {
    active: boolean;
    onforbidden: () => void;
  }

  let { active, onforbidden }: Props = $props();

  interface Live {
    server_now: number;
    signed_in_now: number;
    judge_last_seen_s: number | null;
    queued: number;
    submit_wait_p50_ms: number;
    disk_free_mb: number;
    database_mb: number;
    last_backup_at: number | null;
  }

  let live = $state<Live | null>(null);
  let error = $state('');

  async function read() {
    try {
      live = await getJSON<Live>('/api/admin/live');
      error = '';
    } catch (err) {
      if (!settingsRefused(err, onforbidden)) error = errorText(err);
    }
  }

  $effect(() => {
    if (!active) return;
    void read();
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') void read();
    }, 5000);
    const onShow = () => {
      if (document.visibilityState === 'visible') void read();
    };
    document.addEventListener('visibilitychange', onShow);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onShow);
    };
  });

  function seconds(s: number): string {
    return s < 60 ? `${s} s ago` : s < 3600 ? `${Math.floor(s / 60)} min ago` : `${Math.floor(s / 3600)} h ago`;
  }
</script>

<section class="section" aria-labelledby="live-heading">
  <h2 id="live-heading">Live</h2>
  <p class="intro muted">Updated every 5 seconds while this tab is open.</p>
  {#if error !== ''}
    <p class="field-error" role="alert">{error}{live ? ' The lines below may be out of date.' : ''}</p>
  {/if}
  {#if live}
    <dl class="lines">
      <dt>Signed in now</dt>
      <dd><MonoDigits text={`${countOf(live.signed_in_now, 'person', 'people')} with a page open`} /></dd>
      <dt>Judge</dt>
      <dd>
        {#if live.judge_last_seen_s === null}
          No judge has asked for work since the server started.
        {:else}
          <MonoDigits text={`Last asked for work ${seconds(live.judge_last_seen_s)}`} />
        {/if}
      </dd>
      <dt>Queue</dt>
      <dd>
        <MonoDigits
          text={`${live.queued} waiting${live.submit_wait_p50_ms > 0 ? `, median wait ${(live.submit_wait_p50_ms / 1000).toFixed(1)} s over the last 10 minutes` : ''}`}
        />
      </dd>
      <dt>Disk</dt>
      <dd>
        {#if live.disk_free_mb < 0}
          Free space is not known on this machine; database <MonoDigits text={`${live.database_mb} MB`} />
        {:else}
          <MonoDigits text={`${live.disk_free_mb.toLocaleString('en-IN')} MB free, database ${live.database_mb} MB`} />
        {/if}
      </dd>
      <dt>Last backup</dt>
      <dd>{#if live.last_backup_at === null}No backup yet.{:else}<MonoDigits text={formatDateTime(live.last_backup_at)} />{/if}</dd>
    </dl>
  {:else if error === ''}
    <p class="muted" role="status">Loading</p>
  {/if}
</section>

<style>
  .lines {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-2) var(--space-5);
    max-width: var(--measure-form);
    margin-top: var(--space-4);
  }

  .lines dt {
    font-weight: 500;
  }

  .lines dd {
    margin: 0;
  }

  @media (width < 768px) {
    .lines {
      grid-template-columns: 1fr;
      gap: 0;
    }

    .lines dd {
      margin-bottom: var(--space-3);
    }
  }
</style>
