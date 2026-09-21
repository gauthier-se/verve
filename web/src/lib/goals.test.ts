import { describe, expect, it } from "vitest";

import { attainmentText, describeGoal, goalAt, goalEligible, lastDayHeld, storedGoalValue, todayUTC } from "./goals";
import type { Metric } from "./types";

const metric = (over: Partial<Metric>): Metric => ({
  slug: "x",
  unit: "count",
  nature: "imported",
  aggregation: "sum",
  ...over,
});

const steps = metric({ slug: "steps" });
const sleep = metric({ slug: "sleep", unit: "min", aggregation: "duration_by_state" });
const spo2 = metric({ slug: "oxygen_saturation", unit: "%", aggregation: "average" });
const balance = metric({ slug: "calorie_balance", unit: "kcal", nature: "derived", aggregation: undefined });

describe("goalEligible", () => {
  // Pinned against internal/api/goalhandlers.go, which refuses the same Metrics.
  it("refuses a latest Metric and accepts every other rule", () => {
    expect(goalEligible(metric({ aggregation: "latest" }))).toBe(false);
    expect(goalEligible(steps)).toBe(true);
    expect(goalEligible(sleep)).toBe(true);
    expect(goalEligible(spo2)).toBe(true);
    expect(goalEligible(balance)).toBe(true);
    expect(goalEligible(metric({ aggregation: "sum_by_state" }))).toBe(true);
  });
});

describe("describeGoal", () => {
  it("writes the bound as a fact, per day or per night", () => {
    expect(describeGoal({ direction: "at_least", value: 7500 }, steps)).toBe("At least 7 500 a day");
    expect(describeGoal({ direction: "at_least", value: 420 }, sleep)).toBe("At least 7h 0m a night");
    expect(describeGoal({ direction: "at_most", value: -300 }, balance)).toBe("At most -300 kcal a day");
  });

  it("shows a percent the way it is typed, not the fraction it is stored as", () => {
    expect(describeGoal({ direction: "at_least", value: 0.95 }, spo2)).toBe("At least 95 % a day");
  });
});

describe("storedGoalValue", () => {
  const input = (over: Partial<{ value: string; hours: string; minutes: string }>) => ({
    value: "",
    hours: "",
    minutes: "",
    ...over,
  });

  it("stores a duration in minutes", () => {
    expect(storedGoalValue(input({ hours: "7", minutes: "30" }), sleep)).toBe(450);
    expect(storedGoalValue(input({ hours: "8" }), sleep)).toBe(480);
    expect(storedGoalValue(input({ minutes: "45" }), sleep)).toBe(45);
    expect(storedGoalValue(input({}), sleep)).toBeNull();
    expect(storedGoalValue(input({ hours: "-1" }), sleep)).toBeNull();
  });

  it("stores a percent as a fraction and anything else as typed, negatives included", () => {
    expect(storedGoalValue(input({ value: "95" }), spo2)).toBeCloseTo(0.95);
    expect(storedGoalValue(input({ value: "7500" }), steps)).toBe(7500);
    expect(storedGoalValue(input({ value: "-300" }), balance)).toBe(-300);
    expect(storedGoalValue(input({ value: "" }), steps)).toBeNull();
    expect(storedGoalValue(input({ value: "abc" }), steps)).toBeNull();
  });
});

describe("todayUTC", () => {
  it("is the UTC date, whatever the browser's zone", () => {
    expect(todayUTC(new Date("2026-03-10T23:30:00-05:00"))).toBe("2026-03-11");
  });
});

describe("lastDayHeld", () => {
  it("is the day before the exclusive end, across a month", () => {
    expect(lastDayHeld("2026-03-01")).toBe("2026-02-28");
    expect(lastDayHeld("2026-03-13")).toBe("2026-03-12");
  });
});

describe("attainmentText", () => {
  const seg = (value: number, from: string, to: string) => ({ direction: "at_least" as const, value, from, to });
  const stepsRule = { unit: "count", aggregation: "sum" as const };

  it("counts met over measured, with the bound when one held throughout", () => {
    const t = attainmentText({ covered: 30, measured: 24, met: 18, segments: [seg(7500, "a", "b")] }, stepsRule);
    expect(t.short).toBe("18 of 24 days ≥ 7 500");
    expect(t.title).toContain("6 with no data");
    expect(t.title).toContain("Today is not counted");
  });

  it("says 'at goal' when the Goal changed inside the window", () => {
    const t = attainmentText(
      { covered: 4, measured: 4, met: 2, segments: [seg(7000, "a", "b"), seg(7500, "b", "c")] },
      stepsRule,
    );
    expect(t.short).toBe("2 of 4 days at goal");
  });

  it("writes a sleep bound as a duration and a percent as typed", () => {
    const sleepGoal = { covered: 7, measured: 7, met: 5, segments: [seg(420, "a", "b")] };
    expect(attainmentText(sleepGoal, { unit: "min", aggregation: "duration_by_state" }).short).toBe("5 of 7 days ≥ 7h 0m");
    const spo2Goal = { covered: 7, measured: 7, met: 7, segments: [seg(0.95, "a", "b")] };
    expect(attainmentText(spo2Goal, { unit: "%", aggregation: "average" }).short).toBe("7 of 7 days ≥ 95 %");
  });

  it("never prints 0 of 0", () => {
    expect(attainmentText({ covered: 0, measured: 0, met: 0, segments: [seg(1, "a", "b")] }, stepsRule).short).toBe(
      "goal set, nothing to count yet",
    );
    expect(attainmentText({ covered: 5, measured: 0, met: 0, segments: [seg(1, "a", "b")] }, stepsRule).short).toBe(
      "no measured day under the goal",
    );
  });
});

describe("goalAt", () => {
  it("is half-open, like the segments", () => {
    const a = { covered: 0, measured: 0, met: 0, segments: [{ direction: "at_most" as const, value: 2300, from: "2024-01-01", to: "2024-01-03" }] };
    expect(goalAt(a, "2024-01-01")).toBe(2300);
    expect(goalAt(a, "2024-01-02")).toBe(2300);
    expect(goalAt(a, "2024-01-03")).toBeUndefined();
    expect(goalAt(undefined, "2024-01-01")).toBeUndefined();
  });
});
