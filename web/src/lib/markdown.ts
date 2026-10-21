/**
 * Statement rendering: Markdown with TeX maths, as students will see it.
 *
 * Loaded with a dynamic import only by the screens that show statements, so
 * marked and DOMPurify stay out of the main bundle. KaTeX, its stylesheet
 * and its self-hosted fonts are one more import (lib/katex.ts), made only
 * when a text holds maths (F8.2).
 *
 * The pipeline, in order:
 * 1. marked turns Markdown into HTML. Raw HTML in the source is escaped and
 *    shown as text, never passed through. A marked extension turns $...$
 *    into <span class="math" data-tex="..."> and a $$...$$ block into
 *    <div class="math math-block" data-tex="...">, with the TeX escaped, so
 *    maths survives sanitising as plain data. The TeX is also the
 *    placeholder's text, so a formula reads as its source until KaTeX
 *    arrives, or if it never does. Statement headings are moved down so
 *    "#" becomes the level the page asks for.
 * 2. DOMPurify sanitises that HTML: inline style attributes are forbidden,
 *    as are script, style, iframe, object, embed and form controls; links
 *    get rel="noopener noreferrer" and open in the same tab; images load
 *    lazily and decode off the main thread.
 * 3. After the sanitised HTML is in the page, renderMath() loads KaTeX when
 *    there is a placeholder and asks it to fill each one from its data-tex
 *    text, with trust off and errors shown inline instead of thrown.
 *    KaTeX's output is generated from text, so it is trusted.
 */
import DOMPurify from 'dompurify';
import { Marked, type TokenizerAndRendererExtension, type Tokens } from 'marked';

/** Escapes text for use inside HTML text or a double-quoted attribute. */
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

interface MathToken extends Tokens.Generic {
  type: 'mathBlock' | 'mathInline';
  raw: string;
  tex: string;
}

// A $$ block on its own lines.
const BLOCK_MATH = /^\$\$([\s\S]+?)\$\$[^\S\n]*(?:\n|$)/;
// Inline display maths, $$...$$ inside a paragraph.
const INLINE_DISPLAY = /^\$\$([\s\S]+?)\$\$/;
// Inline maths: no space just inside either dollar and no digit right after
// the closing one, so "costs $5 and $10" stays text.
const INLINE_MATH = /^\$(?!\s)((?:\\[\s\S]|[^\\$\n])+?)(?<!\s)\$(?!\d)/;
// The first dollar that is not escaped with a backslash.
const UNESCAPED_DOLLAR = /(?<!\\)\$/;

const mathBlock: TokenizerAndRendererExtension = {
  name: 'mathBlock',
  level: 'block',
  start(src: string) {
    const i = src.indexOf('$$');
    return i < 0 ? undefined : i;
  },
  tokenizer(src: string): MathToken | undefined {
    const m = BLOCK_MATH.exec(src);
    if (!m) return undefined;
    return { type: 'mathBlock', raw: m[0], tex: m[1].trim() };
  },
  renderer(token) {
    const tex = escapeHtml((token as MathToken).tex);
    return `<div class="math math-block" data-tex="${tex}">${tex}</div>\n`;
  },
};

const mathInline: TokenizerAndRendererExtension = {
  name: 'mathInline',
  level: 'inline',
  start(src: string) {
    const m = UNESCAPED_DOLLAR.exec(src);
    return m ? m.index : undefined;
  },
  tokenizer(src: string): MathToken | undefined {
    const display = INLINE_DISPLAY.exec(src);
    if (display) return { type: 'mathInline', raw: display[0], tex: display[1].trim(), display: true };
    const m = INLINE_MATH.exec(src);
    if (!m) return undefined;
    return { type: 'mathInline', raw: m[0], tex: m[1], display: false };
  },
  renderer(token) {
    const t = token as MathToken;
    const cls = t.display ? 'math math-display' : 'math';
    const tex = escapeHtml(t.tex);
    return `<span class="${cls}" data-tex="${tex}">${tex}</span>`;
  },
};

