<!--
  Landing: what a signed-out visitor sees at / (v4 P1, V4-6 and V4-8).
  What Podium is for at this college, who uses it, how a contest works in
  three plain lines, and the way in: "Sign in with your college Google
  account". It loads no data and needs no script to read, so the build
  prerenders it to dist/landing.html (scripts/prerender.mjs), and inside
  the app it is the signed-out home. The privacy link (once approved) and
  the organisers' contact sit in the app shell's footer; the prerendered
  copy carries only the privacy link, since the contact is runtime data.
  Left-aligned, one column, no animation (design.md section 2).
  Takes no props.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { setTitle } from '../lib/head';

  onMount(() => setTitle('Programming contests'));
</script>

<div class="landing">
  <PageHeader title="Programming contests at this college" />
  <p class="lead">
    Podium is where students take the college's programming contests and where organisers set them. Everything runs in the browser: there is
    nothing to install.
  </p>
  <p class="way-in"><a class="btn btn-primary" href="/signin">Sign in with your college Google account</a></p>

  <section aria-labelledby="who-students">
    <h2 id="who-students">For students</h2>
    <p>
      Open a contest when it starts, read each problem and write your code in the browser. Run tries it on the examples and never counts. Submit
      grades it on hidden tests.
    </p>
  </section>

  <section aria-labelledby="who-organisers">
    <h2 id="who-organisers">For organisers</h2>
    <p>
      Write problems with examples and hidden tests, check them against a reference solution, then run the contest from the Live view while the
      standings fill in.
    </p>
  </section>

  <section aria-labelledby="how">
    <h2 id="how">How a contest works</h2>
    <ul class="steps">
      <li>It opens at its start time. You press Start and agree to one honour line.</li>
      <li>Each problem is scored by subtasks, and your best result on each subtask counts.</li>
      <li>The standings hide results from the last minutes until the end, then show them all.</li>
    </ul>
  </section>
</div>

<style>
  .landing {
    max-width: var(--measure-prose);
  }

  .lead {
    font-size: var(--fs-2);
    line-height: var(--lh-body);
    margin-top: var(--space-3);
  }

  .way-in {
    margin-block: var(--space-5) var(--space-7);
  }

  /* The one long button in the product: it wraps at 320 px instead of pushing the page sideways. */
  .way-in .btn {
    height: auto;
    min-height: var(--control-h);
    white-space: normal;
    text-align: left;
  }

  section + section {
    margin-top: var(--space-6);
  }

  section h2 {
    font-size: var(--fs-2);
  }

  section p,
  .steps {
    margin-top: var(--space-2);
  }

  /* Plain lines, no numbers or icons: three facts, not a process diagram. */
  .steps {
    display: grid;
    gap: var(--space-2);
    padding-left: var(--space-5);
  }
</style>
