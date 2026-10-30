<!--
  Dialog: the one native modal dialog every dialog and panel is built on,
 styled by controls.css (.dialog, .dialog-small, .dialog-sheet).
  It opens with showModal() when `open` turns true and closes when it turns
  false; Escape and the close button call `onclose` (Escape does nothing
  while `busy`). Focus starts on the heading, or on what `initialFocus`
  returns, and goes back to the control that opened it when it closes;
  when that control is gone (a menu that closed with it), focus goes to the
  page heading, never to the body.
  With `title` it draws the standard head, the title as an h2 and a Close
  button named `closeLabel`; without one the caller draws its own heading
  with the id `labelledby`. `kind` is 'normal', 'small' (a confirm) or
  'sheet' (a panel down the right edge, over the editor column). Props:
  `open`, `onclose`, `title`, `closeLabel`, `labelledby`, `describedby`,
  `kind`, `busy`, `initialFocus`, `onkeydown`, `extraClass`, `dialog`
  (bindable) and `children`.
-->
<script lang="ts">
  import { tick, type Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  interface Props {
    open: boolean;
    onclose: () => void;
    title?: string;
    closeLabel?: string;
    labelledby?: string;
    describedby?: string;
    kind?: 'normal' | 'small' | 'sheet';
    busy?: boolean;
    initialFocus?: () => HTMLElement | null | undefined;
    onkeydown?: (event: KeyboardEvent) => void;
    extraClass?: string;
    dialog?: HTMLDialogElement;
    children: Snippet;
  }

  let {
    open,
    onclose,
    title,
    closeLabel,
    labelledby,
    describedby,
    kind = 'normal',
    busy = false,
    initialFocus,
    onkeydown,
    extraClass = '',
    dialog = $bindable(),
    children,
  }: Props = $props();

  const titleId = `dialog-${Math.random().toString(36).slice(2, 9)}`;
  let heading: HTMLHeadingElement | undefined = $state();
  let opener: HTMLElement | null = null;

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      dialog.showModal();
      void tick().then(() => (initialFocus?.() ?? heading ?? dialog?.querySelector<HTMLElement>('h2'))?.focus());
    } else if (!open && dialog.open) {
      dialog.close();
      const back = opener;
      opener = null;
      if (back && back.isConnected && !back.closest('[hidden], [inert]')) back.focus();
      else document.querySelector<HTMLElement>('main h1')?.focus();
    }
  });

  function onCancel(event: Event) {
    event.preventDefault();
    if (!busy) onclose();
  }
</script>

<dialog
  bind:this={dialog}
  class="dialog {kind === 'small' ? 'dialog-small' : kind === 'sheet' ? 'dialog-sheet' : ''} {extraClass}"
  aria-labelledby={title ? titleId : labelledby}
  aria-describedby={describedby}
  oncancel={onCancel}
  {onkeydown}
>
  {#if title}
    <div class="dialog-head">
      <h2 id={titleId} tabindex="-1" bind:this={heading}>{title}</h2>
      <button type="button" class="btn btn-quiet btn-compact" aria-label={closeLabel ?? `Close ${title.toLowerCase()}`} title="Close" onclick={onclose}>
        <Icon name="x" />
      </button>
    </div>
  {/if}
  {@render children()}
</dialog>