function makeMarked(headingBase: number): Marked {
  const shift = Math.max(0, Math.min(5, headingBase - 1));
  return new Marked({
    async: false,
    gfm: true,
    breaks: false,
    extensions: [mathBlock, mathInline],
    renderer: {
      // Raw HTML in a statement is shown as the text the author typed.
      html({ text }) {
        return escapeHtml(text);
      },
      heading({ tokens, depth }) {
        const level = Math.min(6, depth + shift);
        return `<h${level}>${this.parser.parseInline(tokens)}</h${level}>\n`;
      },
    },
  });
}

const FORBID_TAGS = ['script', 'style', 'iframe', 'frame', 'object', 'embed', 'form', 'input', 'button', 'textarea', 'select', 'option', 'link', 'meta', 'base'];

let hooked = false;

function purifier() {
  if (!hooked) {
    DOMPurify.addHook('afterSanitizeAttributes', (node) => {
      if (node.tagName === 'A') {
        node.removeAttribute('target');
        if (node.hasAttribute('href')) node.setAttribute('rel', 'noopener noreferrer');
      } else if (node.tagName === 'IMG') {
        node.setAttribute('loading', 'lazy');
        node.setAttribute('decoding', 'async');
      }
    });
    hooked = true;
  }
  return DOMPurify;
}

/**
 * Sanitises an HTML string with the statement rules: no inline styles, no
 * scripts, frames, embeds or form controls, no event handlers or
 * javascript: URLs, and safe link relations.
 */
export function sanitizeHtml(html: string): string {
  return purifier().sanitize(html, {
    FORBID_ATTR: ['style', 'target', 'srcset', 'formaction'],
    FORBID_TAGS,
    ALLOW_DATA_ATTR: true,
  });
}

/** Options for statementHtml() and renderStatement(). */
export interface StatementOptions {
  /** The heading level a "#" heading in the source becomes; default 2. */
  headingBase?: number;
}

/**
 * Turns statement Markdown into sanitised HTML with maths left as
 * placeholders for renderMath().
 */
export function statementHtml(markdown: string, options: StatementOptions = {}): string {
  const html = makeMarked(options.headingBase ?? 2).parse(markdown) as string;
  return sanitizeHtml(html);
}

type Katex = typeof import('./katex').default;
let katexLoad: Promise<Katex> | null = null;

/** KaTeX, imported once; a failed import is tried again on the next render. */
function loadKatex(): Promise<Katex> {
  katexLoad ??= import('./katex').then(
    (mod) => mod.default,
    (err: unknown) => {
      katexLoad = null;
      throw err;
    },
  );
  return katexLoad;
}

/**
 * Renders every maths placeholder under `root` with KaTeX, importing it
 * first. Resolves at once when there is no placeholder; when KaTeX cannot
 * load, the placeholders keep their TeX text.
 */
export async function renderMath(root: ParentNode): Promise<void> {
  const placeholders = [...root.querySelectorAll<HTMLElement>('.math[data-tex]')];
  if (placeholders.length === 0) return;
  let katex: Katex;
  try {
    katex = await loadKatex();
  } catch {
    return;
  }
  for (const el of placeholders) {
    // A newer render while KaTeX loaded replaced this one.
    if (!root.contains(el)) continue;
    const tex = el.getAttribute('data-tex') ?? '';
    const displayMode = el.classList.contains('math-block') || el.classList.contains('math-display');
    katex.render(tex, el, { throwOnError: false, trust: false, displayMode, output: 'htmlAndMathml' });
  }
}

/** Replaces the content of `target` with the rendered statement; resolves once its maths is drawn. */
export function renderStatement(target: HTMLElement, markdown: string, options: StatementOptions = {}): Promise<void> {
  target.innerHTML = statementHtml(markdown, options);
  return renderMath(target);
}
