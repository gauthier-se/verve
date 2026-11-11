import { describe, expect, it } from "vitest";

import { initI18n } from "@/i18n";
import enCommon from "@/locales/en/common.json";
import frCommon from "@/locales/fr/common.json";

/** The entries of a locale file by their full key, such as "notFound.title". */
function entries(messages: object, prefix = ""): [string, unknown][] {
  return Object.entries(messages).flatMap(([key, value]: [string, unknown]) =>
    typeof value === "object" && value !== null
      ? entries(value, `${prefix}${key}.`)
      : [[`${prefix}${key}`, value]],
  );
}

describe("translations", () => {
  it("given any English text, then French has a text for the same key", () => {
    const french = new Map(entries(frCommon));

    const missing = entries(enCommon)
      .map(([key]) => key)
      .filter((key) => {
        const text = french.get(key);
        return typeof text !== "string" || text.trim() === "";
      });

    expect(missing).toEqual([]);
  });

  it("given a key missing from the French file, when French is used, then the English text is displayed", async () => {
    const i18n = await initI18n();
    i18n.addResource("en", "common", "testOnly.englishOnly", "English only");
    await i18n.changeLanguage("fr");

    // @ts-expect-error The key exists only in this test, not in the generated types.
    expect(i18n.t("testOnly.englishOnly")).toBe("English only");
  });

  it.each([
    { lng: "en", code: "unauthorized" },
    { lng: "en", code: "forbidden" },
    { lng: "en", code: "csrf_failed" },
    { lng: "en", code: "invalid_credentials" },
    { lng: "en", code: "rate_limited" },
    { lng: "fr", code: "unauthorized" },
    { lng: "fr", code: "forbidden" },
    { lng: "fr", code: "csrf_failed" },
    { lng: "fr", code: "invalid_credentials" },
    { lng: "fr", code: "rate_limited" },
  ])(
    "given the API error code $code, then $lng has a message for it",
    async ({ lng, code }) => {
      const i18n = await initI18n();

      expect(i18n.getResource(lng, "common", `errors.${code}`)).toEqual(
        expect.stringMatching(/\S/),
      );
    },
  );

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

  it.each([
    { lng: "en", name: "Heart rate", value: "Core sleep" },
    { lng: "fr", name: "Fréquence cardiaque", value: "Sommeil léger" },
  ])(
    "given a metric key, when $lng translates it in the metrics namespace, then it shows its name and the names of its values",
    async ({ lng, name, value }) => {
      const i18n = await initI18n();
      await i18n.changeLanguage(lng);

      expect(i18n.t("heart_rate.name", { ns: "metrics" })).toBe(name);
      expect(
        i18n.t("sleep_analysis.values.asleep_core", { ns: "metrics" }),
      ).toBe(value);
    },
  );
});
