<!--
  Me: the /me page (v4 M1), the signed-in person's own profile, for every
  role and for nobody else. Top: the Google picture (the first image in
  the product, never sent a referrer) with the person's initials while it
  loads or when it fails, the name, then email, role, member since and
  last sign-in (GET /api/me/profile). Below: the theme and colours
  (ThemePicker, the same control as the account menu's dialog) and the
  code editor's preferences (EditorSettingsFields, the same fields as the
  workspace's Settings), which are kept on this device. Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import EditorSettingsFields from '../components/EditorSettingsFields.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import PageState from '../components/PageState.svelte';
  import ThemePicker from '../components/ThemePicker.svelte';
  import { getJSON } from '../lib/api';
  import { FONT_SIZES, draftStorage, readEditorPrefs, writeEditorPrefs, type EditorPrefs } from '../lib/drafts';
  import { formatDateTime, roleLabel } from '../lib/format';
  import { setTitle } from '../lib/head';
  import { loader } from '../lib/loader.svelte';
  import { routeTitle } from '../lib/routing';
  import { displayName, session } from '../lib/session.svelte';

  interface Profile {
    member_since: number;
    last_sign_in: number;
  }

  const user = $derived(session.user!);
  let profile = $state<Profile | null>(null);
  const page = loader(
    () => getJSON<Profile>('/api/me/profile'),
    (p) => (profile = p),
  );

  // The picture shows only once it has loaded; until then, or if it fails, the initials do.
  let pictureLoaded = $state(false);
  let pictureFailed = $state(false);
  const initials = $derived(
    displayName(user)
      .split(/[\s@._-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0].toUpperCase())
      .join(''),
  );

  const storage = draftStorage();
  let editor = $state<EditorPrefs>(readEditorPrefs(storage, session.user!.id));
  function changeEditor(next: Partial<EditorPrefs>) {
    editor = { ...editor, ...next };
    writeEditorPrefs(storage, editor, user.id);
  }

  onMount(() => {
    setTitle(routeTitle('me'));
    void page.load();
    return () => page.stop();
  });
</script>

<PageHeader title="Your profile" />

<section class="who" aria-labelledby="who-name">
  <div class="avatar" aria-hidden="true">
    {#if !pictureLoaded}<span class="initials">{initials}</span>{/if}
    {#if user.avatar_url && !pictureFailed}
      <img
        src={user.avatar_url}
        alt=""
        width="48"
        height="48"
        referrerpolicy="no-referrer"
        class:hidden={!pictureLoaded}
        onload={() => (pictureLoaded = true)}
        onerror={() => (pictureFailed = true)}
      />
    {/if}
  </div>
  <div class="facts">
    <h2 id="who-name" class="name">{displayName(user)}</h2>
    <dl>
      <dt>Email</dt>
      <dd>{user.email}</dd>
      <dt>Role</dt>
      <dd>{roleLabel(user.role)}</dd>
      {#if profile}
        <dt>Member since</dt>
        <dd>{formatDateTime(profile.member_since)}</dd>
        <dt>Last sign-in</dt>
        <dd>{profile.last_sign_in ? formatDateTime(profile.last_sign_in) : 'Not yet'}</dd>
      {/if}
    </dl>
    {#if page.state === 'loading'}
      <PageState kind="loading" message="Loading your dates" />
    {:else if page.state !== 'ready'}
      <PageState kind="error" message="Your member and sign-in dates could not load." onretry={() => void page.load()} />
    {/if}
  </div>
</section>

<section class="prefs" aria-labelledby="theme-heading">
  <h2 id="theme-heading">Theme and colours</h2>
  <p class="muted small">Saved with your account, so they follow you to any computer.</p>
  <div class="picker"><ThemePicker /></div>
</section>

<section class="prefs" aria-labelledby="editor-heading">
  <h2 id="editor-heading">Code editor</h2>
  <p class="muted small">Kept on this device, like the editor's own Settings.</p>
  <div class="fields">
    <EditorSettingsFields
      fontSize={editor.fontSize}
      sizes={FONT_SIZES}
      tabSize={editor.tabSize}
      wrap={editor.wrap}
      tabMovesFocus={editor.tabMovesFocus}
      idPrefix="me"
      onchange={changeEditor}
    />
  </div>
</section>

<style>
  .who {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
    max-width: var(--measure-form);
    margin-top: var(--space-5);
  }

  /* One size: the picture or the initials sit in the same 48 px square. */
  .avatar {
    position: relative;
    display: grid;
    flex: none;
    place-items: center;
    width: var(--space-7);
    height: var(--space-7);
    overflow: hidden;
    border-radius: 50%;
    background: var(--surface-2);
    color: var(--ink);
    font-weight: 600;
  }

  .avatar img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .avatar img.hidden {
    visibility: hidden;
  }

  .facts {
    min-width: 0;
  }

  .name {
    font-size: var(--fs-3);
    overflow-wrap: anywhere;
  }

  dl {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-1) var(--space-4);
    margin-top: var(--space-3);
  }

  dt {
    color: var(--ink-muted);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .prefs {
    max-width: var(--measure-form);
    margin-top: var(--space-7);
    padding-top: var(--space-5);
    border-top: var(--rule) solid var(--line);
  }

  .prefs h2 {
    font-size: var(--fs-2);
  }

  .picker,
  .fields {
    display: grid;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  /* Short choices: a select as wide as the column reads as a text field. */
  .fields {
    max-width: 20rem;
  }
</style>
