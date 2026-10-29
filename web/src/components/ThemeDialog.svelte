<!--
  ThemeDialog: "Theme and colours", the theme picker (ThemePicker.svelte)
  in the shared native dialog shell, opened from the account menu (v2 N2,
  audit 04-5). The shell renders the one copy and opens it from
  shell.theme. Focus starts on the heading so the title is read first;
  Escape and the close button shut it, and focus returns to the control
  that opened it, or to the page heading when the account menu closed
  with it (components/Dialog, P8). Props: `open` and `onclose`.
-->
<script lang="ts">
  import Dialog from './Dialog.svelte';
  import ThemePicker from './ThemePicker.svelte';

  let { open, onclose }: { open: boolean; onclose: () => void } = $props();
</script>

<Dialog {open} {onclose} title="Theme and colours" extraClass="theme-dialog">
  <div class="body">
    <ThemePicker />
  </div>
</Dialog>

<style>
  :global(.theme-dialog) {
    width: min(26rem, calc(100vw - 2 * var(--gutter)));
  }

  .body {
    margin-inline: calc(-1 * var(--space-2));
  }
</style>
