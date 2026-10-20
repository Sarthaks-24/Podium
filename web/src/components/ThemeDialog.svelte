<!--
  ThemeDialog: "Theme and colours", the theme picker (ThemePicker.svelte)
  in the shared native dialog shell, opened from the account menu (v2 N2,
  audit 04-5). The shell renders the one copy and opens it from
  shell.theme. Focus starts on the heading so the title is read first;
  Escape and the close button shut it, and the browser returns focus to the
  control that opened it. Props: `open` and `onclose`.
-->
<script lang="ts">
  import Icon from './Icon.svelte';
  import ThemePicker from './ThemePicker.svelte';

  let { open, onclose }: { open: boolean; onclose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      heading?.focus();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });

  function onCancel(event: Event) {
    event.preventDefault();
    onclose();
  }
</script>

<dialog bind:this={dialog} class="dialog theme-dialog" aria-labelledby="theme-dialog-title" oncancel={onCancel}>
  <div class="dialog-head">
    <h2 id="theme-dialog-title" tabindex="-1" bind:this={heading}>Theme and colours</h2>
    <button type="button" class="btn btn-quiet btn-compact" aria-label="Close theme and colours" title="Close" onclick={onclose}>
      <Icon name="x" />
    </button>
  </div>
  <div class="body">
    <ThemePicker />
  </div>
</dialog>

<style>
  .theme-dialog {
    width: min(26rem, calc(100vw - 2 * var(--gutter)));
  }

  .body {
    margin-inline: calc(-1 * var(--space-2));
  }
</style>
