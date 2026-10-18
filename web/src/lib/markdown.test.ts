// @vitest-environment jsdom
// Statement rendering: the sanitiser removes everything that could run code
// or restyle the page, raw HTML is shown as text, and maths is extracted to
// placeholders without touching code.
import { describe, expect, it } from 'vitest';
import { renderStatement, sanitizeHtml, statementHtml } from './markdown';

function dom(html: string): HTMLElement {
  const div = document.createElement('div');
  div.innerHTML = html;
  return div;
}

describe('sanitizeHtml', () => {
  it('removes script tags and their content', () => {
    const out = sanitizeHtml('<p>Hi</p><script>alert(1)</script>');
    expect(out).toBe('<p>Hi</p>');
  });

  it('removes event handler attributes', () => {
    const el = dom(sanitizeHtml('<img src="x.png" onerror="alert(1)"><p onclick="alert(2)">Hi</p>'));
    expect(el.querySelector('img')?.hasAttribute('onerror')).toBe(false);
    expect(el.querySelector('p')?.hasAttribute('onclick')).toBe(false);
  });

  it('removes javascript: links', () => {
    const el = dom(sanitizeHtml('<a href="javascript:alert(1)">x</a><a href="JaVaScRiPt:alert(1)">y</a>'));
    for (const a of el.querySelectorAll('a')) expect(a.getAttribute('href')).toBeNull();
  });

  it('removes inline style attributes and style elements', () => {
    const el = dom(sanitizeHtml('<p style="color: red">Hi</p><style>body{display:none}</style>'));
    expect(el.querySelector('p')?.hasAttribute('style')).toBe(false);
    expect(el.querySelector('style')).toBeNull();
  });

  it('removes frames, embeds and form controls', () => {
    const el = dom(
      sanitizeHtml('<iframe src="/"></iframe><object data="x"></object><embed src="x"><form><input><button>b</button></form>'),
    );
    expect(el.querySelector('iframe, object, embed, form, input, button')).toBeNull();
  });

  it('gives links a safe rel and opens them in the same tab', () => {
    const a = dom(sanitizeHtml('<a href="https://example.com" target="_blank">x</a>')).querySelector('a');
    expect(a?.getAttribute('rel')).toBe('noopener noreferrer');
    expect(a?.hasAttribute('target')).toBe(false);
  });

  it('keeps maths placeholders', () => {
    const out = sanitizeHtml('<span class="math" data-tex="x^2"></span>');
    expect(out).toBe('<span class="math" data-tex="x^2"></span>');
  });
});

describe('statementHtml', () => {
  it('renders Markdown', () => {
    const el = dom(statementHtml('Read **n**.\n\n- one\n- two'));
    expect(el.querySelector('strong')?.textContent).toBe('n');
    expect(el.querySelectorAll('li')).toHaveLength(2);
  });

  it('shows raw HTML as text instead of passing it through', () => {
    const el = dom(statementHtml('Hello <img src=x onerror="alert(1)"> and <script>alert(2)</script>'));
    expect(el.querySelector('img, script')).toBeNull();
    expect(el.textContent).toContain('<img src=x onerror="alert(1)">');
  });

  it('drops javascript: links written in Markdown', () => {
    const el = dom(statementHtml('[click](javascript:alert(1))'));
    expect(el.querySelector('a')?.getAttribute('href') ?? null).toBeNull();
  });

  it('extracts inline maths with the TeX intact', () => {
    const el = dom(statementHtml('Given $1 \\le n \\le 10^5$ and $a_i < b_i$.'));
    const spans = [...el.querySelectorAll('span.math')];
    expect(spans.map((s) => s.getAttribute('data-tex'))).toEqual(['1 \\le n \\le 10^5', 'a_i < b_i']);
  });

  it('extracts block maths', () => {
    const el = dom(statementHtml('Sum:\n\n$$\n\\sum_{i=1}^{n} a_i\n$$\n\nDone.'));
    const block = el.querySelector('div.math.math-block');
    expect(block?.getAttribute('data-tex')).toBe('\\sum_{i=1}^{n} a_i');
    expect(el.querySelectorAll('p')).toHaveLength(2);
  });

  it('escapes TeX that looks like HTML', () => {
    const el = dom(statementHtml('$<b onclick="x">$'));
    expect(el.querySelector('b')).toBeNull();
    expect(el.querySelector('span.math')?.getAttribute('data-tex')).toBe('<b onclick="x">');
  });

  it('leaves dollars in code spans and code blocks alone', () => {
    const el = dom(statementHtml('Run `echo $x$` then:\n\n```\nprice = $5$\n```'));
    expect(el.querySelector('.math')).toBeNull();
    expect(el.querySelector('p code')?.textContent).toBe('echo $x$');
    expect(el.querySelector('pre code')?.textContent).toBe('price = $5$\n');
  });

  it('leaves prices and escaped dollars as text', () => {
    const el = dom(statementHtml('It costs $5 and $10. Also \\$x\\$.'));
    expect(el.querySelector('.math')).toBeNull();
    expect(el.textContent?.trim()).toBe('It costs $5 and $10. Also $x$.');
  });

  it('moves headings down to the requested level', () => {
    const el = dom(statementHtml('# Input\n\n## Note', { headingBase: 3 }));
    expect(el.querySelector('h3')?.textContent).toBe('Input');
    expect(el.querySelector('h4')?.textContent).toBe('Note');
    expect(el.querySelector('h1, h2')).toBeNull();
  });
});

describe('renderStatement', () => {
  it('renders maths with KaTeX after sanitising', () => {
    const target = document.createElement('div');
    renderStatement(target, 'Let $x^2$ be.\n\n$$y$$');
    expect(target.querySelector('span.math .katex')).not.toBeNull();
    expect(target.querySelector('div.math-block .katex-display')).not.toBeNull();
  });

  it('shows bad TeX as an inline error instead of throwing', () => {
    const target = document.createElement('div');
    expect(() => renderStatement(target, 'Broken $\\frac{1}{$ here')).not.toThrow();
  });
});
