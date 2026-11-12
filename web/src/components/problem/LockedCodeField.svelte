<!--
  LockedCodeField: whether students write the whole program or only a
  function (v2.0.2), on the editor's Statement tab under the starter code.
  For a function problem, one language at a time: the locked top (common
  imports) and the locked bottom (reads the input, calls the student's
  function, prints the result). The judge compiles top, the student's code
  and bottom as one file; the starter for the language is the function a
  student fills in. A language is offered only with all three. The mode
  cannot change once a student has a submission or a draft; the server
  refuses it then. Folded like the starter code, and opened on a field
  error. Shown only when the server knows about function problems. A function
  problem also shows the Languages panel (a snippet from the editor) under the
  mode choice, and the summary names the offered languages that were never
  checked. Props: `draft` (bindable), `fields` (the field errors by key),
  `languages` (the panel) and `unchecked` (names of offered languages that
  were never checked, or "").
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { layoutDoc } from '../../lib/lockedCode';
  import type { Draft } from '../../lib/problems';

  interface Props {
    draft: Draft;
    fields: Record<string, string>;
    languages?: Snippet;
    unchecked?: string;
  }

  let { draft = $bindable(), fields, languages, unchecked = '' }: Props = $props();

  const LANGUAGES = [
    { id: 'cpp17', label: 'C++17' },
    { id: 'java21', label: 'Java 21' },
    { id: 'python3', label: 'Python 3' },
    { id: 'node22', label: 'JavaScript (Node 22)' },
  ] as const;

  let language = $state<string>('cpp17');
  let open = $state(false);
  $effect(() => {
    if (fields.mode || fields.locked_top || fields.locked_bottom) open = true;
  });
  const offered = $derived(LANGUAGES.filter((l) => (draft.locked_bottom?.[l.id] ?? '').trim() !== '').map((l) => l.label));
  const label = $derived(LANGUAGES.find((l) => l.id === language)?.label ?? language);

  // Imports most solutions need; the top holds imports only (no declarations).
  const STANDARD_IMPORTS: Record<string, string[]> = {
    cpp17: ['#include <bits/stdc++.h>', 'using namespace std;'],
    java21: ['import java.util.*;', 'import java.io.*;'],
    python3: ['import sys', 'import math'],
    node22: ["'use strict';"],
  };

  // Adds only the standard lines the top lacks, each after the standard line that comes before it.
  function addStandardImports() {
    const lines = (draft.locked_top?.[language] ?? '').split('\n');
    if (lines.at(-1) === '') lines.pop();
    let after = -1;
    for (const line of STANDARD_IMPORTS[language] ?? []) {
      const at = lines.findIndex((l) => l.trim() === line);
      if (at >= 0) after = at;
      else lines.splice(++after, 0, line);
    }
    write('locked_top', lines.length > 0 ? `${lines.join('\n')}\n` : '');
  }

  // The server names a language by its id; the page says it as the label and shows that language.
  const named = (message: string | undefined) => LANGUAGES.find((l) => message !== undefined && message.includes(l.id));
  const said = (message: string) => LANGUAGES.reduce((text, l) => text.replaceAll(l.id, l.label), message);
  $effect(() => {
    const l = named(fields.locked_bottom) ?? named(fields.locked_top);
    if (l) language = l.id;
  });

  // What a student sees, joined by the editor's own join (lib/lockedCode.ts).
  const preview = $derived.by(() => {
    const top = draft.locked_top?.[language] ?? '';
    const bottom = draft.locked_bottom?.[language] ?? '';
    const l = layoutDoc(top, draft.starters?.[language] ?? '', bottom);
    return { top: l.doc.slice(0, l.topEnd), code: l.doc.slice(l.topEnd, l.bottomStart < 0 ? undefined : l.bottomStart), bottom: l.bottomStart < 0 ? '' : l.doc.slice(l.bottomStart) };
  });

  function write(part: 'locked_top' | 'locked_bottom', code: string) {
    draft[part] = { ...(draft[part] ?? {}), [language]: code };
  }
</script>

