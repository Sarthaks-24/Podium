// Playwright runs the screen sweep in tests/ against the Vite dev server.
// Every /api/ call is mocked inside the tests, so no Go server is needed.
// The port is fixed and never reused, so an owner's own `vite` on 5173 is
// left alone.
//
// toHaveScreenshot references (tests/baseline.spec.ts) live in the git-ignored
// cache/ at the repository root, not next to the tests: they are
// Windows-rendered, owner-machine artefacts and never go to GitHub. From a
// worktree the path points at the worktree's own cache/, so copy the folder
// in from the main checkout first (cache/v2-planning-files/v2-frontend/baseline/INDEX.md).
import { defineConfig, devices } from '@playwright/test';

const port = 5179;
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: './tests',
  snapshotPathTemplate: '{testDir}/../../cache/v2-planning-files/v2-frontend/baseline/{testFileName}/{arg}-{platform}{ext}',
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.01 } },
  fullyParallel: true,
  forbidOnly: true,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL,
    timezoneId: 'Asia/Kolkata',
    locale: 'en-GB',
    trace: 'off',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npx vite --host 127.0.0.1 --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 60_000,
  },
});
