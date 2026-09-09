import { describe, expect, it } from "vitest";

import { computeDelta, formatBucketKey, formatDuration, formatSummaryValue } from "./format";

// The locale is fr-FR, so a thousands separator is a narrow no-break space and a
// decimal separator is a comma. Asserting on the exact glyph would pin the ICU
// data rather than the rule, so these normalise the spacing and check the shape.
const norm = (s: string) => s.replace(/\s/g, " ");

describe("formatSummaryValue", () => {
  it("abbreviates a large sum, because a Panel headline is read at a glance", () => {
    expect(norm(formatSummaryValue(245_321, "sum"))).toBe("245,3 k");
  });

  it("keeps a small sum in full, where abbreviating would lose more than it saves", () => {
    expect(norm(formatSummaryValue(9_999, "sum"))).toBe("9 999");
  });

  it("never abbreviates a non-sum, however large", () => {
    // A heart rate or a body mass is not a quantity you round to the nearest
    // thousand, and the aggregation is what says which it is.
    expect(norm(formatSummaryValue(74.2, "average"))).toBe("74,2");
    expect(norm(formatSummaryValue(120_000, "latest"))).toBe("120 000");
  });
});

describe("formatDuration", () => {
  it("reads minutes as hours and minutes", () => {
    // Sleep's canonical unit is the minute, and "432" is a number nobody reads as
    // seven hours (ADR 0027).
    expect(formatDuration(432)).toBe("7h 12m");
  });

  it("drops the hours when there are none", () => {
    expect(formatDuration(47)).toBe("47m");
  });

  it("keeps the sign in front, which is what a delta needs", () => {
    expect(formatDuration(-90)).toBe("-1h 30m");
  });

  it("rounds to the minute", () => {
    expect(formatDuration(59.6)).toBe("1h 0m");
  });
});

describe("computeDelta", () => {
  it("reads as a percentage by default", () => {
    const d = computeDelta(112, 100, "sum", false);
    expect(d.arrow).toBe("↑");
    expect(norm(d.label)).toBe("12 %");
  });

  it("falls back to the absolute difference when the Baseline is zero", () => {
    // There is no percentage base to divide by, and "∞ %" is not an answer.
    const d = computeDelta(50, 0, "sum", false);
    expect(d.arrow).toBe("↑");
    expect(norm(d.label)).not.toContain("%");
  });

  it("uses the absolute difference for a signed Metric", () => {
    // A percentage around zero is meaningless: a calorie balance going from -10 to
    // +10 is not a 200% improvement (ADR 0014).
    const d = computeDelta(10, -10, "sum", true);
    expect(norm(d.label)).not.toContain("%");
  });

  it("reads a change that rounds to zero as neutral, not as an increase", () => {
    // The arrow follows the shown magnitude, so "↑ 0 %" never appears.
    const d = computeDelta(100.001, 100, "sum", false);
    expect(d.arrow).toBe("→");
  });

  it("points down for a decrease, with no judgement attached", () => {
    // Direction and magnitude only: Verve does not know which way is good for a
    // given Metric (ADR 0015).
    expect(computeDelta(88, 100, "sum", false).arrow).toBe("↓");
  });
});

describe("formatBucketKey", () => {
  it("passes a day through unchanged", () => {
    expect(formatBucketKey("2024-03-05", "day")).toBe("2024-03-05");
  });

  it("truncates a month to its year and month", () => {
    expect(formatBucketKey("2024-03-01", "month")).toBe("2024-03");
  });

  // The week label is the one place the client derives anything from a date, and
  // it derives it by the ISO rule the server buckets with: the week belongs to the
  // year holding its Thursday. These are the cases where that rule and a naive one
  // disagree, which is the whole reason the rule exists.
  it("labels an ordinary week by its own year", () => {
    expect(formatBucketKey("2024-03-04", "week")).toBe("2024-W10");
  });

  it("gives a year's first days to the previous year when its Thursday is there", () => {
    // Monday 2019-12-30 is the start of ISO week 2020-W01: its Thursday is 2020-01-02.
    expect(formatBucketKey("2019-12-30", "week")).toBe("2020-W01");
  });

  it("gives a year's last days to the next year when its Thursday is there", () => {
    // Monday 2021-01-04 starts 2021-W01; the week before it, starting 2020-12-28,
    // is 2020-W53 because its Thursday (2020-12-31) is still in 2020.
    expect(formatBucketKey("2020-12-28", "week")).toBe("2020-W53");
  });

  it("labels the first full week of a year as W01", () => {
    expect(formatBucketKey("2024-01-01", "week")).toBe("2024-W01");
  });

  it("returns an unparseable key unchanged rather than inventing a label", () => {
    expect(formatBucketKey("not-a-day", "week")).toBe("not-a-day");
  });
});
