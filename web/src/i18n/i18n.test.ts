import { describe, expect, it } from "vitest";

import { initI18n } from "@/i18n";

describe("translations", () => {
  it("given a key missing from the French file, when French is used, then the English text is displayed", async () => {
    const i18n = await initI18n();
    i18n.addResource("en", "common", "testOnly.englishOnly", "English only");
    await i18n.changeLanguage("fr");

    // The key exists only for this test, so it is not in the generated types.
    expect(i18n.t("testOnly.englishOnly" as never)).toBe("English only");
  });

  it.each([
    { lng: "en", count: 0, expected: "0 days" },
    { lng: "en", count: 1, expected: "1 day" },
    { lng: "en", count: 2, expected: "2 days" },
    { lng: "fr", count: 0, expected: "0 jour" },
    { lng: "fr", count: 1, expected: "1 jour" },
    { lng: "fr", count: 2, expected: "2 jours" },
  ])(
    "given the plural key days, when $lng renders count $count, then it shows $expected",
    async ({ lng, count, expected }) => {
      const i18n = await initI18n();
      await i18n.changeLanguage(lng);

      expect(i18n.t("days", { count })).toBe(expected);
    },
  );
});
