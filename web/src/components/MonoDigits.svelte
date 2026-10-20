<!--
  MonoDigits: text in which only the numbers are set in Fragment Mono, so
  "240 of 300", "21 Oct 2026 00:11" and "2 d 23 h" keep their words in the
  interface face (v2 S2, audit 01-19 and 2026-10-20 change 5;
  lib/format.ts numberParts and scoreParts). Props: `parts`, a list of
  { text, mono }, or `text`, which is split with numberParts. A date is
  <MonoDigits text={formatDateTime(ms)} />. The parts sit in one inline
  span, so inside a flex container such as a Badge they stay one item and
  keep the spaces between words and digits (a bare text node beside a span
  in a flex box loses its edge spaces).
-->
<script lang="ts">
  import { numberParts, type TextPart } from '../lib/format';

  let { parts, text = '' }: { parts?: TextPart[]; text?: string } = $props();

  const shown = $derived(parts ?? numberParts(text));
</script>

<span class="mono-digits">{#each shown as p, i (i)}{#if p.mono}<span class="mono">{p.text}</span>{:else}{p.text}{/if}{/each}</span>
