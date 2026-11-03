<!--
  InvitesPanel: the invited students of an invite-only contest, in contest
  setup (v4.01 stage I). It lists who is invited, each with Remove, which
  is disabled while the contest runs (the server refuses it then too). New
  students come from the sign-in list: a picker with a search, checkboxes,
  "Select all shown" and "Invite N", or a pasted list of addresses, one per
  line, that Check previews (added, already invited, and each line that
  matched no student) before "Invite N" writes anything. Only enabled
  students on the sign-in list can be invited; the server decides.
  Props: `base` (the contest's admin API path, /api/admin/contests/{id})
  and `running` (true from the start until the final close).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { polite } from '../lib/announce.svelte';
  import { errorText, getJSON, sendJSON } from '../lib/api';
  import type { ContestInvite, InviteCandidate, InvitePasteResult } from '../lib/contests';
  import { countOf } from '../lib/format';

  interface Props {
    base: string;
    running: boolean;
  }

  let { base, running }: Props = $props();

  let invites = $state<ContestInvite[]>([]);
  let candidates = $state<InviteCandidate[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let busy = $state(false);
  let query = $state('');
  let chosen = $state<number[]>([]);
  let paste = $state('');
  let preview = $state<InvitePasteResult | null>(null);

  const LOCKED = 'Invites cannot be removed while the contest runs.';

  const open = $derived(candidates.filter((c) => !c.invited));
  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return q === '' ? open : open.filter((c) => c.name.toLowerCase().includes(q) || c.email.toLowerCase().includes(q));
  });

  async function load() {
    try {
      const [list, cands] = await Promise.all([
        getJSON<{ invites: ContestInvite[] }>(`${base}/invites`),
        getJSON<{ candidates: InviteCandidate[] }>(`${base}/invites/candidates`),
      ]);
      invites = list.invites;
      candidates = cands.candidates;
      chosen = chosen.filter((id) => candidates.some((c) => c.user_id === id && !c.invited));
      error = '';
    } catch (e) {
      error = errorText(e);
    } finally {
      loaded = true;
    }
  }

  onMount(load);

  function toggle(id: number, on: boolean) {
    chosen = on ? [...chosen, id] : chosen.filter((x) => x !== id);
  }

  function selectShown() {
    chosen = [...new Set([...chosen, ...shown.map((c) => c.user_id)])];
  }

  async function invitePicked() {
    if (chosen.length === 0 || busy) return;
    busy = true;
    error = '';
    try {
      const out = await sendJSON<{ added: number }>('POST', `${base}/invites`, { user_ids: chosen });
      chosen = [];
      await load();
      polite(`Invited ${countOf(out.added, 'student')}.`);
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function check() {
    if (paste.trim() === '' || busy) return;
    busy = true;
    error = '';
    try {
      preview = await sendJSON<InvitePasteResult>('POST', `${base}/invites/paste`, { text: paste, dry_run: true });
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function invitePasted() {
    if (!preview || preview.added.length === 0 || busy) return;
    busy = true;
    error = '';
    try {
      const out = await sendJSON<InvitePasteResult>('POST', `${base}/invites/paste`, { text: paste, dry_run: false });
      paste = '';
      preview = null;
      await load();
      polite(`Invited ${countOf(out.added.length, 'student')}.`);
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function remove(invite: ContestInvite) {
    if (running || busy) return;
    busy = true;
    error = '';
    try {
      await sendJSON('DELETE', `${base}/invites/${invite.user_id}`);
      await load();
      polite(`Removed the invite of ${invite.name || invite.email}.`);
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="invites">
  {#if error}<p class="field-error" role="alert">{error}</p>{/if}

  <h3 id="invited-title">Invited students ({invites.length})</h3>
  {#if !loaded}
    <p class="muted">Loading invites.</p>
  {:else if invites.length === 0}
    <p class="muted">Nobody is invited yet. Only invited students see this contest.</p>
  {:else}
    {#if running}<p id="invites-locked" class="muted">{LOCKED}</p>{/if}
    <ul class="list" aria-labelledby="invited-title">
      {#each invites as invite (invite.user_id)}
        <li>
          <span>{invite.name || invite.email} <span class="muted">{invite.email}</span></span>
          <button
            type="button"
            class="btn btn-danger-quiet btn-compact"
            disabled={running || busy}
            aria-describedby={running ? 'invites-locked' : undefined}
            onclick={() => remove(invite)}>Remove {invite.email}</button
          >
        </li>
      {/each}
    </ul>
  {/if}

  <h3 id="pick-title">Invite from the sign-in list</h3>
  <div class="field">
    <label for="invite-search">Search students by name or email</label>
    <input id="invite-search" class="input" type="search" autocomplete="off" bind:value={query} />
  </div>
  {#if loaded && shown.length === 0}
    <p class="muted">{open.length === 0 && query.trim() === '' ? 'Every student on the sign-in list is invited.' : 'Not on the sign-in list. Ask an admin to add them.'}</p>
  {:else}
    <ul class="list picker" aria-labelledby="pick-title">
      {#each shown as c (c.user_id)}
        <li>
          <label class="choice">
            <input type="checkbox" checked={chosen.includes(c.user_id)} onchange={(e) => toggle(c.user_id, e.currentTarget.checked)} />
            {c.name || c.email} <span class="muted">{c.email}</span>
          </label>
        </li>
      {/each}
    </ul>
  {/if}
  <div class="row">
    <button type="button" class="btn btn-quiet" disabled={shown.length === 0} onclick={selectShown}>Select all shown</button>
    <span class="muted" aria-live="polite">{countOf(chosen.length, 'student')} selected</span>
    <button type="button" class="btn btn-secondary" disabled={chosen.length === 0 || busy} onclick={invitePicked}
      >Invite {countOf(chosen.length, 'student')}</button
    >
  </div>

  <h3>Or paste addresses</h3>
  <div class="field">
    <label for="invite-paste">One email address per line</label>
    <textarea id="invite-paste" class="input" rows="4" spellcheck="false" bind:value={paste} oninput={() => (preview = null)}></textarea>
  </div>
  <div class="row">
    <button type="button" class="btn btn-quiet" disabled={paste.trim() === '' || busy} onclick={check}>Check</button>
    {#if preview}
      <button type="button" class="btn btn-secondary" disabled={preview.added.length === 0 || busy} onclick={invitePasted}
        >Invite {countOf(preview.added.length, 'student')}</button
      >
    {/if}
  </div>
  {#if preview}
    <div class="preview" role="status">
      <p>
        {countOf(preview.added.length, 'student')} to invite, {preview.already_invited.length} already invited,
        {countOf(preview.unmatched.length, 'line')} not matched.
      </p>
      {#if preview.unmatched.length > 0}
        <ul class="list">
          {#each preview.unmatched as miss, i (i)}
            <li><span class="mono">{miss.line}</span>: {miss.reason}</li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</div>

<style>
  .invites {
    display: grid;
    gap: var(--space-3);
  }

  .list {
    display: grid;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .list li {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
    justify-content: space-between;
  }

  .picker {
    max-height: calc(var(--control-h-touch) * 8);
    overflow-y: auto;
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    padding: var(--space-2);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
  }

  .choice {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    min-height: var(--control-h-touch);
  }

  .mono {
    font-family: var(--font-mono);
  }
</style>
