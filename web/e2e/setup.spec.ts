import { expect, test } from "./fixtures";
import { readSetupLink } from "./setup-link";

// The wizard proposes the timezone of the browser.
test.use({ timezoneId: "Asia/Tokyo" });

const password = "a long passphrase for the first admin";

test("given the built binary on an empty data directory, when the installer opens the setup link of its log and completes the wizard, then the admin can log out and log in", async ({
  page,
  baseURL,
}) => {
  if (baseURL === undefined) {
    throw new Error("The project has no base URL");
  }
  // The binary prints the link before it is ready, but e2e/server.ts may not
  // have written it yet.
  let link: string | undefined;
  await expect
    .poll(async () => (link = await readSetupLink(baseURL)))
    .toBeDefined();
  if (link === undefined) {
    throw new Error("The server printed no setup link");
  }
  const token = new URL(link, baseURL).hash.slice(1);
  expect(token).not.toBe("");
  const requested: string[] = [];
  page.on("request", (request) => {
    requested.push(request.url());
  });

  await page.goto(link);

  await expect(
    page.getByRole("heading", { level: 2, name: "Admin account" }),
  ).toBeVisible();
  expect(page.url()).toBe(new URL("/setup", baseURL).href);
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();

  await expect(page.getByLabel("Timezone")).toHaveValue("Asia/Tokyo");
  await page.getByRole("button", { name: "Continue" }).click();

  await page.getByRole("link", { name: "Open Verve" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Verve" }),
  ).toBeVisible();

  await page.getByRole("banner").getByRole("button", { name: "admin" }).click();
  await page.getByRole("menuitem", { name: "Log out" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Log in" }),
  ).toBeVisible();

  await page.getByLabel("Username or email").fill("admin");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Verve" }),
  ).toBeVisible();
  // The timezone of the wizard is stored: the app does not ask for it again.
  await expect(page.getByRole("dialog")).toBeHidden();
  expect(requested.filter((url) => url.includes(token))).toEqual([]);
});
