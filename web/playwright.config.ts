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
  // Twenty pixels at a 0.1 colour threshold: too few to hide a changed
  // digit or a token colour shift (council C1-020, C2-046). Measured noise
  // on the frozen-clock baselines is zero (cache/v2-planning-files/v2-frontend/baseline/INDEX.md).
  expect: { toHaveScreenshot: { maxDiffPixels: 20, threshold: 0.1 } },
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
