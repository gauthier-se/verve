import { test as base, expect } from "@playwright/test";

export { expect };

/**
 * The Playwright test, with a page that fails the test when the browser
 * reports an error: a console error, a CSP violation or an uncaught exception.
 */
export const test = base.extend({
  page: async ({ page }, provide) => {
    const errors: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") {
        errors.push(message.text());
      }
    });
    page.on("pageerror", (error) => {
      errors.push(error.message);
    });

    await provide(page);

    // Errors can arrive after the last assertion of a test.
    await page.waitForLoadState("networkidle");
    expect(errors).toEqual([]);
  },
});
