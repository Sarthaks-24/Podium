<!--
  ConfirmDialog: a native modal <dialog> that asks before a destructive
  action. Props: `open` (shows the dialog with showModal when true), `title`
  (the question, used as the dialog's accessible name), `body` (one line of
  consequence), `confirmLabel` (the action word, reused from the button that
  opened it), `danger` (styles the confirm button as danger), `busy`
  (disables both buttons while the request runs), `onconfirm` and
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
    danger?: boolean;
    busy?: boolean;
    onconfirm: () => void;
    oncancel: () => void;
  }

  let { open, title, body, confirmLabel, danger = false, busy = false, onconfirm, oncancel }: Props = $props();

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
  <p id="{id}-body" class="body">{body}</p>
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
