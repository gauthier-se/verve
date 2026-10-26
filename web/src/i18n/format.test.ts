import { describe, expect, it } from "vitest";

import { formatDate, formatDuration, formatNumber } from "@/i18n/format";

describe("formatDuration", () => {
  it.each([
    { seconds: 27120, lng: "en", expected: "7h 32m" },
    { seconds: 27120, lng: "fr", expected: "7 h 32 min" },
    { seconds: 45, lng: "en", expected: "45s" },
    { seconds: 45, lng: "fr", expected: "45 s" },
    { seconds: 3600, lng: "en", expected: "1h" },
    { seconds: 3600, lng: "fr", expected: "1 h" },
    { seconds: 90, lng: "en", expected: "1m 30s" },
    { seconds: 90, lng: "fr", expected: "1 min 30 s" },
    { seconds: 1200, lng: "en", expected: "20m" },
    { seconds: 1200, lng: "fr", expected: "20 min" },
  ])(
    "given formatDuration($seconds) in $lng, then it returns $expected",
    ({ seconds, lng, expected }) => {
      expect(formatDuration(seconds, lng)).toBe(expected);
    },
  );
});

describe("formatNumber", () => {
  it.each([
    { lng: "en", expected: "2,150.5" },
    // French groups thousands with a narrow no-break space.
    { lng: "fr", expected: "2 150,5" },
  ])(
    "given formatNumber(2150.5) in $lng, then it equals the Intl.NumberFormat output $expected",
    ({ lng, expected }) => {
      expect(formatNumber(2150.5, lng)).toBe(expected);
      expect(formatNumber(2150.5, lng)).toBe(
        new Intl.NumberFormat(lng).format(2150.5),
      );
    },
  );

  it("given Intl options, when formatting, then they are passed to Intl.NumberFormat", () => {
    expect(formatNumber(0.25, "en", { style: "percent" })).toBe("25%");
  });
});

describe("formatDate", () => {
  const date = new Date(Date.UTC(2026, 9, 26, 12));

  it.each([
    { lng: "en", expected: "10/26/2026" },
    { lng: "fr", expected: "26/10/2026" },
  ])(
    "given formatDate(date) in $lng, then it equals the Intl.DateTimeFormat output $expected",
    ({ lng, expected }) => {
      expect(formatDate(date, lng)).toBe(expected);
      expect(formatDate(date, lng)).toBe(
        new Intl.DateTimeFormat(lng).format(date),
      );
    },
  );

  it("given Intl options, when formatting, then they are passed to Intl.DateTimeFormat", () => {
    expect(
      formatDate(date, "fr", { dateStyle: "long", timeZone: "UTC" }),
    ).toBe("26 octobre 2026");
  });
});
