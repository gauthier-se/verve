import { describe, expect, it } from "vitest";
import { curveMetrics, formatElapsed, workoutData } from "./workout-series";
import type { SessionStat, WorkoutSeries } from "./types";

const series: WorkoutSeries = {
  metric: "heart_rate",
  unit: "count/min",
  source: "Apple Watch",
  start_at: "2026-03-01T06:00:00Z",
  end_at: "2026-03-01T07:00:00Z",
  points: [
    { at: "2026-03-01T06:00:00Z", value: 120, count: 2 },
    { at: "2026-03-01T06:30:00Z", value: 150, count: 3 },
  ],
};

describe("workoutData", () => {
  it("reads the axis as elapsed time, not as a clock", () => {
    expect(workoutData(series)).toEqual([
      { elapsed: 0, value: 120 },
      { elapsed: 1800, value: 150 },
    ]);
  });

  it("has nothing to draw without a series", () => {
    expect(workoutData(undefined)).toEqual([]);
  });

  it("drops a point whose timestamp does not parse rather than drawing it at the start", () => {
    const broken = { ...series, points: [{ at: "not a time", value: 99, count: 1 }] };
    expect(workoutData(broken)).toEqual([]);
  });
});

describe("formatElapsed", () => {
  it("names minutes into the ride, and hours only when there are any", () => {
    expect(formatElapsed(90)).toBe("1:30");
    expect(formatElapsed(3900)).toBe("1:05:00");
    expect(formatElapsed(0)).toBe("0:00");
  });
});

describe("curveMetrics", () => {
  it("offers what this workout measured, once each", () => {
    const stats: SessionStat[] = [
      { metric: "heart_rate", stat: "average", value: 140, unit: "count/min" },
      { metric: "heart_rate", stat: "max", value: 176, unit: "count/min" },
      { metric: "active_energy_burned", stat: "sum", value: 620, unit: "kcal" },
    ];
    expect(curveMetrics(stats)).toEqual(["heart_rate", "active_energy_burned"]);
  });

  it("leaves out a stat that is only a min or a max", () => {
    const stats: SessionStat[] = [{ metric: "running_power", stat: "max", value: 300, unit: "W" }];
    expect(curveMetrics(stats)).toEqual([]);
  });
});
