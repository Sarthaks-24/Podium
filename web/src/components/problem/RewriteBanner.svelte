<!--
  RewriteBanner: the note on a draft that "Rewrite with AI" made, above the
  editor's tabs: "This is a rewrite of <title>." with "Replace <title> with
  this" and "Keep both". The draft is already a problem of its own, so Keep
  both only removes the note. Replace asks first (ConfirmDialog, focus on
  Cancel), then puts the saved draft on the old problem as one new revision
  and opens that problem. It is off until the draft has passed Validate and
  while the editor has unsaved changes, because the server uses the saved
  draft; the reason sits beside the button and is tied to it with
  aria-describedby. A refusal (a running contest, students who used the old
  problem, an archived or deleted or changed old problem) is shown in the
  server's words in an error banner and the draft stays as it is.

  Props: `draftId`, `link` (the old problem's id and title, and the
  fingerprint from when the rewrite began), `validated` (the draft has passed
  Validate), `dirty` (the editor has unsaved changes) and `onkept` (called
  after Keep both, so the editor drops the note).
-->
<script lang="ts">
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import type { RewriteLink } from '../../lib/ai';
  import { errorText, sendJSON } from '../../lib/api';
  import { draftStorage } from '../../lib/drafts';
  import { confirmText, forgetRewrite, replacedToast, rewritePath, type Replaced } from '../../lib/rewrite';
  import { navigate } from '../../lib/router.svelte';
  import { showToast } from '../../lib/toast.svelte';

  interface Props {
    draftId: number;
    link: RewriteLink;
    validated: boolean;
    dirty: boolean;
    onkept: () => void;
  }

  let { draftId, link, validated, dirty, onkept }: Props = $props();

  let confirmOpen = $state(false);
  let busy = $state(false);
  let refusal = $state('');

  const hint = $derived(!validated ? 'Validate this rewrite first.' : dirty ? 'Save your changes first.' : '');
  const words = $derived(confirmText(link.title, new Date()));

  async function replace() {
    busy = true;
    refusal = '';
    try {
      const out = await sendJSON<Replaced>('POST', rewritePath(link.problem_id), { draft_id: draftId, base: link.base });
      forgetRewrite(draftStorage(), draftId);
      confirmOpen = false;
      showToast(replacedToast(link.title, out));
      navigate(`/admin/problems/${link.problem_id}`);
    } catch (e) {
      confirmOpen = false;
      refusal = errorText(e);
    } finally {
      busy = false;
    }
  }

  function keep() {
    forgetRewrite(draftStorage(), draftId);
    showToast('Kept as its own problem.');
    onkept();
  }
</script>

<div class="banner banner-info lead rewrite">
  <p>This is a rewrite of {link.title}.</p>
  <div class="buttons">
    <button type="button" class="btn btn-primary" disabled={hint !== '' || busy} aria-describedby={hint !== '' ? 'rewrite-hint' : undefined} onclick={() => (confirmOpen = true)}>
      Replace {link.title} with this
    </button>
    <button type="button" class="btn btn-secondary" disabled={busy} onclick={keep}>Keep both</button>
  </div>
  {#if hint !== ''}<p id="rewrite-hint" class="field-help">{hint}</p>{/if}
</div>

{#if refusal !== ''}
  <div class="banner banner-error lead" role="alert">
    <p>{refusal}</p>
  </div>
{/if}

<ConfirmDialog
  open={confirmOpen}
  title={words.title}
  body={words.body}
  confirmLabel={words.confirm}
  {busy}
  onconfirm={replace}
  oncancel={() => (confirmOpen = false)}
/>

<style>
  .lead {
    margin-top: var(--space-4);
    max-width: var(--measure-form);
  }

  .rewrite {
    display: grid;
    gap: var(--space-3);
    justify-items: start;
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
</style>
