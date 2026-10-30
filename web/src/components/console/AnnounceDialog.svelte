<!--
  AnnounceDialog: an announcement to every student of the contest, from
  the console. Start from a template or write it, see
  the preview, then "Post announcement" (POST .../announcements). The
  dialog is the confirmation: it says that every student sees it at once
  and that it cannot be taken back. After Pause, Resume, Extend and Judge
  again the page opens it with a template filled in; nothing is posted by
  itself. The announcements posted so far are listed under the form.

  Props: `open`, `base` (the contest's admin API path), `announcements`
  (newest first), `labels` (problem letters, for the templates),
  `body` and `template` (bindable, so a draft survives closing), `onclose`
  and `onposted`.
-->
<script lang="ts">
  import Dialog from '../Dialog.svelte';
  import Markdown from '../Markdown.svelte';
  import MonoDigits from '../MonoDigits.svelte';
  import { ApiError, errorText, sendJSON } from '../../lib/api';
  import { ANNOUNCEMENT_TEMPLATES, fillTemplate, type Announcement } from '../../lib/contests';
  import { ticker } from '../../lib/ticker.svelte';
  import { formatWhen } from '../../lib/workspace';

  interface Props {
    open: boolean;
    base: string;
    announcements: readonly Announcement[];
    labels: readonly string[];
    body: string;
    template: string;
    onclose: () => void;
    onposted: () => Promise<void> | void;
  }

  let { open, base, announcements, labels, body = $bindable(), template = $bindable(), onclose, onposted }: Props = $props();

  let error = $state('');
  let busy = $state(false);

  $effect(() => {
    if (open) error = '';
  });

  function useTemplate() {
    const t = ANNOUNCEMENT_TEMPLATES.find((x) => x.id === template);
    if (t) body = fillTemplate(t.text, { label: labels[0] ?? 'A', minutes: 10 });
  }

  async function post(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    if (body.trim() === '') {
      error = 'Write the announcement first.';
      document.getElementById('ann-body')?.focus();
      return;
    }
    busy = true;
    try {
      await sendJSON<unknown>('POST', `${base}/announcements`, { body_md: body.trim() });
      body = '';
      template = '';
      await onposted();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      error = e instanceof ApiError ? (e.fields.body_md ?? e.message) : errorText(e);
      document.getElementById('ann-body')?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {onclose} {busy} title="Announce to every student" closeLabel="Close">
  <form class="form" novalidate onsubmit={post}>
    <div class="field">
      <label for="ann-template">Start from a template</label>
      <select id="ann-template" class="select" bind:value={template} onchange={useTemplate}>
        <option value="">No template</option>
        {#each ANNOUNCEMENT_TEMPLATES as t (t.id)}<option value={t.id}>{t.label}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="ann-body">Announcement (Markdown)</label>
      <textarea
        id="ann-body"
        aria-required="true"
        class="input"
        rows="3"
        bind:value={body}
        aria-invalid={error ? 'true' : undefined}
        aria-describedby="ann-help{error ? ' ann-error' : ''}"
      ></textarea>
      <p id="ann-help" class="field-help">Every student sees it at once, as a banner over their workspace. It cannot be taken back.</p>
      {#if error}<p id="ann-error" class="field-error">{error}</p>{/if}
    </div>
    {#if body.trim() !== ''}
      <div class="preview" role="group" aria-label="Preview">
        <p class="small muted">Preview</p>
        <Markdown source={body} compact />
      </div>
    {/if}
    <div class="actions">
      <button type="submit" class="btn btn-primary" disabled={busy}>Post announcement</button>
      <button type="button" class="btn btn-secondary" disabled={busy} onclick={onclose}>Cancel</button>
    </div>
  </form>
  {#if announcements.length > 0}
    <h3 class="sub">Posted</h3>
    <ul class="items">
      {#each announcements as a (a.id)}
        <li><p class="muted small"><MonoDigits text={formatWhen(a.created_at, ticker.now)} /></p><Markdown source={a.body_md} compact /></li>
      {/each}
    </ul>
  {/if}
</Dialog>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
    margin-top: var(--space-3);
  }

  .preview {
    padding: var(--space-3);
    border: var(--rule) dashed var(--line-strong);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .sub {
    margin-top: var(--space-5);
    font-size: var(--fs-sub);
  }

  .items {
    display: grid;
    margin-top: var(--space-2);
    padding: 0;
    list-style: none;
    border-top: var(--rule) solid var(--line);
  }

  .items > li {
    display: grid;
    gap: var(--space-1);
    padding-block: var(--space-3);
    border-bottom: var(--rule) solid var(--line);
    font-size: var(--fs-0);
  }
</style>
