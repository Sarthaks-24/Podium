// Live smoke tests against a real podium-server in testing mode (start it
// with `make dev-test`, or tools/dev-stack.sh inside WSL2). Nothing is
// mocked: the browser talks to the server through Vite's proxy, so this
// catches drift between the web app and the server's real responses.
// Run with: npm run test:live
import { defineConfig, devices } from '@playwright/test';

// Node's process, declared here as in vite.config.ts.
declare const process: { env: Record<string, string | undefined> };

// The server's testing-mode base URL is http://localhost:5173, and its
// cross-site check refuses any other origin, so the live run uses that port
// (reusing the Vite that `make dev-test` started, if any). A second stack on
// side ports sets PODIUM_LIVE_PORT to its PODIUM_BASE_URL port and
// PODIUM_DEV_API to its server (tools/dev-stack.sh).
const port = Number(process.env.PODIUM_LIVE_PORT || 5173);

export default defineConfig({
  testDir: './tests-live',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  reporter: 'list',
  use: { baseURL: `http://localhost:${port}`, trace: 'off' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npx vite --host localhost --port ${port} --strictPort`,
    url: `http://localhost:${port}`,
    reuseExistingServer: true,
    timeout: 60_000,
  },
});
