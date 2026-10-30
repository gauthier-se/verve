import { defineConfig, devices } from "@playwright/test";

const listenAddr = "127.0.0.1:18080";
const baseURL = `http://${listenAddr}`;

export default defineConfig({
  testDir: "e2e",
  forbidOnly: !!process.env.CI,
  // The HTML report opens on failure by default, which blocks `task e2e`.
  reporter: [["list"], ["html", { open: "never" }]],
  use: { baseURL, trace: "retain-on-failure" },
  projects: [
    { name: "chromium", use: devices["Desktop Chrome"] },
    { name: "webkit", use: devices["iPhone 15"] },
  ],
  webServer: {
    command: "node e2e/server.ts",
    url: `${baseURL}/readyz`,
    env: { VERVE_LISTEN_ADDR: listenAddr, VERVE_LOG_LEVEL: "warn" },
    reuseExistingServer: !process.env.CI,
    // Without it Playwright sends SIGKILL, and e2e/server.ts cannot clean up.
    gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
  },
});
