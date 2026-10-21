<!--
  ConfirmDialog: a native modal <dialog> that asks before a destructive
  action. Props: `open` (shows the dialog with showModal when true), `title`
  (the question, used as the dialog's accessible name), `body` (one line of
  consequence), `confirmLabel` (the action word, reused from the button that
  opened it), `items` (optional lines under the body, such as the checks a
  publish passed), `danger` (styles the confirm button as danger), `busy`
  (disables both buttons while the request runs), `error` (optional: why
  the action failed, shown inside the dialog with role="alert"; a toast
  would sit under the modal backdrop, out of reach), `onconfirm` and
  `oncancel`. Escape and Cancel both call `oncancel`. Focus starts on Cancel
  so a stray Enter never confirms, and the browser returns focus to the
  opener when the dialog closes.
-->
<script lang="ts">
  interface Props {
    open: boolean;
    title: string;
    body: string;
    confirmLabel: string;
    items?: readonly string[];
    danger?: boolean;
    busy?: boolean;
    error?: string;
    onconfirm: () => void;
    oncancel: () => void;
  }

  let { open, title, body, confirmLabel, items = [], danger = false, busy = false, error = '', onconfirm, oncancel }: Props = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let cancelButton: HTMLButtonElement | undefined = $state();
  const id = `confirm-${Math.random().toString(36).slice(2, 9)}`;

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      cancelButton?.focus();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  function onCancelEvent(event: Event) {
    event.preventDefault();
    if (!busy) oncancel();
  }
</script>

<dialog
  bind:this={dialog}
  class="dialog dialog-small"
  aria-labelledby="{id}-title"
  aria-describedby="{id}-body"
  oncancel={onCancelEvent}
>
  <h2 id="{id}-title" class="title">{title}</h2>
  <div id="{id}-body">
    <p class="body">{body}</p>
    {#if items.length > 0}<ul class="items">{#each items as item, i (i)}<li>{item}</li>{/each}</ul>{/if}
  </div>
  {#if error}<p class="field-error confirm-error" role="alert">{error}</p>{/if}
  <div class="actions">
    <button
      type="button"
      class="btn {danger ? 'btn-danger' : 'btn-primary'}"
      disabled={busy}
      onclick={onconfirm}
    >
      {confirmLabel}
    </button>
    <button type="button" class="btn btn-secondary" disabled={busy} bind:this={cancelButton} onclick={oncancel}>
      Cancel
    </button>
  </div>
</dialog>

<style>
  .items {
    margin-top: var(--space-2);
    padding-left: var(--space-5);
    color: var(--ink-muted);
  }

  .confirm-error {
    margin-top: var(--space-3);
  }

  .body {
    margin-top: var(--space-3);
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-5);
  }
</style>
