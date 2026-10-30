<!--
  EditorNotes: the problem editor's notes above the tabs (split out of
  ProblemEditor to keep it readable): that the problem is archived; that a
  live contest uses it, with a link to each one's live view (where "Update
  students now" gives students the saved change) and the "Reason for this
  change" field a save then needs; and that the AI assistant wrote the
  draft. Props: `problem`, `aiDraft`, `reason` (bindable) and `fields`
  (the save's field errors, for `reason`).
-->
<script lang="ts">
  import type { Problem } from '../../lib/problems';

  interface Props {
    problem: Problem;
    aiDraft: boolean;
    reason: string;
    fields: Record<string, string>;
  }

  let { problem, aiDraft, reason = $bindable(), fields }: Props = $props();
</script>

{#if problem.archived}
  <div class="banner banner-info lead">
    <p>This problem is archived. It is hidden from the bank and kept for the contests that used it.</p>
  </div>
{/if}

{#if (problem.live_contests ?? []).length > 0}
  {@const live = problem.live_contests ?? []}
  <div class="banner banner-info lead live-note">
    <p>
      {live.map((c) => c.title).join(', ')}
      {live.length === 1 ? 'is' : 'are'} live with this problem. Students keep seeing it as it was until you choose Update
      students now on the contest's live view.
    </p>
    <ul class="consoles">
      {#each live as c (c.contest_id)}
        <li><a href="/admin/contests/{c.contest_id}/console#versions-title">Open the live view of {c.title}</a> (problem {c.label})</li>
      {/each}
    </ul>
    <div class="field reason-field">
      <label for="reason">Reason for this change</label>
      <input
        id="reason"
        class="input"
        type="text"
        maxlength="500"
        autocomplete="off"
        bind:value={reason}
        aria-invalid={fields.reason ? 'true' : undefined}
        aria-describedby="reason-help{fields.reason ? ' reason-error' : ''}"
      />
      <p id="reason-help" class="field-help">Required while a contest is live. It is kept in the contest's history of changes.</p>
      {#if fields.reason}<p id="reason-error" class="field-error">{fields.reason}</p>{/if}
    </div>
  </div>
{/if}

{#if aiDraft}
  <div class="banner banner-info lead">
    <p>
      The AI assistant wrote this draft. Read the statement, the solutions and the tests before you use it; students see nothing until it
      is validated and added to a contest. The AI draft tag goes when you save.
    </p>
  </div>
{/if}

<style>
  .reason-field {
    max-width: var(--measure-form);
    margin-top: var(--space-3);
  }

  .consoles {
    margin: var(--space-2) 0 0;
    padding-left: var(--space-5);
  }

  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-form);
  }
</style>
