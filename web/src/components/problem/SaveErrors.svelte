<!--
  SaveErrors: why the problem editor's last save was refused, under the
  sticky bar so it shows on every tab: the sentence, then a link per field
  that needs attention. A link calls `onreveal` with the field's id, which
  switches to the field's tab before focusing it. Props: `saveError`,
  `fieldList` (lib/problemFields FieldError) and `onreveal`.
-->
<script lang="ts">
  import type { FieldError } from '../../lib/problemFields';

  interface Props {
    saveError: string;
    fieldList: FieldError[];
    onreveal: (id: string) => void;
  }

  let { saveError, fieldList, onreveal }: Props = $props();

  function go(event: MouseEvent, id: string | null) {
    if (!id || !document.getElementById(id)) return;
    event.preventDefault();
    onreveal(id);
  }
</script>

{#if saveError !== ''}
  <div id="field-errors" class="banner banner-error problems-list" role="alert" tabindex="-1">
    <p>{saveError}</p>
    {#if fieldList.length > 0}
      <ul>
        {#each fieldList as f (f.key)}
          <li>
            {#if f.id}
              <a href="#{f.id}" onclick={(e) => go(e, f.id)}>{f.label}: {f.message}</a>
            {:else}
              {f.label ? `${f.label}: ` : ''}{f.message}
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}
