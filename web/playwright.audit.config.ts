// The repeatable screen audit (`make audit`, V201-16): every screen in
// tests/audit/screens.ts at four widths in light and dark, with a screenshot,
// an overflow check, axe, and the focus check after Escape. It runs only
// tests/audit/*.spec.ts against the Vite dev server on its own port, every
// /api/ call mocked, and is not part of `npm test`, the normal Playwright run
// or CI. Results go to cache/audit-<date>/ (git-ignored); REPORT.md is written
// by tests/audit/report.ts even when checks fail.
import { defineConfig, devices } from '@playwright/test';

const port = 5392;
const baseURL = `http://127.0.0.1:${port}`;

// One date for the whole run, shared with the workers through the environment.
process.env.AUDIT_DATE ??= new Date().toLocaleDateString('en-CA');

export default defineConfig({
  testDir: './tests/audit',
  testMatch: '*.spec.ts',
  globalSetup: './tests/audit/setup.ts',
  globalTeardown: './tests/audit/report.ts',
  fullyParallel: true,
  forbidOnly: true,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL,
    timezoneId: 'Asia/Kolkata',
    locale: 'en-GB',
    trace: 'off',
    // A step that cannot find its control fails its cell in seconds, not at the test timeout.
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npx vite --host 127.0.0.1 --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 60_000,
  },
});
