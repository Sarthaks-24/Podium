<!--
  Icon: one Lucide icon from lib/icons.ts (design.md section 9) drawn
  inline with a 1.5 stroke in the current text colour, --icon-size (20 px)
  on its own and the words' size inside a button. Icons sit beside words,
  never instead of them, so the SVG is hidden from assistive technology;
  the control it sits in carries the name. Props: `name`, an IconName.
-->
<script lang="ts">
  import { ICONS, type IconName } from '../lib/icons';

  let { name }: { name: IconName } = $props();
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
  {#each ICONS[name] as shape, i (i)}
    {#if shape.tag === 'path'}
      <path d={shape.d} />
    {:else if shape.tag === 'circle'}
      <circle cx={shape.cx} cy={shape.cy} r={shape.r} />
    {:else}
      <rect x={shape.x} y={shape.y} width={shape.width} height={shape.height} rx={shape.rx} />
    {/if}
  {/each}
</svg>

<style>
  .icon {
    flex: none;
    width: var(--icon-size);
    height: var(--icon-size);
  }

  /* Inside a button the icon follows the words, as before v2. */
  :global(.btn) .icon {
    width: 1.125em;
    height: 1.125em;
  }
</style>
