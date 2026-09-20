import { describe, expect, it } from "vitest";
import { isEmptyDay, isValidDay, rowState, shiftDay, today } from "./day";
import type { Day, DayMetric } from "./types";

const metric = (over: Partial<DayMetric> = {}): DayMetric => ({
  metric: "steps",
  unit: "count",
  aggregation: "sum",
  ...over,
});

const emptyDay: Day = {
  date: "2026-03-03",
  metrics: [],
  sessions: [],
  annotations: [],
  manual_entries: [],
  exclusions: [],
};

describe("shiftDay", () => {
  it("moves within a month", () => {
    expect(shiftDay("2026-03-03", 1)).toBe("2026-03-04");
    expect(shiftDay("2026-03-03", -1)).toBe("2026-03-02");
  });

  it("crosses a month boundary in both directions", () => {
    expect(shiftDay("2026-03-31", 1)).toBe("2026-04-01");
    expect(shiftDay("2026-03-01", -1)).toBe("2026-02-28");
  });

  it("crosses a year boundary", () => {
    expect(shiftDay("2026-12-31", 1)).toBe("2027-01-01");
    expect(shiftDay("2026-01-01", -1)).toBe("2025-12-31");
  });

  it("knows a leap day", () => {
    expect(shiftDay("2028-02-28", 1)).toBe("2028-02-29");
    expect(shiftDay("2026-02-28", 1)).toBe("2026-03-01");
  });

  // The whole reason this is UTC arithmetic: a date that drifts by a day on its way
  // to the URL is worse than no navigation, and it would only drift for half the
  // world, which is how such a bug survives a review.
  it("does not drift with the local zone", () => {
    expect(shiftDay("2026-03-08", 0)).toBe("2026-03-08");
    expect(shiftDay("2026-11-01", 0)).toBe("2026-11-01");
  });
});

describe("isValidDay", () => {
  it("accepts a real date", () => {
    expect(isValidDay("2026-03-03")).toBe(true);
  });

  it("refuses what the server would refuse", () => {
    for (const bad of ["last-tuesday", "2026-13-40", "2026-03", "2026-02-30", "", "2026-3-3"]) {
      expect(isValidDay(bad)).toBe(false);
    }
  });
});

describe("rowState", () => {
  it("reads a value as a value", () => {
    expect(rowState(metric({ value: 6500 }))).toBe("value");
  });

  it("tells a refusal from a gap", () => {
    expect(rowState(metric({ excluded: true }))).toBe("excluded");
    expect(rowState(metric())).toBe("gap");
  });

  // A zero is a value. A pinned Metric that recorded nothing and one that recorded
  // no steps are different days, and collapsing them is the mistake this exists to
  // prevent (ADR 0014).
  it("does not read a zero as a gap", () => {
    expect(rowState(metric({ value: 0 }))).toBe("value");
  });
});

describe("isEmptyDay", () => {
  it("is true for a date nothing happened on", () => {
    expect(isEmptyDay(emptyDay)).toBe(true);
  });

  it("is false as soon as anything is on it", () => {
    expect(isEmptyDay({ ...emptyDay, metrics: [metric({ value: 1 })] })).toBe(false);
    expect(isEmptyDay({ ...emptyDay, annotations: [{ id: 1, label: "flu", body: null, starts_on: "2026-03-03", ends_on: null }] })).toBe(false);
  });

  // An Exclusion is a standing rule, not something that happened on the date: a day
  // whose only content is "this Metric is refused" is still an empty day.
  it("ignores the standing rules", () => {
    const rules = [{ id: 1, metric: "body_fat_percentage", starts_on: "", ends_on: "", purged: 12, created_at: "2026-01-01T00:00:00Z" }];
    expect(isEmptyDay({ ...emptyDay, exclusions: rules })).toBe(true);
  });
});

describe("today", () => {
  it("is a valid day", () => {
    expect(isValidDay(today())).toBe(true);
  });
});
