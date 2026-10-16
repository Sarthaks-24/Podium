// Apply the saved theme before first paint. Loaded as a classic blocking
// script from the same origin, so the server's Content-Security-Policy can
// stay at script-src 'self' with no inline-script hash. The key matches
// STORAGE_KEY in src/lib/theme.ts.
try {
  const saved = localStorage.getItem('podium-theme');
  if (saved === 'light' || saved === 'dark') {
    document.documentElement.setAttribute('data-theme', saved);
  }
} catch {
  // Storage blocked: follow the system theme.
}
