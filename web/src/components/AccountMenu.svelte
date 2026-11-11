<!--
  AccountMenu: the one account control in the header. It says who is signed in ("Signed in as {email}" and the role
  word), then "Your profile" (the /me page, v4 M1), "Theme and
  colours" and "Help" (dialogs the shell owns, opened
  through lib/shell.svelte.ts; the shortcuts item is left out on
  touch-only screens), for organisers "Forget key" (DELETE
  /api/admin/ai/key: the server drops every AI provider key this sign-in
  holds) and, after a rule, "Sign out".

  As a popover (the default) a quiet button names the person from 1024 px
  and reads "Account" below that, with a chevron; it opens a small panel
  under it. Escape closes the panel and returns focus to the button, as do
  a click outside and tabbing out; the panel stays open behind a dialog it
  opened, so closing the dialog lands back on the item. Inline (inside the
  organiser's phone menu panel) the same items are listed in place, with no
  second popover. An organiser, admin or owner whose role the owner set to
  "Each person chooses" also gets a "Keep me signed in on this device" switch
  (PUT /api/me/keep-signed-in), with the same two help lines as the sign-in
  page's tick.

  Props: `user` (the signed-in user), `inline`, and `onsignout`.
-->
<script lang="ts">
  import { organises } from '../lib/roles';
  import { tick } from 'svelte';
  import { errorText, sendJSON } from '../lib/api';
  import { dismissable } from '../lib/dismissable';
  import { roleLabel } from '../lib/format';
  import { keepHelp, KEEP_WARNING } from '../lib/keep';
  import { runMode } from '../lib/mode.svelte';
  import { displayName, session, type User } from '../lib/session.svelte';
  import { shell } from '../lib/shell.svelte';
  import { showToast } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    user: User;
    inline?: boolean;
    onsignout: () => void;
  }

  let { user, inline = false, onsignout }: Props = $props();

  let open = $state(false);
  let button: HTMLButtonElement | undefined = $state();

  const panelId = 'account-panel';
  // The popover and the phone menu each render one copy, so the ids differ.
  const helpId = $derived(inline ? 'account-keep-help-inline' : 'account-keep-help-panel');
  const organiser = $derived(organises(user.role));
  const showKeep = $derived(organiser && user.keep_mode === 'choose');
  let keepBusy = $state(false);

  /** Turns this session into a kept one, or back into an ordinary one. */
  async function toggleKeep(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const keep = input.checked;
    keepBusy = true;
    try {
      const out = await sendJSON<{ remembered: boolean }>('PUT', '/api/me/keep-signed-in', { keep });
      session.user = { ...user, remembered: out.remembered };
      showToast(out.remembered ? 'You stay signed in on this device' : 'You are signed out when the browser closes or after 12 hours');
    } catch (e) {
      input.checked = !keep;
      showToast(errorText(e), 'error');
    } finally {
      keepBusy = false;
    }
  }

  async function toggle() {
    open = !open;
    await tick();
  }

  function signOut() {
    open = false;
    onsignout();
  }

  /** Closes the menu first, so the dialog it opens returns focus to the trigger. */
  function openDialog(which: 'theme' | 'help') {
    open = false;
    button?.focus();
    shell[which] = true;
  }

  /** Drops the AI provider keys the server holds for this sign-in. */
  async function forgetKey() {
    open = false;
    button?.focus();
    try {
      await sendJSON<void>('DELETE', '/api/admin/ai/key');
      showToast('AI key forgotten');
    } catch (e) {
      showToast(errorText(e), 'error');
    }
  }
</script>

