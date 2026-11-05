<!--
  ContactLine: the contest's own contact, typed by its organisers, as one
  sentence with a mailto link. Shown on the contest page and where a
  student meets a closed problem. Never an account email. Renders
  nothing when `email` is empty or missing. Props: `email`, `class`
  (the paragraph's classes).
-->
<script lang="ts">
  interface Props {
    email?: string;
    class?: string;
  }

  let { email = '', class: cls = 'muted small' }: Props = $props();
</script>

{#if email}<p class={cls}>Questions about this contest? Email <a href="mailto:{email}">{email}</a></p>{/if}
