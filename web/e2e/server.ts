import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";

// Runs the production binary for Playwright on a temporary data directory, and
// removes the directory once the binary has stopped. A global setup and
// teardown cannot do this: Playwright starts its web server before the setup
// and stops it after the teardown.
//
// While no user exists, the binary prints the setup link to stderr. Its stderr
// still reaches the terminal, and the link goes to the file that
// E2E_SETUP_LINK_FILE names, where the setup test reads it.

const dataDir = mkdtempSync(join(tmpdir(), "verve-e2e-"));
const setupLinkFile = process.env.E2E_SETUP_LINK_FILE;

function removeSetupLink() {
  if (setupLinkFile !== undefined) {
    rmSync(setupLinkFile, { force: true });
  }
}

// A file left by a run that did not stop cleanly holds an old link.
removeSetupLink();

const server = spawn("../bin/verve", ["serve"], {
  env: { ...process.env, VERVE_DATA_DIR: dataDir },
  stdio: ["inherit", "inherit", "pipe"],
});

createInterface({ input: server.stderr }).on("line", (line) => {
  process.stderr.write(`${line}\n`);
  const link = /^Create the first admin at (\S+)/.exec(line)?.[1];
  if (link !== undefined && setupLinkFile !== undefined) {
    writeFileSync(setupLinkFile, link);
  }
});

// Playwright signals the whole process group, so the binary receives SIGTERM
// too. It must not be forwarded: a second signal stops the binary at once.
process.on("SIGTERM", () => undefined);

server.on("close", (code) => {
  removeSetupLink();
  rmSync(dataDir, { recursive: true, force: true });
  process.exitCode = code ?? 1;
});