{#snippet items()}
  <p class="who">
    <span class="who-line">Signed in as <span class="email">{user.email}</span></span>
    <span class="role">{roleLabel(user.role)}</span>
  </p>
  {#if showKeep}
    <div class="keep">
      <label class="choice">
        <input type="checkbox" checked={user.remembered === true} disabled={keepBusy} aria-describedby={helpId} onchange={toggleKeep} />
        Keep me signed in on this device
      </label>
      <p id={helpId} class="field-help">{keepHelp(runMode.keep.days, runMode.keep.untilSignOut)} {KEEP_WARNING}</p>
    </div>
  {/if}
  <ul class="items">
    <li><a class="item" href="/me" onclick={() => (open = false)}>Your profile</a></li>
    <li><button type="button" class="item" onclick={() => openDialog('theme')}>Theme and colours</button></li>
    <li class="keys"><button type="button" class="item" aria-keyshortcuts="?" onclick={() => openDialog('help')}>Help</button></li>
    {#if organiser}<li><button type="button" class="item" onclick={forgetKey}>Forget key</button></li>{/if}
    <li class="rule"><button type="button" class="item" onclick={signOut}>Sign out</button></li>
  </ul>
{/snippet}

{#if inline}
  <div class="inline">{@render items()}</div>
{:else}
  <div class="account" use:dismissable={{ open, onclose: () => (open = false), trigger: button }}>
    <button type="button" class="trigger" aria-expanded={open} aria-controls={panelId} bind:this={button} onclick={toggle}>
      <span class="name">{displayName(user)}</span><span class="word">Account</span>
      <Icon name="chevron-down" />
    </button>
    <div id={panelId} class="panel" hidden={!open}>{@render items()}</div>
  </div>
{/if}

<style>
  .account {
    position: relative;
    min-width: 0;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    max-width: 16rem;
    min-height: var(--control-h);
    padding-inline: var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--ink-muted);
    font-size: var(--fs-0);
    font-weight: 500;
    white-space: nowrap;
    transition: color var(--dur-fast) var(--ease), background-color var(--dur-fast) var(--ease);
  }

  .trigger:hover,
  .trigger[aria-expanded='true'] {
    background: var(--surface-2);
    color: var(--ink);
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* The name from 1024 px; below that the button reads "Account". */
  .word {
    display: none;
  }

  @media (width < 1024px) {
    .name {
      display: none;
    }

    .word {
      display: inline;
    }
  }

  .panel {
    position: absolute;
    inset-inline-end: 0;
    top: calc(100% + var(--space-2));
    z-index: var(--z-bar);
    width: min(18rem, calc(100vw - 2 * var(--gutter)));
    padding: var(--space-2);
    border: var(--rule) solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-menu);
  }

  .panel[hidden] {
    display: none;
  }

  .who {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2) var(--space-2);
    font-size: var(--fs-0);
  }

  .who-line {
    color: var(--ink-muted);
    overflow-wrap: anywhere;
  }

  .email {
    color: var(--ink);
  }

  .role {
    color: var(--ink-muted);
  }

  .keep {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2) var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  .choice {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    font-weight: 500;
    cursor: pointer;
  }

  .choice input {
    flex: none;
    width: var(--space-4);
    height: var(--space-4);
    margin: 0;
    accent-color: var(--act);
  }

  .inline .keep {
    padding-inline: 0;
  }

  .items {
    display: grid;
    padding: 0;
    list-style: none;
  }

  .rule {
    margin-top: var(--space-1);
    padding-top: var(--space-1);
    border-top: var(--rule) solid var(--line);
  }

  .item {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: var(--control-h);
    padding-inline: var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--ink);
    font: inherit;
    font-weight: 500;
    text-align: start;
  }

  a.item {
    text-decoration: none;
  }

  .item:hover {
    background: var(--surface-2);
  }

  .inline .who {
    padding-inline: 0;
    font-size: var(--fs-1);
  }

  .inline .item {
    padding-inline: 0;
  }

  .inline .item:hover {
    background: transparent;
    text-decoration: underline;
  }

  /* Touch screens without a keyboard have no use for the list. */
  @media (hover: none) and (pointer: coarse) {
    .keys {
      display: none;
    }
  }
</style>
