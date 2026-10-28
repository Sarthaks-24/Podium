import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Node's process, declared here so the web app needs no @types/node.
declare const process: { env: Record<string, string | undefined> };

// The Go server listens here during development (`make dev`).
// PODIUM_DEV_API points Vite at a second stack on other ports (tools/dev-stack.sh
// with PODIUM_LISTEN), for example http://127.0.0.1:8097.
const apiTarget = process.env.PODIUM_DEV_API || 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [svelte()],
  // Component tests mount Svelte in jsdom, which needs the client build of
  // svelte rather than the server one Node would pick.
  resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
  define: {
    // Computed once per build so the footer year is never hard-coded.
    __BUILD_YEAR__: JSON.stringify(new Date().getFullYear()),
  },
  // The statement renderer and the code editor are imported lazily, so the
  // dev server would only discover these on first use and reload the page
  // to re-bundle them.
  optimizeDeps: {
    include: [
      'marked',
      'dompurify',
      'katex',
      '@codemirror/state',
      '@codemirror/view',
      '@codemirror/commands',
      '@codemirror/language',
      '@codemirror/lang-cpp',
      '@codemirror/lang-java',
      '@codemirror/lang-javascript',
      '@codemirror/lang-python',
      '@lezer/highlight',
    ],
  },
  server: {
    proxy: {
      '/api': apiTarget,
      '/healthz': apiTarget,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    rolldownOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
    // Process CSS so the guardrail test can read stylesheets with ?raw.
    css: true,
    // One worker per CPU reused across files: about 40 s to about 10 s (stage A7).
    isolate: false,
  },
});
