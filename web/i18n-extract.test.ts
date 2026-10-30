import { execSync } from "node:child_process";
import {
  cp,
  mkdtemp,
  readFile,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import packageJson from "./package.json";

const webDir = import.meta.dirname;

describe("i18n:extract script", { timeout: 30_000 }, () => {
  let workspace: string;

  // Runs on a copy of the web app: the script writes to the files it checks.
  beforeEach(async () => {
    workspace = await mkdtemp(join(tmpdir(), "verve-i18n-extract-"));
    await cp(join(webDir, "src"), join(workspace, "src"), { recursive: true });
    await cp(
      join(webDir, "i18next.config.ts"),
      join(workspace, "i18next.config.ts"),
    );
    await symlink(
      join(webDir, "node_modules"),
      join(workspace, "node_modules"),
    );
  });

  afterEach(async () => {
    await rm(workspace, { recursive: true });
  });

  function runScript() {
    const binDir = join(webDir, "node_modules", ".bin");
    execSync(packageJson.scripts["i18n:extract"], {
      cwd: workspace,
      stdio: "pipe",
      env: {
        ...process.env,
        PATH: `${binDir}${delimiter}${process.env.PATH ?? ""}`,
      },
    });
  }

  function localePath(lng: string) {
    return join(workspace, "src", "locales", lng, "common.json");
  }

  async function addTestOnlyKey(lng: string, text: string) {
    const messages = JSON.parse(
      await readFile(localePath(lng), "utf8"),
    ) as object;
    await writeFile(
      localePath(lng),
      `${JSON.stringify({ ...messages, testOnly: { added: text } }, null, 2)}\n`,
    );
  }

  function generatedTypes() {
    return readFile(join(workspace, "src/i18n/resources.d.ts"), "utf8");
  }

  it("given a key added by hand to the locale files and used in code, when the script runs, then the types have the key", async () => {
    await addTestOnlyKey("en", "Added by hand");
    await addTestOnlyKey("fr", "Ajouté à la main");
    // Extraction would remove a key that code does not use, which rewrites the
    // locale files and hides the stale types.
    await writeFile(
      join(workspace, "src/added.ts"),
      `import i18n from "i18next";\n\nexport const added = i18n.t("testOnly.added");\n`,
    );

    runScript();

    expect(await generatedTypes()).toContain(`"added": "Added by hand"`);
  });

  it("given an English text edited by hand, when the script runs, then the types have the new text", async () => {
    const english = await readFile(localePath("en"), "utf8");
    await writeFile(
      localePath("en"),
      english.replace("Page not found", "Nothing here"),
    );

    runScript();

    expect(await generatedTypes()).toContain(`"title": "Nothing here"`);
  });
});
