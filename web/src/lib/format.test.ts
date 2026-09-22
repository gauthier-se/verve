import { describe, expect, it } from "vitest";

import {
  computeDelta,
  formatAxisValue,
  formatBucketKey,
  formatDuration,
  formatDurationTick,
  formatExact,
  formatFigure,
  formatSummaryValue,
} from "./format";
import { boundText } from "./goals";
import { tsvFigure } from "./clipboard";

// The locale is fr-FR, so a thousands separator is a narrow no-break space and a
// decimal separator is a comma. Asserting on the exact glyph would pin the ICU
// data rather than the rule, so these normalise the spacing and check the shape.
const norm = (s: string) => s.replace(/\s/g, " ");

describe("formatSummaryValue", () => {
  it("abbreviates a large sum, because a Panel headline is read at a glance", () => {
    expect(norm(formatSummaryValue(245_321, "sum", ""))).toBe("245,3 k");
  });

  it("keeps a small sum in full, where abbreviating would lose more than it saves", () => {
    expect(norm(formatSummaryValue(9_999, "sum", ""))).toBe("9 999");
  });

  it("never abbreviates a non-sum, however large", () => {
    // A heart rate or a body mass is not a quantity you round to the nearest
    // thousand, and the aggregation is what says which it is.
    expect(norm(formatSummaryValue(74.2, "average", ""))).toBe("74,2");
    expect(norm(formatSummaryValue(120_000, "latest", ""))).toBe("120 000");
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
    const d = computeDelta(112, 100, "sum", false, "");
    expect(d.arrow).toBe("↑");
    expect(norm(d.label)).toBe("12 %");
  });

  it("falls back to the absolute difference when the Baseline is zero", () => {
    // There is no percentage base to divide by, and "∞ %" is not an answer.
    const d = computeDelta(50, 0, "sum", false, "");
    expect(d.arrow).toBe("↑");
    expect(norm(d.label)).not.toContain("%");
  });

  it("uses the absolute difference for a signed Metric", () => {
    // A percentage around zero is meaningless: a calorie balance going from -10 to
    // +10 is not a 200% improvement (ADR 0014).
    const d = computeDelta(10, -10, "sum", true, "");
    expect(norm(d.label)).not.toContain("%");
  });

  it("reads a change that rounds to zero as neutral, not as an increase", () => {
    // The arrow follows the shown magnitude, so "↑ 0 %" never appears.
    const d = computeDelta(100.001, 100, "sum", false, "");
    expect(d.arrow).toBe("→");
  });

  it("points down for a decrease, with no judgement attached", () => {
    // Direction and magnitude only: Verve does not know which way is good for a
    // given Metric (ADR 0015).
    expect(computeDelta(88, 100, "sum", false, "").arrow).toBe("↓");
  });
});

// A "%" Metric is stored as a fraction (0.969 for 96.9 %). Printed raw, one decimal
// rounds it to "1" beside a "%", right next to a Goal written "≥ 95 %". Every figure
// format reads it through the same rule (toDisplayValue), so these pin that each does.
describe("a percent figure", () => {
  it("reads a stored fraction in 0–100 on every screen", () => {
    expect(norm(formatSummaryValue(0.969, "average", "%"))).toBe("96,9");
    expect(norm(formatFigure(0.969, "average", "%"))).toBe("96,9");
    expect(norm(formatExact(0.9694, "%"))).toBe("96,94");
    expect(formatAxisValue(0.969, "%")).toBe("96.9");
  });

  it("is written on the scale its Goal is written on", () => {
    const rule = { unit: "%", aggregation: "average" as const };
    expect(norm(boundText({ direction: "at_least", value: 0.95 }, rule))).toBe("≥ 95 %");
    expect(norm(formatFigure(0.969, rule.aggregation, rule.unit))).toBe("96,9");
  });

  it("leaves every other unit alone", () => {
    expect(norm(formatFigure(0.969, "average", "kg"))).toBe("1");
    expect(formatAxisValue(0.969, "")).toBe("1.0");
  });

  it("gives an absolute delta in points, and a relative one unchanged", () => {
    // 95 % → 96,9 %: 1,9 points, never "0" from a raw 0.019.
    const abs = computeDelta(0.969, 0.95, "average", true, "%");
    expect(norm(abs.label)).toBe("1,9");
    expect(norm(abs.exact)).toBe("1,9");
    expect(norm(computeDelta(0.969, 0.95, "average", false, "%").label)).toBe("2 %");
  });

  it("copies as the screen shows it, without binary noise", () => {
    // 0.969 × 100 is 96.89999999999999 in floating point; a pasted cell must not be.
    expect(tsvFigure(0.969, "%")).toBe("96.9");
    expect(tsvFigure(0.969, "kg")).toBe("0.969");
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

describe("formatDurationTick", () => {
  // An axis is 40 px wide: "60h 0m" wraps onto two lines there and collides with
  // the tick below it, so a whole hour drops its minutes.
  it("drops zero minutes from a whole hour", () => {
    expect(formatDurationTick(3600)).toBe("60h");
  });

  it("keeps the minutes when there are some", () => {
    expect(formatDurationTick(45 * 60 + 30)).toBe("45h 30m");
  });

  it("reads under an hour in minutes, and zero as 0", () => {
    expect(formatDurationTick(50)).toBe("50m");
    expect(formatDurationTick(0)).toBe("0");
  });
});
