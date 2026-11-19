// Fails when the built web app inlines a font as a data: URL. The server's
// Content-Security-Policy allows fonts from 'self' only, so an inlined font
// is blocked in the browser (KaTeX's smaller fonts were, before v2.1.1).
// Runs after `vite build`, from web/: node scripts/check-inline-fonts.mjs
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const dist = new URL('../dist/', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1');
const patterns = ['data:font/woff2', 'data:application/font-woff2'];
const found = [];

for (const entry of readdirSync(dist, { recursive: true, withFileTypes: true })) {
  if (!entry.isFile() || !/\.(css|js|html)$/.test(entry.name)) continue;
  const path = join(entry.parentPath, entry.name);
  const text = readFileSync(path, 'utf8');
  for (const p of patterns) if (text.includes(p)) found.push(`${path}: ${p}`);
}

if (found.length > 0) {
  console.error('Inlined fonts found; the font-src rule blocks them:\n' + found.join('\n'));
  process.exit(1);
}
console.log('check-inline-fonts: no inlined fonts in dist');
