import { describe, expect, it } from "vitest";

import { isSleepSeries, stageColor, stageLabel, stagesPresent } from "./sleep";
import type { Point, Series } from "./types";

const point = (bucket: string, states: Record<string, number>): Point => ({
  bucket,
  value: Object.entries(states)
    .filter(([s]) => s.startsWith("asleep"))
    .reduce((n, [, m]) => n + m, 0),
  states,
});

describe("stagesPresent", () => {
  it("lists the Stages a window holds, in stack order rather than in data order", () => {
    // The order is the stack's, bottom to top, so a night reads the same way on
    // every chart whatever order the server happened to send.
    const points = [point("2024-01-01", { awake: 20, asleep_core: 200, asleep_deep: 60 })];
    expect(stagesPresent(points)).toEqual(["asleep_deep", "asleep_core", "awake"]);
  });

  it("omits a Stage the window does not hold", () => {
    // An empty column explains nothing, so a night with no REM does not get a REM
    // segment sitting at zero.
    const points = [point("2024-01-01", { asleep_core: 200 })];
    expect(stagesPresent(points)).toEqual(["asleep_core"]);
  });

  it("unions across buckets, so a Stage in one night is drawn for the window", () => {
    const points = [
      point("2024-01-01", { asleep_core: 200 }),
      point("2024-01-02", { asleep_rem: 90, asleep_core: 180 }),
    ];
    expect(stagesPresent(points)).toEqual(["asleep_core", "asleep_rem"]);
  });

  it("keeps a Stage this build has never heard of, after the known ones", () => {
    // A Connector may send a Stage newer than this build. An unlabelled segment is
    // better than a missing one.
    const points = [point("2024-01-01", { asleep_core: 200, asleep_shallow: 30 })];
    expect(stagesPresent(points)).toEqual(["asleep_core", "asleep_shallow"]);
  });

  it("returns nothing for points carrying no breakdown", () => {
    expect(stagesPresent([{ bucket: "2024-01-01", value: 400 }])).toEqual([]);
    expect(stagesPresent([])).toEqual([]);
  });
});

describe("stageLabel", () => {
  it("humanises a known Stage", () => {
    expect(stageLabel("asleep_rem")).toBe("REM");
    expect(stageLabel("in_bed")).toBe("In bed");
  });

  it("falls back to the prettified slug for an unknown one", () => {
    expect(stageLabel("asleep_shallow")).toBe("asleep shallow");
  });
});

describe("stageColor", () => {
  it("gives a Stage the same colour whatever else a Night contains", () => {
    // A night with no REM must not repaint deep sleep, so the slot is fixed rather
    // than assigned by position in what happens to be present.
    expect(stageColor("asleep_deep", 0)).toBe(stageColor("asleep_deep", 3));
  });

  it("draws awake recessed rather than as a fourth kind of sleep", () => {
    // Awake minutes are stacked so a broken night looks broken, and are never
    // counted as sleep (ADR 0027). A ramp colour would make it a kind of sleep and
    // give the least important segment the most saturated treatment.
    const awake = stageColor("awake", 0);
    for (const stage of ["asleep_deep", "asleep_core", "asleep_rem", "asleep", "in_bed"]) {
      expect(stageColor(stage, 0)).not.toBe(awake);
    }
  });

  it("gives every known Stage a colour of its own", () => {
    const stages = ["asleep_deep", "asleep_core", "asleep_rem", "asleep", "in_bed", "awake"];
    expect(new Set(stages.map((s) => stageColor(s, 0))).size).toBe(stages.length);
  });
});

describe("isSleepSeries", () => {
  it("keys off the aggregation rule rather than the Metric slug", () => {
    // Any duration_by_state Metric stacks, which is what lets training volume use
    // the same shape later without naming it here.
    expect(isSleepSeries({ aggregation: "duration_by_state" } as Series)).toBe(true);
    expect(isSleepSeries({ aggregation: "sum" } as Series)).toBe(false);
    expect(isSleepSeries(undefined)).toBe(false);
  });
});
