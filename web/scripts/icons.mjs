// Makes the web manifest's icons and the share image (v4 P4) from the
// favicon's "p" mark and the word-mark, on the paper colour, with the
// browser Playwright already installs. Run it again after the mark, the
// word-mark's font or the paper colour changes; the PNGs are committed.
// Usage: node scripts/icons.mjs
import { readFileSync } from 'node:fs';
import { chromium } from '@playwright/test';

const PAPER = '#F3F4F1'; // --paper, light (src/styles/tokens.css)
const INK = '#161A17'; // --ink, light
const ACT = '#2B3FD6'; // --act, light: the mark's colour in favicon.svg
const font = readFileSync('public/fonts/schibsted-grotesk-600.woff2').toString('base64');

// The favicon's "p": a stem and a bowl on a 32 unit square.
const mark = (size) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" width="${size}" height="${size}">` +
  `<path d="M9 6v24" fill="none" stroke="${ACT}" stroke-width="4"/><circle cx="17" cy="15" r="7" fill="none" stroke="${ACT}" stroke-width="4"/></svg>`;

const page = (body, width, height) => `<!doctype html><html><head><style>
  @font-face { font-family: "Schibsted Grotesk"; font-weight: 600; src: url(data:font/woff2;base64,${font}) format("woff2"); }
  html, body { margin: 0; width: ${width}px; height: ${height}px; background: ${PAPER}; }
  body { display: flex; align-items: center; }
</style></head><body>${body}</body></html>`;

const browser = await chromium.launch();
const tab = await browser.newPage();
async function shoot(html, width, height, file) {
  await tab.setViewportSize({ width, height });
  await tab.setContent(html);
  await tab.evaluate(() => document.fonts.ready);
  await tab.screenshot({ path: file });
  console.log(`icons: ${file}`);
}

// App icons: the mark inside the middle 60 percent, so a maskable crop never cuts it.
for (const size of [192, 512]) {
  await shoot(page(`<div style="margin:auto">${mark(Math.round(size * 0.6))}</div>`, size, size), size, size, `public/icon-${size}.png`);
}

// The share image, 1200 x 630: the mark and the word-mark, left-aligned like every page.
await shoot(
  page(
    `<div style="display:flex;align-items:center;gap:40px;margin-left:120px">${mark(200)}` +
      `<span style="font:600 160px 'Schibsted Grotesk';color:${INK};letter-spacing:-2px">podium</span></div>`,
    1200,
    630,
  ),
  1200,
  630,
  'public/og.png',
);
await browser.close();
