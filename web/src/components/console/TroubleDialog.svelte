<!--
  TroubleDialog: "If something goes wrong", the contest-day failure path
  written into the console (stage C2). Three cases, each a short list of
  what to do in order: the judge stalled (pause, restart the judge, judge
  again), the VM gone (restore, extend, announce) and sign-in down (the
  contest moves). docs/RUNBOOK.md carries the same steps with the
  commands. Props: `open` and `onclose`.
-->
<script lang="ts">
  import Dialog from '../Dialog.svelte';

  let { open, onclose }: { open: boolean; onclose: () => void } = $props();
</script>

<Dialog {open} {onclose} title="If something goes wrong" closeLabel="Close">
  <div class="cases">
    <section aria-labelledby="trouble-judge">
      <h3 id="trouble-judge">The judge stalled</h3>
      <p class="small muted">The top line says "Judge not seen", or submissions wait for minutes.</p>
      <ol>
        <li>Pause judging here, so submissions wait in the queue. Students can keep submitting.</li>
        <li>Restart the judge on the VM: <code>sudo systemctl restart podium-judge</code>.</li>
        <li>Resume judging, then judge again any problem whose submissions ended in a judge error.</li>
        <li>Announce what happened. Nothing students submitted is lost.</li>
      </ol>
    </section>
    <section aria-labelledby="trouble-vm">
      <h3 id="trouble-vm">The VM is gone</h3>
      <p class="small muted">Nobody can open the site, and the provider's console shows the VM down or lost.</p>
      <ol>
        <li>Restore onto a new VM, as the runbook's "Restoring onto a new VM" says. Results are safe up to the last backup.</li>
        <li>Extend the contest by the time lost, or end it and run it again.</li>
        <li>Announce the new end, by email as well if students cannot reach the site.</li>
      </ol>
    </section>
    <section aria-labelledby="trouble-signin">
      <h3 id="trouble-signin">Sign-in is down</h3>
      <p class="small muted">Students cannot sign in with Google. Those already signed in keep working.</p>
      <ol>
        <li>If many students cannot get in, the contest moves: before the start, change the start in Setup; after it, end the contest now and run it again at a new time.</li>
        <li>Tell students the new time by email.</li>
      </ol>
    </section>
  </div>
</Dialog>

<style>
  .cases {
    display: grid;
    gap: var(--space-5);
    margin-top: var(--space-3);
  }

  .cases section {
    display: grid;
    gap: var(--space-2);
  }

  h3 {
    font-size: var(--fs-sub);
  }

  ol {
    display: grid;
    gap: var(--space-1);
    padding-left: var(--space-5);
  }

  code {
    font-family: var(--font-mono);
    font-size: var(--mono-optical);
    overflow-wrap: anywhere;
  }
</style>
