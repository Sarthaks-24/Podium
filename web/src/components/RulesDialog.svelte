<!--
  RulesDialog: the contest's rules during the contest (D114, F4.3), opened
  from "Rules" in the workspace rail. It lists the same sentences as the
  page before the start (lib/contests.ts rulesSentences), then what Podium
  records (RECORDED_SENTENCE), in the shared native dialog shell. Focus
  starts on the heading; Escape and the close button shut it, and the
  browser returns focus to "Rules". Props: `open`, `contest` and `onclose`.
-->
<script lang="ts">
  import { RECORDED_SENTENCE, rulesSentences, type Contest } from '../lib/contests';
  import Icon from './Icon.svelte';

  interface Props {
    open: boolean;
    contest: Pick<Contest, 'start_mode' | 'duration_minutes' | 'starts_at' | 'ends_at' | 'rules'>;
    onclose: () => void;
  }

  let { open, contest, onclose }: Props = $props();

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

<dialog bind:this={dialog} class="dialog" aria-labelledby="rules-dialog-title" oncancel={onCancel}>
  <div class="dialog-head">
    <h2 id="rules-dialog-title" tabindex="-1" bind:this={heading}>Rules</h2>
    <button type="button" class="btn btn-quiet btn-compact" aria-label="Close rules" title="Close" onclick={onclose}>
      <Icon name="x" />
    </button>
  </div>
  <ul class="rules">
    {#each rulesSentences(contest) as line, i (i)}<li>{line}</li>{/each}
    <li>{RECORDED_SENTENCE}</li>
  </ul>
</dialog>

<style>
  .rules {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-3);
    padding-left: var(--space-5);
  }
</style>
