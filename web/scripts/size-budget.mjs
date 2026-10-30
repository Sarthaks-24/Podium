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
import { readdirSync, readFileSync } from 'node:fs';
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

// The editor (V4-3): what the first open of a code editor downloads beyond
// the first load. That is the editor chunk (lib/editor.ts), every chunk it
// imports statically, and the default language, C++, with the chunks Vite
// lists for it in __vite__mapDeps. Budget: 171.57 kB measured on 2026-10-31
// plus 10 percent (cache/v3/calibration.md); raise it only with a
// measurement and a reason there.
const EDITOR_BUDGET_KB = 189;
const firstLoad = new Set(files.js.map((p) => p.slice('/assets/'.length)));
const assets = new URL('assets/', dist);
const read = (name) => readFileSync(new URL(name, assets), 'utf8');
const editorFile = readdirSync(assets).find((n) => /^editor-[\w-]+\.js$/.test(n));
if (!editorFile) {
  console.error('size budget: no editor chunk (assets/editor-*.js) in dist; has lib/editor.ts stopped being loaded with import()?');
  process.exit(1);
}
const editorText = read(editorFile);
const mapped = /m\.f=\[([^\]]*)\]/.exec(editorText)?.[1].split(',').map((s) => s.replace(/"|assets\//g, ''));
const cpp = /cpp17:\(\)=>\w+\(\(\)=>import\(`\.\/([^`]+)`\)[^\n]*?__vite__mapDeps\(\[([\d,]+)\]\)/.exec(editorText);
if (!mapped || !cpp) {
  console.error('size budget: cannot find the C++ language import in the editor chunk; update the pattern in scripts/size-budget.mjs.');
  process.exit(1);
}
const opened = new Set();
const visit = (name) => {
  if (firstLoad.has(name) || opened.has(name)) return;
  opened.add(name);
  for (const m of read(name).matchAll(/(?:from|import)\s*"\.\/([\w.-]+\.js)"/g)) visit(m[1]);
};
visit(editorFile);
for (const i of cpp[2].split(',')) visit(mapped[Number(i)]);
let editorGz = 0;
for (const name of opened) {
  const size = gzipSync(readFileSync(new URL(name, assets))).length;
  editorGz += size;
  console.log(`  /assets/${name}  ${(size / 1000).toFixed(2)} kB gzip`);
}
const editorOver = editorGz / 1000 > EDITOR_BUDGET_KB;
console.log(`editor: ${opened.size} files on first open (C++), ${(editorGz / 1000).toFixed(2)} kB gzip, budget ${EDITOR_BUDGET_KB} kB gzip${editorOver ? ': over budget' : ''}`);
if (editorOver) {
  console.error('size budget: the editor is over budget. Load the new part only when it is used, or record a measurement and a reason in calibration.md before raising the budget.');
  process.exit(1);
}
