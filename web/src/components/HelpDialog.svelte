<!--
  HelpDialog: the one Help dialog (cache/v3/design/editor.md section 1), in
  a native modal dialog. It holds the contest's rules when a contest page
  has put them in `shell.rules`, then Run and Submit in two lines, then the
  keyboard shortcuts (lib/shortcuts.ts). The shell opens it when "?" is
  pressed outside a text field, or from the footer and the account menu;
  the contest's "Rules" opens it too. Escape and Close shut it, and the
  browser returns focus to whatever had it before, or to the page heading
  when that element was replaced while it was open (components/Dialog).
  Focus starts on the
  heading, so a screen reader reads the title first. Organisers also see
  the problem editor and reveal keys. Props: `open`, `organiser` and
  `onclose`.
-->
<script lang="ts">
  import { shell } from '../lib/shell.svelte';
  import { isMacPlatform, shortcutGroups } from '../lib/shortcuts';
  import Dialog from './Dialog.svelte';

  interface Props {
    open: boolean;
    organiser: boolean;
    onclose: () => void;
  }

  let { open, organiser, onclose }: Props = $props();

  const groups = $derived(shortcutGroups({ mac: isMacPlatform(), organiser }));
</script>

<Dialog {open} {onclose} title="Help">
  {#if shell.rules}
    <section class="group" aria-labelledby="help-rules">
      <h3 id="help-rules">Rules</h3>
      <ul class="rules">
        {#each shell.rules as line, i (i)}<li>{line}</li>{/each}
      </ul>
    </section>
  {/if}
  <section class="group" aria-labelledby="help-run">
    <h3 id="help-run">Run and Submit</h3>
    <ul class="rules">
      <li>Run tries your code on the examples, or on your own input. It never counts.</li>
      <li>Submit grades your code on the hidden tests. Your score on a problem is the best result per subtask across your submissions.</li>
    </ul>
  </section>
  <h3 class="keys-title">Keyboard shortcuts</h3>
  {#each groups as g (g.title)}
    <section class="group" aria-labelledby="shortcuts-{g.title.toLowerCase().replace(/\s+/g, '-')}">
      <h4 id="shortcuts-{g.title.toLowerCase().replace(/\s+/g, '-')}">{g.title}</h4>
      <dl>
        {#each g.items as item (item.action)}
          <div class="row">
            <dt>
              {#each item.keys as k, i (k)}{#if i > 0}<span class="joiner">{item.joiner ?? 'or'}</span>{/if}<kbd>{k}</kbd>{/each}
            </dt>
            <dd>{item.action}</dd>
          </div>
        {/each}
      </dl>
    </section>
  {/each}
</Dialog>

<style>
  .group {
    margin-top: var(--space-5);
  }

  h3 {
    font-size: var(--fs-1);
  }

  .keys-title {
    margin-top: var(--space-6);
    font-size: var(--fs-2);
  }

  .group h4 {
    font-size: var(--fs-1);
  }

  .rules {
    display: grid;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding-left: var(--space-5);
  }

  dl {
    margin-top: var(--space-2);
    border-top: var(--rule) solid var(--line);
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 13rem) minmax(0, 1fr);
    gap: var(--space-2) var(--space-4);
    padding-block: var(--space-2);
    border-bottom: var(--rule) solid var(--line);
  }

  dt {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-2);
  }

  dd {
    margin: 0;
    color: var(--ink-muted);
  }

  .joiner {
    color: var(--ink-muted);
    font-size: var(--fs-0);
  }

  kbd {
    padding: 0 var(--space-1);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-2);
    font-family: var(--font-mono);
    font-size: var(--fs-0);
    white-space: nowrap;
  }

  @media (width < 768px) {
    .row {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--space-1);
    }
  }
</style>
