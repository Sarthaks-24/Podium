/**
 * Builds dist/privacy-body.html, the no-JavaScript copy of the privacy
 * notice (P3 FP.1, D100). Runs in Node at build time (vite.config.ts) and
 * in the unit test: marked renders src/content/privacy.md, DOMPurify on a
 * jsdom window sanitises it with the statement rules, and raw HTML in the
 * Markdown is refused. The retention table is not known at build time, so
 * its place holds a marked block the server can fill from
 * server/internal/retention/periods.go:
 *
 *   <!--podium:retention--> ... <!--/podium:retention-->
 *
 * The server fills it with a table built from retention.Periods when it
 * loads the build (server/internal/webui/head.go, council C1-052); the
 * sentence below is what a build served by anything else shows.
 */
const MARKER = '<!-- retention -->';

/** What the no-JavaScript page says in place of the table. */
export const RETENTION_FALLBACK =
  '<p>The table of how long each kind of data is kept is on this page when it opens in a browser with JavaScript, and the organisers send it on request.</p>';

export async function privacyBodyHtml(markdown: string): Promise<string> {
  const [{ Marked }, { default: createDOMPurify }, { JSDOM }] = await Promise.all([import('marked'), import('dompurify'), import('jsdom')]);
  const purify = createDOMPurify(new JSDOM('').window as unknown as Parameters<typeof createDOMPurify>[0]);
  const marked = new Marked({ gfm: true, renderer: { html: () => '' } });
  const clean = (md: string) =>
    purify.sanitize(marked.parse(md) as string, {
      FORBID_ATTR: ['style', 'target', 'srcset', 'formaction'],
      FORBID_TAGS: ['script', 'style', 'iframe', 'frame', 'object', 'embed', 'form', 'input', 'button', 'textarea', 'select', 'option', 'link', 'meta', 'base'],
    });
  const i = markdown.indexOf(MARKER);
  const before = i < 0 ? markdown : markdown.slice(0, i);
  const after = i < 0 ? '' : markdown.slice(i + MARKER.length);
  const table = i < 0 ? '' : `<!--podium:retention-->${RETENTION_FALLBACK}<!--/podium:retention-->`;
  return `<h1>Privacy</h1>\n${clean(before)}${table}\n${clean(after)}`;
}
