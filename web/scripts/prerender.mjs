// Prerenders the public landing page after `vite build` (v4 P2, V4-7), so
// a signed-out visitor and a search engine read it as HTML before any
// script runs. It builds src/prerender.ts for the server, renders Landing
// with svelte/server, and writes dist/landing.html from the built
// index.html: the same CSS, fonts, theme-init.js and app script (which
// takes over on load, main.ts clearing #app), an indexable head, and the
// page in a plain shell. Go serves it at / to requests with no session
// cookie (webui.go). No inline script or style, so the CSP holds.
// Usage: node scripts/prerender.mjs (npm run build runs it).
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { build } from 'vite';

const OUT = 'node_modules/.prerender';
const started = performance.now();

await build({
  logLevel: 'warn',
  build: { ssr: 'src/prerender.ts', outDir: OUT, emptyOutDir: true, rollupOptions: { output: { entryFileNames: 'entry.mjs' } } },
});
const { render, Landing, PRIVACY_LINKED } = await import(pathToFileURL(`${OUT}/entry.mjs`).href);

const swap = (html, from, to, what) => {
  if (!from.test(html)) throw new Error(`prerender: ${what} not found in dist/index.html`);
  return html.replace(from, to);
};

const TITLE = 'Programming contests · podium';
const DESCRIPTION = "Podium runs this college's programming contests: students solve problems in the browser, organisers set them.";
// The site address is a token Go fills on serve (webui.go), so one build serves any host.
const HEAD = [
  `<meta name="description" content="${DESCRIPTION}" />`,
  '<link rel="canonical" href="%BASE_URL%/" />',
  '<meta property="og:type" content="website" />',
  '<meta property="og:site_name" content="podium" />',
  `<meta property="og:title" content="${TITLE}" />`,
  `<meta property="og:description" content="${DESCRIPTION}" />`,
  '<meta property="og:url" content="%BASE_URL%/" />',
  '<meta property="og:image" content="%BASE_URL%/og.png" />',
  '<meta property="og:image:width" content="1200" />',
  '<meta property="og:image:height" content="630" />',
  '<meta property="og:image:alt" content="The podium word-mark" />',
  '<meta name="twitter:card" content="summary_large_image" />',
].join('\n    ');
const { body } = render(Landing);
const foot = PRIVACY_LINKED ? '<p><a href="/privacy">Privacy</a></p>' : '';
const shell =
  '<div class="prerendered">' +
  '<header><a class="prerendered-mark" href="/">podium</a></header>' +
  `<main id="main">${body}</main>` +
  `<footer>${foot}</footer>` +
  '</div>';

let html = readFileSync('dist/index.html', 'utf8');
html = swap(html, /\s*<!-- A college tool behind sign-in: never indexed \(v3\)\. -->\s*<meta name="robots" content="noindex" \/>/, `\n    ${HEAD}`, 'the noindex meta');
html = swap(html, /<title>podium<\/title>/, `<title>${TITLE}</title>`, 'the title');
html = swap(html, /<div id="app">[\s\S]*?<\/main>\s*<\/div>/, `<div id="app">${shell}</div>`, 'the #app fallback');
writeFileSync('dist/landing.html', html);
rmSync(OUT, { recursive: true, force: true });

console.log(`prerender: dist/landing.html, ${(html.length / 1024).toFixed(1)} kB, in ${Math.round(performance.now() - started)} ms`);
