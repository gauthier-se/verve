import { describe, expect, it } from "vitest";
import { hasBreakdown, OTHER_SEGMENT, segmentColor, segmentLabel, segmentsPresent } from "./breakdown";
import type { Activity, Point, Series } from "./types";

const series = (aggregation: string): Series =>
  ({ metric: "x", unit: "min", aggregation, bucket: "day", points: [] }) as unknown as Series;

const point = (states: Record<string, number>): Point =>
  ({ bucket: "2026-03-01", value: Object.values(states).reduce((a, b) => a + b, 0), states }) as Point;

const activities = new Map<string, Activity>([
  ["running", { slug: "running", label: "Running", group: "cardio", reading: "pace" }],
  [
    "high_intensity_interval_training",
    { slug: "high_intensity_interval_training", label: "HIIT", group: "cardio", reading: "none" },
  ],
]);

describe("hasBreakdown", () => {
  it("is true for both by-state rules and false for the rest", () => {
    expect(hasBreakdown(series("duration_by_state"))).toBe(true);
    expect(hasBreakdown(series("sum_by_state"))).toBe(true);
    expect(hasBreakdown(series("sum"))).toBe(false);
    expect(hasBreakdown(undefined)).toBe(false);
  });
});

describe("segmentsPresent", () => {
  it("orders an open set by the window's totals, largest first", () => {
    const points = [point({ running: 30, cycling: 90 }), point({ running: 80 })];
    expect(segmentsPresent("training_time", points)).toEqual(["running", "cycling"]);
  });

  it("prefers the summary's totals when the Series carries one", () => {
    const points = [point({ running: 10, cycling: 90 })];
    const summary = point({ running: 400, cycling: 90 });
    expect(segmentsPresent("training_time", points, summary)).toEqual(["running", "cycling"]);
  });

  it("stacks `other` last whatever its size", () => {
    const points = [point({ other: 500, running: 30, cycling: 20 })];
    expect(segmentsPresent("training_time", points)).toEqual(["running", "cycling", OTHER_SEGMENT]);
  });

  it("keeps sleep's fixed Stage order rather than the data's", () => {
    const points = [point({ awake: 30, asleep_deep: 60, asleep_rem: 90 })];
    expect(segmentsPresent("sleep", points)).toEqual(["asleep_deep", "asleep_rem", "awake"]);
  });

  it("reports nothing for points with no breakdown", () => {
    expect(segmentsPresent("training_time", [{ bucket: "2026-03-01", value: 10 } as Point])).toEqual([]);
  });
});

describe("segmentLabel", () => {
  it("uses the Catalog's curated label for an Activity", () => {
    expect(segmentLabel("training_time", "high_intensity_interval_training", activities)).toBe("HIIT");
  });

  it("prettifies an Activity the table does not list", () => {
    expect(segmentLabel("training_time", "underwater_diving", activities)).toBe("Underwater Diving");
  });

  it("names `other` rather than prettifying it", () => {
    expect(segmentLabel("training_time", OTHER_SEGMENT, activities)).toBe("Other activities");
  });

  it("labels a sleep Stage", () => {
    expect(segmentLabel("sleep", "asleep_rem")).toBe("REM");
  });
});

describe("segmentColor", () => {
  it("gives a Stage its fixed slot, whatever its position in this night", () => {
    expect(segmentColor("sleep", "asleep_deep", 3)).toBe(segmentColor("sleep", "asleep_deep", 0));
  });

  it("gives an Activity the slot of its position, since the set is open", () => {
    expect(segmentColor("training_time", "running", 0)).not.toBe(segmentColor("training_time", "cycling", 1));
  });

  it("always paints `other` in the last slot", () => {
    expect(segmentColor("training_time", OTHER_SEGMENT, 0)).toBe(segmentColor("training_time", OTHER_SEGMENT, 4));
  });
});
