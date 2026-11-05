<!--
  PackPromptPanel: "Prompt for another AI" in the problem bank's Import
  pack area. The button opens a read-only box holding the prompt the server
  builds from its real pack format (GET /api/admin/problems/pack-prompt),
  a Copy button with a status line, and four reminders: write the problem
  in your own words, keep unreleased problems and student material out of
  an outside AI, paste an import error list back into the chat, and
  Validate before a contest uses the problem. No props. The prompt is
  fetched once, on the first open.
-->
<script lang="ts">
  import { errorText, getJSON } from '../lib/api';
  import Icon from './Icon.svelte';

  let open = $state(false);
  let prompt = $state('');
  let loadError = $state('');
  let loading = $state(false);
  let status = $state('');

  async function toggle() {
    open = !open;
    if (!open || prompt !== '' || loading) return;
    loading = true;
    loadError = '';
    try {
      prompt = (await getJSON<{ prompt: string }>('/api/admin/problems/pack-prompt')).prompt;
    } catch (e) {
      loadError = errorText(e);
    } finally {
      loading = false;
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(prompt);
      status = 'Copied the prompt.';
    } catch {
      status = 'Copy did not work here. Select the text in the box and copy it yourself.';
    }
  }
</script>

<div class="prompt-area">
  <button type="button" class="btn btn-link" aria-expanded={open} aria-controls="pack-prompt-panel" onclick={toggle}>
    Prompt for another AI
  </button>
  {#if open}
    <section id="pack-prompt-panel" class="panel" aria-labelledby="pack-prompt-heading">
      <h2 id="pack-prompt-heading" class="panel-heading">Prompt for another AI</h2>
      <ul class="notes">
        <li>Write the problem in your own words.</li>
        <li>Do not paste unreleased contest problems or student material into an outside AI.</li>
        <li>If the import fails, paste its error list back into the chat.</li>
        <li>An imported problem still has to pass Validate before a contest can use it.</li>
      </ul>
      {#if loading}
        <p class="muted" role="status">Loading the prompt</p>
      {:else if loadError !== ''}
        <p class="banner banner-error" role="alert">{loadError}</p>
      {:else}
        <textarea class="input textarea-code prompt-text" readonly rows="14" aria-label="Prompt for another AI" value={prompt}></textarea>
        <div class="action-row">
          <button type="button" class="btn btn-secondary" onclick={copy}><Icon name="copy" />Copy prompt</button>
          <span class="muted" role="status">{status}</span>
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  .prompt-area {
    margin-top: var(--space-3);
  }

  .panel {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-3);
    max-width: var(--measure-wide);
  }

  .panel-heading {
    font-size: var(--fs-2);
  }

  .notes {
    padding-left: var(--space-5);
    display: grid;
    gap: var(--space-1);
  }

  .prompt-text {
    width: 100%;
  }
</style>
