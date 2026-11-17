import { fileURLToPath } from "node:url";

import { admin } from "./admin";
import { expect, test } from "./fixtures";

/**
 * The synthetic export that `go run ./tools/synthexport` writes before the
 * run, as `task e2e` and CI do.
 */
const exportPath = fileURLToPath(
  new URL("../../bin/export.zip", import.meta.url),
);

test("given the built binary, when the admin uploads the fixture export, then the upload page ends on a completed import with its report", async ({
  page,
}) => {
  await page.goto("/login");
  await page.getByLabel("Username or email").fill(admin.username);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Verve" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Imports", exact: true }).click();
  await page
    .getByRole("link", { name: "Import an Apple Health export" })
    .click();
  await page.getByLabel("Choose a file").setInputFiles(exportPath);

  const imported = page.getByRole("region", { name: "Import of export.zip" });
  // The page polls the import every 5 seconds until it ends.
  await expect(imported.getByText("Completed", { exact: true })).toBeVisible({
    timeout: 60_000,
  });
  await expect(imported.getByRole("region", { name: "Period" })).toHaveText(
    /^PeriodFrom .+ to .+$/,
  );
  await expect(imported.getByRole("region", { name: "Routes" })).toHaveText(
    /stored with (its|their) workouts?\.$/,
  );
  await expect(
    imported.getByRole("region", { name: "By type" }).getByRole("listitem"),
  ).not.toHaveCount(0);
});
