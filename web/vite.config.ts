import { defineConfig } from 'vitest/config';
import type { Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync } from 'node:fs';
import { routeManifest } from './src/lib/routing.ts';
import { privacyBodyHtml } from './src/lib/privacyBody.ts';

// Node's process, declared here so the web app needs no @types/node.
declare const process: { env: Record<string, string | undefined> };

// The Go server listens here during development (`make dev`).
const apiTarget = 'http://127.0.0.1:8080';

/**
 * Writes dist/routes.json from the route table in src/lib/routing.ts
 * (contract C2). The server reads it from the embedded build for the real
 * 404, the public head and robots; a copy is checked in at
 * server/internal/webui/testdata/routes.json and lib/routing.test.ts fails
 * when the two differ.
 */
function routesManifest(): Plugin {
  return {
    name: 'podium-routes-manifest',
    apply: 'build',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'routes.json', source: `${JSON.stringify(routeManifest(), null, 2)}
` });
    },
  };
}

/**
 * Writes dist/privacy-body.html from src/content/privacy.md (P3 FP.1,
 * D100): the notice rendered with marked and sanitised with DOMPurify at
 * build time, for the server's <!--podium:body--> block on /privacy, so
 * the page reads without JavaScript. The server serves it only once
 * routing.ts marks /privacy public, which waits for the owner (D85).
 */
function privacyBody(): Plugin {
  return {
    name: 'podium-privacy-body',
    apply: 'build',
    async generateBundle() {
      const markdown = readFileSync(new URL('./src/content/privacy.md', import.meta.url), 'utf8');
      this.emitFile({ type: 'asset', fileName: 'privacy-body.html', source: await privacyBodyHtml(markdown) });
    },
  };
}

export default defineConfig({
  plugins: [svelte(), routesManifest(), privacyBody()],
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
  },
});
