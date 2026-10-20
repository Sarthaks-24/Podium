/**
 * The Lucide icon set (design.md section 9, v2 F10), as shape data so
 * Icon.svelte draws them without raw HTML and a unit test can list the set.
 * Shapes are copied from Lucide through the Iconify API (better-icons
 * skill) on a 24 x 24 grid; Icon.svelte supplies the stroke.
 */

/** One SVG element of an icon. */
export type Shape =
  | { tag: 'path'; d: string }
  | { tag: 'circle'; cx: number; cy: number; r: number }
  | { tag: 'rect'; x: number; y: number; width: number; height: number; rx?: number };

const path = (d: string): Shape => ({ tag: 'path', d });
const ring: Shape = { tag: 'circle', cx: 12, cy: 12, r: 10 };

/** Every icon the app draws, by Lucide name. */
export const ICONS = {
  sun: [{ tag: 'circle', cx: 12, cy: 12, r: 4 }, path('M12 2v2m0 16v2M4.93 4.93l1.41 1.41m11.32 11.32l1.41 1.41M2 12h2m16 0h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41')],
  moon: [path('M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401')],
  copy: [{ tag: 'rect', x: 8, y: 8, width: 14, height: 14, rx: 2 }, path('M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2')],
  check: [path('M20 6L9 17l-5-5')],
  x: [path('M18 6L6 18M6 6l12 12')],
  bell: [path('M10.268 21a2 2 0 0 0 3.464 0'), path('M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326')],
  'message-square': [path('M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z')],
  download: [path('M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3')],
  upload: [path('M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12')],
  'refresh-cw': [path('M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8M21 3v5h-5M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16M8 16H3v5')],
  'chevron-up': [path('M18 15l-6-6-6 6')],
  'chevron-down': [path('M6 9l6 6 6-6')],
  'chevron-right': [path('m9 18l6-6l-6-6')],
  // P5: Previous in the workspace's phone strip.
  'chevron-left': [path('m15 18l-6-6l6-6')],
  trash: [path('M3 6h18M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2')],
  clock: [ring, path('M12 6v6l4 2')],
  lock: [{ tag: 'rect', x: 3, y: 11, width: 18, height: 11, rx: 2 }, path('M7 11V7a5 5 0 0 1 10 0v4')],
  'alert-triangle': [path('M21.73 18l-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3M12 9v4M12 17h.01')],
  maximize: [path('M8 3H5a2 2 0 0 0-2 2v3M21 8V5a2 2 0 0 0-2-2h-3M3 16v3a2 2 0 0 0 2 2h3M16 21h3a2 2 0 0 0 2-2v-3')],
  // Added in v2 F10: Run, Submit, pause, the phone menu, add, and the verdict marks.
  play: [path('M5 5a2 2 0 0 1 3.008-1.728l11.997 6.998a2 2 0 0 1 .003 3.458l-12 7A2 2 0 0 1 5 19z')],
  send: [path('M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11zm7.318-19.539l-10.94 10.939')],
  pause: [{ tag: 'rect', x: 14, y: 3, width: 5, height: 18, rx: 1 }, { tag: 'rect', x: 5, y: 3, width: 5, height: 18, rx: 1 }],
  menu: [path('M4 5h16M4 12h16M4 19h16')],
  plus: [path('M5 12h14m-7-7v14')],
  'circle-check': [ring, path('m16 9l-5.5 5.5L8 12')],
  'circle-x': [ring, path('m15 9l-6 6m0-6l6 6')],
  'circle-alert': [ring, path('M12 8v4m0 4h.01')],
} satisfies Record<string, Shape[]>;

/** A name in the icon set. */
export type IconName = keyof typeof ICONS;
