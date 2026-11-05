// Apply the cached theme, verdict colours and editor colours before first
// paint. Loaded as a classic blocking script from the same origin, so the
// server's Content-Security-Policy can stay at script-src 'self' with no
// inline-script hash. The keys and ids match src/lib/theme.ts and
// src/lib/editor-colours.ts; a test keeps them in step and runs this file
// against hostile storage. The account's preferences replace these once the
// session loads.
//
// A preset or custom theme is stored as the variables the theme engine
// derived. They are applied only when the id is a well-formed theme id, the
// family and contrast words are known, there is exactly one variable for each
// name on the allowlist, and every value has the shape of a colour (or of the
// scrim and menu shadow). Anything else falls back to the system theme.
try {
  const themes = ['light', 'dark', 'hc-light', 'hc-dark'];
  const editors = ['paper', 'midnight', 'dusk', 'high-contrast'];
  const colour = /^#[0-9a-fA-F]{6}$/;
  const scrim = /^rgba\(\d{1,3}, \d{1,3}, \d{1,3}, 0?\.\d{1,2}\)$/;
  const shadow = /^0 8px 24px rgba\(\d{1,3}, \d{1,3}, \d{1,3}, 0?\.\d{1,2}\)$/;
  const allowed = {
    '--paper': colour, '--surface': colour, '--surface-2': colour, '--ink': colour, '--ink-muted': colour,
    '--ink-faint': colour, '--line': colour, '--line-strong': colour, '--act': colour, '--act-ink': colour,
    '--act-tint': colour, '--scrim': scrim, '--shadow-menu': shadow,
  };
  const root = document.documentElement;
  const theme = localStorage.getItem('podium-theme');
  if (themes.indexOf(theme) >= 0) {
    root.setAttribute('data-theme', theme);
  } else if (theme && /^[a-z0-9-]{1,32}$/.test(theme)) {
    let saved = null;
    try {
      saved = JSON.parse(localStorage.getItem('podium-theme-vars'));
    } catch {
      saved = null;
    }
    const names = Object.keys(allowed);
    const vars = saved && typeof saved === 'object' ? saved.vars : null;
    const ok =
      saved !== null && typeof saved === 'object' && saved.theme === theme &&
      (saved.family === 'light' || saved.family === 'dark') &&
      (saved.contrast === 'standard' || saved.contrast === 'high') &&
      vars !== null && typeof vars === 'object' &&
      Object.keys(vars).length === names.length &&
      names.every((name) => typeof vars[name] === 'string' && allowed[name].test(vars[name]));
    if (ok) {
      root.setAttribute('data-theme', theme);
      root.setAttribute('data-family', saved.family);
      if (saved.contrast === 'high') root.setAttribute('data-contrast', 'high');
      names.forEach((name) => root.style.setProperty(name, vars[name]));
    }
  }
  if (localStorage.getItem('podium-verdicts') === 'signal') {
    root.setAttribute('data-verdicts', 'signal');
  }
  const editor = localStorage.getItem('podium-editor-colours');
  if (editors.indexOf(editor) >= 0) {
    root.setAttribute('data-editor', editor);
  }
} catch {
  // Storage blocked: follow the system theme.
}
