<!--
  Icon: one Lucide icon (design.md section 9) drawn inline at 1em with a
  1.5 stroke in the current text colour. Icons are decorative here, so the
  SVG is hidden from assistive technology; the control it sits in carries
  the words. Props: `name` (sun, moon, copy or check).
-->
<script lang="ts">
  let { name }: { name: 'sun' | 'moon' | 'copy' | 'check' } = $props();
</script>

<svg
  class="icon"
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.5"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {#if name === 'sun'}
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2m0 16v2M4.93 4.93l1.41 1.41m11.32 11.32l1.41 1.41M2 12h2m16 0h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
  {:else if name === 'moon'}
    <path d="M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401" />
  {:else if name === 'copy'}
    <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
    <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
  {:else}
    <path d="M20 6L9 17l-5-5" />
  {/if}
</svg>

<style>
  .icon {
    flex: none;
    width: 1.125em;
    height: 1.125em;
  }
</style>
