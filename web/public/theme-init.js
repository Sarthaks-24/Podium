// Apply the cached theme and verdict colours before first paint. Loaded as
// a classic blocking script from the same origin, so the server's
// Content-Security-Policy can stay at script-src 'self' with no
// inline-script hash. The keys and ids match src/lib/theme.ts; a test keeps
// them in step. The account's preferences replace these once the session
// loads.
try {
  const themes = ['light', 'nordic-frost', 'soft-lavender', 'dark', 'overcast', 'oled-black', 'nordic-slate', 'cosmic-violet', 'hc-light', 'hc-dark'];
  const root = document.documentElement;
  const theme = localStorage.getItem('podium-theme');
  if (themes.indexOf(theme) >= 0) {
    root.setAttribute('data-theme', theme);
  }
  if (localStorage.getItem('podium-verdicts') === 'signal') {
    root.setAttribute('data-verdicts', 'signal');
  }
} catch {
  // Storage blocked: follow the system theme.
}
