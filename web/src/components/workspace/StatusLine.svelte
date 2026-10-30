<!--
  StatusLine: the one line under the editor. A pacing refusal without a
  known wait and "3 of 15 submissions used" (muted, never an alert;
  #ws-submit-status, which Submit points to), then the draft state in one
  word per state (design/words.md; "Saved on this device" while a save is
  pending, so a short wait never reads as a failure) and the cursor as LeetCode shows it
  ("Ln 2, Col 5"). A failed save is also said in a polite live region.
  Props: `pace` (the refusal, or ""), `used` ({ count, max } or null),
  `saveState` and `cursor` ({ line, col }).
-->
<script lang="ts">
  import type { SaveState } from '../../lib/draft.svelte';
  import { usedText } from '../../lib/workspace';

  interface Props {
    pace: string;
    used: { count: number; max: number } | null;
    saveState: SaveState;
    cursor: { line: number; col: number };
  }

  let { pace, used, saveState, cursor }: Props = $props();
</script>

<div class="under">
  {#if pace || used}
    <p class="submit-status" id="ws-submit-status">
      {#if pace}<span>{pace}</span>{/if}
      {#if used}<span>{usedText(used.count, used.max)}</span>{/if}
    </p>
  {/if}
  <p class="save" class:failed={saveState === 'failed'}>
    <!-- A pending save is kept on this device and saved within 10 s; only a failed save is a warning. -->
    {#if saveState === 'saved'}Saved{:else if saveState === 'saving'}Saving{:else if saveState === 'local'}Saved on this device{:else}Not saved to the server, kept on this device{/if}
  </p>
  <p class="mono cursor" aria-hidden="true">Ln {cursor.line}, Col {cursor.col}</p>
  <p class="visually-hidden" aria-live="polite">{saveState === 'failed' ? 'Not saved to the server, kept on this device.' : ''}</p>
</div>

<style>
  .under {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-1) var(--space-4);
    padding-top: var(--space-2);
    font-size: var(--fs-0);
  }

  .submit-status {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 0 var(--space-3);
    margin-right: auto;
    color: var(--ink-muted);
    text-align: right;
  }

  .save,
  .cursor {
    color: var(--ink-muted);
  }

  .save.failed {
    color: var(--bad);
  }

  @media (min-width: 1024px) and (max-height: 45rem) {
    .under {
      padding-top: var(--space-1);
    }
  }
</style>