{#if draft.mode !== undefined}
  <details class="fold" bind:open>
    <summary>
      What students write<span class="set"
        >, {draft.mode === 'function' ? `a function${offered.length > 0 ? ` in ${offered.join(', ')}` : ''}` : 'the whole program'}{draft.mode === 'function' && unchecked !== '' ? ` (Not checked yet: ${unchecked})` : ''}</span
      >
    </summary>
    <fieldset class="mode">
      <legend class="visually-hidden">What students write</legend>
      <div class="choices" role="radiogroup" aria-describedby={fields.mode ? 'mode-error' : undefined}>
        <label class="choice"><input type="radio" name="problem-mode" value="whole" bind:group={draft.mode} /> The whole program</label>
        <label class="choice"><input type="radio" name="problem-mode" value="function" bind:group={draft.mode} /> One function, between locked code</label>
      </div>
      {#if fields.mode}<p id="mode-error" class="field-error">{fields.mode}</p>{/if}
      <p class="muted small">The mode cannot change once a student has a submission or a draft. Make a new problem instead.</p>

      {#if draft.mode === 'function'}
        <p class="muted small" id="locked-help">
          The judge runs the locked top, the student's code and the locked bottom as one file. Students see the locked code but cannot
          change it. The starter for each language is the function they fill in. A language is offered only when it has all three.
        </p>
        {@render languages?.()}
        <div class="field lang">
          <label for="locked-language">Language</label>
          <select id="locked-language" class="select" bind:value={language}>
            {#each LANGUAGES as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
          </select>
        </div>
        <div class="field">
          <label for="locked-top">Locked top in {label}</label>
          <textarea
            id="locked-top"
            class="input mono code"
            rows="4"
            spellcheck="false"
            translate="no"
            value={draft.locked_top?.[language] ?? ''}
            oninput={(e) => write('locked_top', e.currentTarget.value)}
            aria-describedby="locked-top-help{fields.locked_top ? ' locked-top-error' : ''}"
            aria-invalid={fields.locked_top ? 'true' : undefined}
          ></textarea>
          <p id="locked-top-help" class="field-help">Common imports or includes only, no declarations.</p>
          <button type="button" class="btn small" onclick={addStandardImports}>Add standard imports</button>
          {#if fields.locked_top}<p id="locked-top-error" class="field-error">{said(fields.locked_top)}</p>{/if}
        </div>
        <div class="field">
          <label for="locked-bottom">Locked bottom in {label}</label>
          <textarea
            id="locked-bottom"
            class="input mono code"
            rows="8"
            spellcheck="false"
            translate="no"
            value={draft.locked_bottom?.[language] ?? ''}
            oninput={(e) => write('locked_bottom', e.currentTarget.value)}
            aria-describedby="locked-bottom-help{fields.locked_bottom ? ' locked-bottom-error' : ''}"
            aria-invalid={fields.locked_bottom ? 'true' : undefined}
          ></textarea>
          <p id="locked-bottom-help" class="field-help">
            Reads the input, calls the student's function and prints the result. In Java this holds public class Main, and the
            student writes class Solution.
          </p>
          {#if fields.locked_bottom}<p id="locked-bottom-error" class="field-error">{said(fields.locked_bottom)}</p>{/if}
        </div>
        <div class="field">
          <p class="preview-label" id="locked-preview-label">What students see in {label}, with the starter between the locked parts</p>
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <div class="preview" role="region" aria-labelledby="locked-preview-label" tabindex="0">
            <pre class="mono" translate="no"><span class="locked">{preview.top}</span>{preview.code}<span class="locked">{preview.bottom}</span></pre>
          </div>
        </div>
      {/if}
    </fieldset>
  </details>
{/if}

<style>
  .fold {
    margin-top: var(--space-5);
  }

  .fold > summary {
    width: fit-content;
    border-radius: var(--radius);
    color: var(--act);
    font-weight: 500;
    cursor: pointer;
  }

  .set {
    color: var(--ink-muted);
    font-weight: 400;
  }

  .mode {
    display: grid;
    gap: var(--space-3);
    margin: var(--space-3) 0 0;
    padding: 0;
    border: 0;
  }

  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-5);
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    color: var(--ink);
    font-weight: 400;
    cursor: pointer;
  }

  .choice input {
    width: 1rem;
    height: 1rem;
    margin: 0;
    accent-color: var(--act);
  }

  .preview-label {
    color: var(--ink);
    font-weight: 500;
  }

  .preview {
    max-height: 16rem;
    margin: var(--space-1) 0 0;
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--rule) solid var(--line);
    background: var(--surface-2);
    color: var(--ink);
    font-size: var(--fs-0);
  }

  .preview pre {
    margin: 0;
    white-space: pre;
  }

  .locked {
    color: var(--ink-muted);
  }

  .lang {
    max-width: 16rem;
  }

  .code {
    min-height: 6rem;
    resize: vertical;
    tab-size: 4;
  }
</style>
