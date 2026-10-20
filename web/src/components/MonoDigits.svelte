<!--
  MonoDigits: text in which only the numbers are set in Fragment Mono, so
  "240 of 300" and "2 d 23 h" keep their words in the interface face (v2
  S2, audit 01-19; lib/format.ts numberParts and scoreParts). Props:
  `parts`, a list of { text, mono }.
-->
<script lang="ts">
  import type { TextPart } from '../lib/format';

  let { parts }: { parts: TextPart[] } = $props();
</script>

{#each parts as p, i (i)}{#if p.mono}<span class="mono">{p.text}</span>{:else}{p.text}{/if}{/each}
