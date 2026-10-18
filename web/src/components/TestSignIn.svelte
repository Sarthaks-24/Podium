<!--
  TestSignIn: the sign-in form used instead of "Continue with Google" when
  the server runs in testing mode (decision D48). It asks for an email, an
  optional name and a role, posts them to /api/auth/test-signin, reloads the
  session and goes to the page the visitor was heading for. Errors from the
  server appear inline under the form. Props: next, the path to open after
  signing in.
-->
<script lang="ts">
  import { ApiError, sendJSON } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import { loadSession } from '../lib/session.svelte';

  let { next }: { next: string } = $props();

  let email = $state('');
  let name = $state('');
  let role = $state<'student' | 'admin' | 'owner'>('student');
  let busy = $state(false);
  let error = $state('');

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    try {
      const res = await sendJSON<{ next: string }>('POST', '/api/auth/test-signin', { email, name, role, next });
      const failed = await loadSession();
      if (failed) throw failed;
      navigate(res.next || '/');
    } catch (err) {
      error = err instanceof ApiError ? err.message : 'Signing in failed. Try again.';
    } finally {
      busy = false;
    }
  }
</script>

<form class="test-signin" onsubmit={submit} novalidate>
  <div class="field">
    <label for="test-email">Email</label>
    <input
      id="test-email"
      class="input"
      type="email"
      name="email"
      autocomplete="email"
      spellcheck="false"
      required
      placeholder="asha@college.edu"
      bind:value={email}
      aria-invalid={error !== '' ? 'true' : undefined}
      aria-describedby={error !== '' ? 'test-error' : undefined}
    />
  </div>
  <div class="field">
    <label for="test-name">Name <span class="muted">(optional)</span></label>
    <input id="test-name" class="input" type="text" name="name" autocomplete="name" bind:value={name} />
  </div>
  <div class="field">
    <label for="test-role">Role</label>
    <select id="test-role" class="select" name="role" bind:value={role}>
      <option value="student">Student</option>
      <option value="admin">Admin</option>
      <option value="owner">Owner</option>
    </select>
  </div>
  {#if error !== ''}
    <p id="test-error" class="field-error" role="alert">{error}</p>
  {/if}
  <p>
    <button class="btn btn-primary" type="submit" disabled={busy}>{busy ? 'Signing in' : 'Sign in for testing'}</button>
  </p>
</form>

<style>
  .test-signin {
    display: grid;
    gap: var(--space-4);
    max-width: 24rem;
    margin-top: var(--space-5);
  }
</style>
