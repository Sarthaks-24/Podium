<!--
  SubmitButton: the one primary button of the workspace, with the send
  icon. While a submit is on its way it reads "Submitting", and the
  contest's wait counts down inside it ("Submit in 24 s"); both keep
  it aria-disabled so focus stays, and a fixed width so nothing moves.
  Props: `label`, `waiting` (busy or counting down), `locked` (Submit is
  off for a reason said elsewhere), `describedby` and `onclick`.
-->
<script lang="ts">
  import Icon from '../Icon.svelte';
  import { shortcutKeys } from '../../lib/shortcuts';

  interface Props {
    label: string;
    waiting: boolean;
    locked: boolean;
    describedby?: string;
    onclick: () => void;
  }

  let { label, waiting, locked, describedby, onclick }: Props = $props();

  // Submit's key, from the shortcut table (v4 U6).
  const keys = shortcutKeys('Submit');
</script>

<button
  type="button"
  class="btn btn-primary btn-compact submit"
  class:btn-wait={waiting}
  disabled={locked}
  aria-disabled={waiting ? 'true' : undefined}
  aria-describedby={describedby}
  title="Submit ({keys.text})"
  aria-keyshortcuts={keys.aria}
  {onclick}><Icon name="send" />{label}</button
>

<style>
  /* "Submit in 24 s" fits without the button growing as it counts. */
  .submit {
    min-width: 9rem;
  }
</style>
