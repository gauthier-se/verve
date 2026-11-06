import { readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

/**
 * The file where e2e/server.ts writes the setup link that the binary listening
 * on `listenAddr` prints to stderr while it has no user.
 */
export function setupLinkFile(listenAddr: string): string {
  return join(tmpdir(), `verve-e2e-${listenAddr.replaceAll(":", "-")}.link`);
}

/**
 * The setup link of the server at `baseURL`, or undefined until e2e/server.ts
 * has written it.
 */
export async function readSetupLink(
  baseURL: string,
): Promise<string | undefined> {
  return readFile(setupLinkFile(new URL(baseURL).host), "utf8").catch(
    () => undefined,
  );
}
