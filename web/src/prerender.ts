// The server-side entry scripts/prerender.mjs builds and runs after
// `vite build` (v4 P2, V4-7): the public pages rendered to HTML once, at
// build time, never on a server.
export { render } from 'svelte/server';
export { default as Landing } from './routes/Landing.svelte';
export { PRIVACY_LINKED } from './lib/site';
