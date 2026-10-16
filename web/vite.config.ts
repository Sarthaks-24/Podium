import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The Go server listens here during development (`make dev`).
const apiTarget = 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [svelte()],
  define: {
    // Computed once per build so the footer year is never hard-coded.
    __BUILD_YEAR__: JSON.stringify(new Date().getFullYear()),
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
