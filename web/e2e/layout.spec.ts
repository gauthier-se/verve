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
  // The test instance has no account, so the session is faked. A long
  // username checks that the user menu cannot push the page wider.
  const username = "an-admin-with-a-username-longer-than-a-phone-screen";
  await page.route("/api/v1/auth/me", (route) =>
    route.fulfill({ json: { id: 1, username, role: "admin" } }),
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
  await expectTouchTarget(page.getByRole("menuitem", { name: "Log out" }));
  await expectNoHorizontalScroll(page);
});
