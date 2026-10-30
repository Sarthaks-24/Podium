<!--
  Badge: one verdict or state word on a tint, such as "Accepted", "Partial, 40 of 100", "Live",
  "Validated" or "Draft". Sentence case, never an icon or a bare count;
  the styles are .badge in styles/controls.css. Props: `tone` (from
  lib/badges.ts: act, ok, part, bad, wait, muted or neutral; neutral by
  default), and `text` or children for the words. Digits inside go
  through MonoDigits when they are numbers.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { BadgeTone } from '../lib/badges';

  let { tone = 'neutral', text = '', children }: { tone?: BadgeTone; text?: string; children?: Snippet } = $props();
</script>

<span class="badge" data-tone={tone}>{#if children}{@render children()}{:else}{text}{/if}</span>
