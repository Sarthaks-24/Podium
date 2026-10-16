<!--
  ThemeToggle: a text button that cycles the colour theme through System,
  Light and Dark. Its visible text states the current choice ("Theme:
  System"); after each press a visually hidden status line announces the new
  theme, because screen readers do not reliably read a button's changed
  name. The choice is applied to <html> and saved in localStorage through
  lib/theme.ts; when storage is blocked the button still works for the
  current visit. Takes no props.
-->
<script lang="ts">
  import { applyChoice, choiceLabel, getStorage, nextChoice, readChoice, saveChoice } from './theme';

  const storage = getStorage();
  let choice = $state(readChoice(storage));
  let announcement = $state('');

  function cycle() {
    choice = nextChoice(choice);
    applyChoice(document.documentElement, choice);
    saveChoice(storage, choice);
    announcement = `Theme set to ${choiceLabel(choice)}`;
  }
</script>

<button type="button" class="theme" onclick={cycle}>
  Theme: {choiceLabel(choice)}
</button>
<span class="visually-hidden" role="status">{announcement}</span>

<style>
  .theme {
    min-height: var(--control-h);
    padding-inline: var(--space-3);
    border: var(--rule) solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--ink);
    font-size: var(--fs-0);
    font-weight: 500;
    transition: border-color var(--dur-fast) var(--ease);
  }

  .theme:hover {
    border-color: var(--ink-muted);
  }
</style>
