import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Runs the production binary for Playwright on a temporary data directory, and
// removes the directory once the binary has stopped. A global setup and
// teardown cannot do this: Playwright starts its web server before the setup
// and stops it after the teardown.

const dataDir = mkdtempSync(join(tmpdir(), "verve-e2e-"));

const server = spawn("../bin/verve", ["serve"], {
  env: { ...process.env, VERVE_DATA_DIR: dataDir },
  stdio: "inherit",
});

// Playwright signals the whole process group, so the binary receives SIGTERM
// too. It must not be forwarded: a second signal stops the binary at once.
process.on("SIGTERM", () => undefined);

server.on("close", (code) => {
  rmSync(dataDir, { recursive: true, force: true });
  process.exitCode = code ?? 1;
});
