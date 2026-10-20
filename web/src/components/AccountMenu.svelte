<!--
  AccountMenu: the one account control in the header (v2 N2, audit 04-4 and
  04-17). It says who is signed in ("Signed in as {email}" and the role
  word), then "Notices" with the unread count (lib/notices.svelte.ts; for
  an organiser only while one is unread) and,
  for a student, "Your standing" and the "Hide my progress" switch
  (hide_progress, lib/account.ts: their own progress page and the Home
  line go empty; organisers still see the results), then "Theme and
  colours" and "Keyboard shortcuts" (dialogs the shell owns, opened
  through lib/shell.svelte.ts; the shortcuts item is left out on
  touch-only screens) and, after a rule, "Sign out".

  As a popover (the default) a quiet button names the person from 1024 px
  and reads "Account" below that, with a chevron; it opens a small panel
  under it. Escape closes the panel and returns focus to the button, as do
  a click outside and tabbing out; the panel stays open behind a dialog it
  opened, so closing the dialog lands back on the item. Inline (inside the
  organiser's phone menu panel) the same items are listed in place, with no
  second popover.

  Props: `user` (the signed-in user), `inline`, and `onsignout`.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { saveHideProgress } from '../lib/account';
  import { ApiError } from '../lib/api';
  import { roleLabel } from '../lib/format';
  import { notices } from '../lib/notices.svelte';
  import { showToast } from '../lib/toast.svelte';
  import { displayName, type User } from '../lib/session.svelte';
  import { shell } from '../lib/shell.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    user: User;
    inline?: boolean;
    onsignout: () => void;
  }

  let { user, inline = false, onsignout }: Props = $props();

  let open = $state(false);
  let root: HTMLDivElement | undefined = $state();
  let button: HTMLButtonElement | undefined = $state();

  const panelId = 'account-panel';

  function inDialog(node: Node | null): boolean {
    return node instanceof Element && node.closest('dialog') !== null;
  }

  async function toggle() {
    open = !open;
    await tick();
  }

  function onWindowKeydown(event: KeyboardEvent) {
    if (!open || event.key !== 'Escape' || event.defaultPrevented) return;
    // A dialog opened from the panel takes its own Escape.
    if (document.querySelector('dialog[open]')) return;
    event.preventDefault();
    open = false;
    button?.focus();
  }

  function onWindowPointerdown(event: PointerEvent) {
    const target = event.target as Node;
    if (open && root && !root.contains(target) && !inDialog(target)) open = false;
  }

  function onFocusout(event: FocusEvent) {
    const next = event.relatedTarget as Node | null;
    if (open && next && root && !root.contains(next) && !inDialog(next)) open = false;
  }

  const showNotices = $derived(user.role === 'student' || notices.unread > 0);

  let hideBusy = $state(false);

  async function setHide(event: Event) {
    const box = event.currentTarget as HTMLInputElement;
    const hide = box.checked;
    hideBusy = true;
    try {
      await saveHideProgress(hide);
      showToast(hide ? 'Your progress page is hidden. Your results are kept.' : 'Your progress page is shown again.');
    } catch (e) {
      box.checked = !hide;
      showToast(e instanceof ApiError ? e.message : 'Your choice could not be saved. Try again in a moment.', 'error');
    } finally {
      hideBusy = false;
    }
  }

  function signOut() {
    open = false;
    onsignout();
  }
</script>

<svelte:window onkeydown={onWindowKeydown} onpointerdown={onWindowPointerdown} />

{#snippet items()}
  <p class="who">
    <span class="who-line">Signed in as <span class="email">{user.email}</span></span>
    <span class="role">{roleLabel(user.role)}</span>
  </p>
  <ul class="items">
    {#if showNotices}
      <li>
        <a class="item" href="/notices" onclick={() => (open = false)}>
          Notices{#if notices.unread > 0}<span class="unread">, <span class="mono">{notices.unread}</span> unread</span>{/if}
        </a>
      </li>
    {/if}
    {#if user.role === 'student'}
      <li><a class="item" href="/integrity" onclick={() => (open = false)}>Your standing</a></li>
      <li class="hide">
        <label class="item check">
          <input type="checkbox" checked={user.hide_progress === true} disabled={hideBusy} onchange={setHide} aria-describedby="hide-progress-help" />
          Hide my progress
        </label>
        <p id="hide-progress-help" class="help">Empties your progress page. Organisers still see your results.</p>
      </li>
    {/if}
    <li class:rule={showNotices}><button type="button" class="item" onclick={() => (shell.theme = true)}>Theme and colours</button></li>
    <li class="keys"><button type="button" class="item" aria-keyshortcuts="?" onclick={() => (shell.shortcuts = true)}>Keyboard shortcuts</button></li>
    <li class="rule"><button type="button" class="item" onclick={signOut}>Sign out</button></li>
  </ul>
{/snippet}

{#if inline}
  <div class="inline">{@render items()}</div>
{:else}
  <div class="account" bind:this={root} onfocusout={onFocusout}>
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

  /* The name from 1024 px; below that the button reads "Account" (N7). */
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

  .unread {
    margin-left: 0;
    color: var(--ink-muted);
  }

  a.item {
    text-decoration: none;
  }

  .check {
    gap: var(--space-2);
    cursor: pointer;
  }

  .help {
    padding: 0 var(--space-2) var(--space-1) calc(var(--space-2) + 1.5em);
    color: var(--ink-muted);
    font-size: var(--fs-0);
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

  .inline .help {
    padding-left: 1.5em;
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
