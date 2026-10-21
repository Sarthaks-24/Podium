// Fails when a student's first load grows past its budget (v2 P6 F8.3).
//
// Reads dist/index.html after `npm run build` and adds up what every page
// downloads before any screen has loaded: the entry script, the chunks it
// preloads, the stylesheets and the preloaded fonts. Organiser screens, the editor, the
// statement renderer and KaTeX are loaded with import() later and are not
// counted. Sizes are gzip as the server sends them (level 6, as Vite
// reports); fonts are woff2, already compressed, so their raw size is
// counted (four faces, 99.97 kB on 2026-10-21, budget with 8 percent
// headroom; council run 2, C2-047). The budgets were set on 2026-10-21 from the measured clean
// build (cache/v2-planning-files/v1/calibration.md, "Web bundle,
// 2026-10-21, P6"), with about 7 percent of headroom; raise one only with
// a measurement and a reason in that file.
import { readFileSync } from 'node:fs';
import { gzipSync } from 'node:zlib';

const BUDGET_KB = { js: 120, css: 16, font: 108 };

const dist = new URL('../dist/', import.meta.url);
const html = readFileSync(new URL('index.html', dist), 'utf8');
const attr = (tag, name) => new RegExp(`${name}="([^"]+)"`).exec(tag)?.[1];
const files = { js: [], css: [], font: [] };
for (const [tag] of html.matchAll(/<(script|link)\b[^>]*>/g)) {
  if (tag.startsWith('<script') && /type="module"/.test(tag)) files.js.push(attr(tag, 'src'));
  else if (/rel="modulepreload"/.test(tag)) files.js.push(attr(tag, 'href'));
  else if (/rel="stylesheet"/.test(tag)) files.css.push(attr(tag, 'href'));
  else if (/rel="preload"/.test(tag) && /as="font"/.test(tag)) files.font.push(attr(tag, 'href'));
}

let failed = false;
for (const kind of ['js', 'css', 'font']) {
  const paths = files[kind].filter((p) => p?.startsWith(kind === 'font' ? '/fonts/' : '/assets/'));
  if (paths.length === 0) {
    console.error(`size budget: no ${kind} files found in dist/index.html; run npm run build first.`);
    process.exit(1);
  }
  let raw = 0;
  let gz = 0;
  for (const p of paths) {
    const bytes = readFileSync(new URL(p.slice(1), dist));
    const size = kind === 'font' ? bytes.length : gzipSync(bytes).length;
    raw += bytes.length;
    gz += size;
    console.log(`  ${p}  ${(size / 1000).toFixed(2)} ${kind === 'font' ? 'kB woff2' : 'kB gzip'}`);
  }
  const over = gz / 1000 > BUDGET_KB[kind];
  failed ||= over;
  const unit = kind === 'font' ? 'kB woff2' : 'kB gzip';
  console.log(`${kind}: ${paths.length} files, ${(raw / 1000).toFixed(2)} kB, ${(gz / 1000).toFixed(2)} ${unit}, budget ${BUDGET_KB[kind]} ${unit}${over ? ': over budget' : ''}`);
}
if (failed) {
  console.error('size budget: a student\'s first load is over budget. Load the new code with import(), or record a measurement and a reason in calibration.md before raising the budget.');
  process.exit(1);
}
