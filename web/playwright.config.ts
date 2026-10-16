// Playwright runs the screen sweep in tests/ against the Vite dev server.
// Every /api/ call is mocked inside the tests, so no Go server is needed.
// The port is fixed and never reused, so an owner's own `vite` on 5173 is
// left alone.
import { defineConfig, devices } from '@playwright/test';

const port = 5179;
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: './tests',
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
