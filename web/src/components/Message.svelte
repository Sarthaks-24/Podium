<!--
  Message: renders a message built by lib/signin.ts as text, with every
  email part shown as a mailto link. Used so that copy with links never goes
  through {@html}. Props: `parts`, the list of text and email parts.
-->
<script lang="ts">
  import type { MessagePart } from '../lib/signin';

  let { parts }: { parts: readonly MessagePart[] } = $props();
</script>

{#each parts as part, i (i)}{#if typeof part === 'string'}{part}{:else}<a href="mailto:{part.email}">{part.email}</a>{/if}{/each}
