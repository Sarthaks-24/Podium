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

const DESCRIPTION = "Podium runs this college's programming contests: students solve problems in the browser, organisers set them.";
const { body } = render(Landing);
const foot = PRIVACY_LINKED ? '<p><a href="/privacy">Privacy</a></p>' : '';
const shell =
  '<div class="prerendered">' +
  '<header><a class="prerendered-mark" href="/">podium</a></header>' +
  `<main id="main">${body}</main>` +
  `<footer>${foot}</footer>` +
  '</div>';

let html = readFileSync('dist/index.html', 'utf8');
html = swap(html, /\s*<!-- A college tool behind sign-in: never indexed \(v3\)\. -->\s*<meta name="robots" content="noindex" \/>/, `\n    <meta name="description" content="${DESCRIPTION}" />`, 'the noindex meta');
html = swap(html, /<title>podium<\/title>/, '<title>Programming contests · podium</title>', 'the title');
html = swap(html, /<div id="app">[\s\S]*?<\/main>\s*<\/div>/, `<div id="app">${shell}</div>`, 'the #app fallback');
writeFileSync('dist/landing.html', html);
rmSync(OUT, { recursive: true, force: true });

console.log(`prerender: dist/landing.html, ${(html.length / 1024).toFixed(1)} kB, in ${Math.round(performance.now() - started)} ms`);
