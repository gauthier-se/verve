// The functions passed to page.evaluate run in the browser.
/// <reference lib="dom" />

import type { Locator, Page } from "@playwright/test";

import { expect, test } from "./fixtures";

// The narrowest screen that every page supports.
test.use({ viewport: { width: 360, height: 740 } });

/** The smallest width and height of a touch target, in CSS pixels. */
const minTouchTarget = 44;

async function expectNoHorizontalScroll(page: Page) {
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(scrollWidth).toBeLessThanOrEqual(clientWidth);
}

async function expectTouchTarget(target: Locator) {
  const box = await target.boundingBox();
  expect(box?.width).toBeGreaterThanOrEqual(minTouchTarget);
  expect(box?.height).toBeGreaterThanOrEqual(minTouchTarget);
}

test("given a phone screen, when the login page loads, then it fits without horizontal scroll and its controls are touch targets", async ({
  page,
}) => {
  await page.goto("/login");
  await expect(
    page.getByRole("heading", { level: 1, name: "Log in" }),
  ).toBeVisible();

  await expectNoHorizontalScroll(page);
  await expectTouchTarget(page.getByRole("link", { name: "Verve" }));
  await expectTouchTarget(page.getByLabel("Username or email"));
  await expectTouchTarget(page.getByLabel("Password"));
  await expectTouchTarget(page.getByRole("button", { name: "Log in" }));
});

test("given a phone screen and a session, when a protected page loads, then the shell fits without horizontal scroll and its navigation is made of touch targets", async ({
  page,
}) => {
  // The session is faked, so that the test passes whether the setup test has
  // created the admin or not. A long username checks that the user menu cannot
  // push the page wider.
  const username = "an-admin-with-a-username-longer-than-a-phone-screen";
  await page.route("/api/v1/auth/me", (route) =>
    route.fulfill({
      json: { id: 1, username, role: "admin", timezone: "Europe/Paris" },
    }),
  );

  await page.goto("/");
  const header = page.getByRole("banner");
  const userMenu = header.getByRole("button", { name: username });
  await expect(userMenu).toBeVisible();

  await expectNoHorizontalScroll(page);
  await expectTouchTarget(header.getByRole("link", { name: "Verve" }));
  await expectTouchTarget(header.getByRole("link", { name: "Home" }));
  await expectTouchTarget(header.getByRole("link", { name: "Administration" }));
  await expectTouchTarget(userMenu);

  await userMenu.click();
  // The menu zooms in as it opens, which scales its items until it ends.
  await page
    .getByRole("menu")
    .evaluate((menu) =>
      Promise.all(menu.getAnimations().map((animation) => animation.finished)),
    );
  await expectTouchTarget(page.getByRole("menuitem", { name: "Log out" }));
  await expectNoHorizontalScroll(page);
});

test("given a phone screen and a user without a timezone, when a protected page loads, then the timezone dialog fits without horizontal scroll and its controls are touch targets", async ({
  page,
}) => {
  await page.route("/api/v1/auth/me", (route) =>
    route.fulfill({
      json: { id: 1, username: "admin", role: "admin", timezone: null },
    }),
  );

  await page.goto("/");
  const dialog = page.getByRole("dialog", { name: "Confirm your timezone" });
  await expect(dialog).toBeVisible();
  // The dialog zooms in as it opens, which scales its controls until it ends.
  await dialog.evaluate((element) =>
    Promise.all(element.getAnimations().map((animation) => animation.finished)),
  );

  await expectNoHorizontalScroll(page);
  await expectTouchTarget(dialog.getByLabel("Timezone"));
  await expectTouchTarget(dialog.getByRole("button", { name: "Not now" }));
  await expectTouchTarget(dialog.getByRole("button", { name: "Confirm" }));
});

test("given a phone screen and tokens, when the tokens page loads, then the page fits without horizontal scroll, the table scrolls on its own and the creation dialog fits, also once it shows the token", async ({
  page,
}) => {
  await page.route("/api/v1/auth/me", (route) =>
    route.fulfill({
      json: {
        id: 1,
        username: "admin",
        role: "admin",
        timezone: "Europe/Paris",
      },
    }),
  );
  // A long name checks that a cell cannot push the page wider.
  const name = "an-automation-with-a-name-longer-than-a-phone-screen";
  const token = {
    id: 1,
    name,
    scopes: ["ingest", "read"],
    prefix: "vrv_7Tq2",
    createdAt: "2026-10-01T09:00:00Z",
    lastUsedAt: "2026-11-10T08:30:00Z",
    expiresAt: "2027-10-01T09:00:00Z",
  };
  await page.route("/api/v1/account/tokens", (route) =>
    route.request().method() === "POST"
      ? route.fulfill({
          status: 201,
          json: {
            ...token,
            id: 2,
            token: "vrv_3fK9Qm7Lp2Xw8Rt4Vy6Bn1Cz5Dh0Gj3Ks9Ue2Fa7Ho4",
          },
        })
      : route.fulfill({ json: { items: [token] } }),
  );

  await page.goto("/settings/tokens");
  const table = page.getByRole("table", { name: "API tokens" });
  await expect(table.getByText(name)).toBeVisible();

  await expectNoHorizontalScroll(page);
  const tableScroll = await table.evaluate((element) => {
    const container = element.parentElement;
    return {
      overflowX: container ? getComputedStyle(container).overflowX : "",
      scrollWidth: container?.scrollWidth ?? 0,
      clientWidth: container?.clientWidth ?? 0,
    };
  });
  expect(tableScroll.overflowX).toBe("auto");
  expect(tableScroll.scrollWidth).toBeGreaterThan(tableScroll.clientWidth);
  await expectTouchTarget(page.getByRole("button", { name: "Create token" }));

  await page.getByRole("button", { name: "Create token" }).click();
  const dialog = page.getByRole("dialog", { name: "Create a token" });
  await expect(dialog).toBeVisible();
  await dialog.evaluate((element) =>
    Promise.all(element.getAnimations().map((animation) => animation.finished)),
  );
  await expectNoHorizontalScroll(page);
  await expectTouchTarget(dialog.getByLabel("Name"));
  await expectTouchTarget(dialog.getByRole("button", { name: "Create" }));

  await dialog.getByLabel("Name").fill("iPhone");
  await dialog.getByRole("button", { name: "Create" }).click();
  const created = page.getByRole("dialog", { name: "Token created" });
  await expect(created).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectTouchTarget(created.getByLabel("Token"));
  await expectTouchTarget(created.getByRole("button", { name: "Copy token" }));
  await expectTouchTarget(created.getByLabel("URL"));
  await expectTouchTarget(created.getByLabel("Header value"));
});
