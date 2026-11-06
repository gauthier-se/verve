import { defineConfig, devices } from "@playwright/test";

import { setupLinkFile } from "./e2e/setup-link";

// Each browser has its own server, on its own data directory: the setup test
// creates the first admin, which an instance accepts once.
const browsers = [
  {
    name: "chromium",
    device: devices["Desktop Chrome"],
    listenAddr: "127.0.0.1:18080",
  },
  {
    name: "webkit",
    device: devices["iPhone 15"],
    listenAddr: "127.0.0.1:18081",
  },
];

export default defineConfig({
  testDir: "e2e",
  forbidOnly: !!process.env.CI,
  // The HTML report opens on failure by default, which blocks `task e2e`.
  reporter: [["list"], ["html", { open: "never" }]],
  use: { trace: "retain-on-failure" },
  projects: browsers.map(({ name, device, listenAddr }) => ({
    name,
    use: { ...device, baseURL: `http://${listenAddr}` },
  })),
  webServer: browsers.map(({ listenAddr }) => ({
    command: "node e2e/server.ts",
    url: `http://${listenAddr}/readyz`,
    env: {
      VERVE_LISTEN_ADDR: listenAddr,
      VERVE_LOG_LEVEL: "warn",
      E2E_SETUP_LINK_FILE: setupLinkFile(listenAddr),
    },
    reuseExistingServer: !process.env.CI,
    // Without it Playwright sends SIGKILL, and e2e/server.ts cannot clean up.
    gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
  })),
});
