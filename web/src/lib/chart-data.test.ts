import { describe, expect, it } from "vitest";

import { mergeSeries, stageKey } from "./chart-data";
import type { Point, Series } from "./types";

const series = (over: Partial<Series> & { points: Point[] }): Series =>
  ({ metric: "steps", unit: "count", aggregation: "sum", bucket: "day", source: "Watch", ...over }) as Series;

const p = (bucket: string, value: number, over: Partial<Point> = {}): Point => ({
  bucket,
  value,
  ...over,
});

describe("mergeSeries, single Metric", () => {
  it("keeps one row per point, in the order the server sent them", () => {
    const data = mergeSeries([series({ points: [p("2024-01-01", 900), p("2024-01-02", 1100)] })]);
    expect(data).toEqual([
      { bucket: "2024-01-01", v0: 900 },
      { bucket: "2024-01-02", v0: 1100 },
    ]);
  });

  it("carries a min/max band only when the point has one", () => {
    const data = mergeSeries([
      series({ points: [p("2024-01-01", 60, { min: 52, max: 71 }), p("2024-01-02", 62)] }),
    ]);
    expect(data[0].band0).toEqual([52, 71]);
    expect(data[1].band0).toBeUndefined();
  });

  it("namespaces a Stage breakdown so it cannot collide with a series key", () => {
    const data = mergeSeries([
      series({ points: [p("2024-01-01", 400, { states: { asleep_core: 300, awake: 20 } })] }),
    ]);
    expect(data[0][stageKey("asleep_core")]).toBe(300);
    // The scalar the tooltip and the Baseline read is untouched beside it: for
    // sleep that is time asleep, so `awake` appears here and is never counted there.
    expect(data[0].v0).toBe(400);
  });
});

describe("mergeSeries, Baseline", () => {
  // The two windows are aligned by ordinal server-side and arrive equal length
  // (ADR 0015), so index i of one belongs beside index i of the other. Pairing by
  // date instead would drop every point, since the dates differ by construction.
  it("pairs the Baseline by position, not by date", () => {
    const current = series({ points: [p("2024-02-01", 100), p("2024-02-02", 200)] });
    const baseline = series({ points: [p("2024-01-01", 10), p("2024-01-02", 20)] });
    const data = mergeSeries([current], baseline);

    expect(data[0].bucket).toBe("2024-02-01");
    expect(data[0].baselineValue).toBe(10);
    expect(data[1].baselineValue).toBe(20);
  });

  it("keeps each Baseline point's own date, which is what the tooltip names", () => {
    const current = series({ points: [p("2024-02-01", 100)] });
    const baseline = series({ points: [p("2024-01-01", 10)] });
    expect(mergeSeries([current], baseline)[0].baselineBucket).toBe("2024-01-01");
  });

  it("leaves a gap bucket without a value while keeping its date", () => {
    // A Baseline slot held open for alignment carries a date and no value: drawing
    // it as a zero would invent a reading the Account never had (ADR 0014).
    const current = series({ points: [p("2024-02-01", 100)] });
    const baseline = series({ points: [p("2024-01-01", 0, { gap: true })] });
    const [row] = mergeSeries([current], baseline);

    expect(row.baselineBucket).toBe("2024-01-01");
    expect(row.baselineValue).toBeUndefined();
  });
});

describe("mergeSeries, several Metrics", () => {
  it("merges on the shared bucket and keys each Series by its position", () => {
    const steps = series({ points: [p("2024-01-01", 900), p("2024-01-02", 1100)] });
    const mass = series({ metric: "body_mass", points: [p("2024-01-02", 80)] });
    const data = mergeSeries([steps, mass]);

    expect(data).toEqual([
      { bucket: "2024-01-01", v0: 900 },
      { bucket: "2024-01-02", v0: 1100, v1: 80 },
    ]);
  });

  it("leaves a Series' key absent where it has no data, rather than zero", () => {
    const steps = series({ points: [p("2024-01-01", 900)] });
    const mass = series({ metric: "body_mass", points: [p("2024-01-02", 80)] });
    const data = mergeSeries([steps, mass]);

    expect(data[0].v1).toBeUndefined();
    expect(data[1].v0).toBeUndefined();
  });

  it("sorts by bucket, so a Series arriving second cannot reorder the axis", () => {
    const a = series({ points: [p("2024-01-03", 3)] });
    const b = series({ metric: "body_mass", points: [p("2024-01-01", 1), p("2024-01-02", 2)] });
    expect(mergeSeries([a, b]).map((d) => d.bucket)).toEqual([
      "2024-01-01",
      "2024-01-02",
      "2024-01-03",
    ]);
  });

  it("ignores a Baseline entirely, because a multi-Metric Panel does not render one", () => {
    // Co-variation between Metrics and comparison between periods are different
    // questions, and are deliberately not superposed (ADR 0020).
    const a = series({ points: [p("2024-02-01", 100)] });
    const b = series({ metric: "body_mass", points: [p("2024-02-01", 80)] });
    const baseline = series({ points: [p("2024-01-01", 10)] });
    const [row] = mergeSeries([a, b], baseline);

    expect(row.baselineValue).toBeUndefined();
    expect(row.baselineBucket).toBeUndefined();
  });
});

describe("mergeSeries trend", () => {
  it("carries the smoothed value and leaves a trendless bucket absent", () => {
    // A bucket with no trend must produce no key at all, not a zero: the chart draws
    // that line with connectNulls={false}, so an absent key is what breaks it across a
    // gap (ADR 0032) and a zero would draw a cliff to the floor.
    const s: Series = {
      metric: "body_mass",
      unit: "kg",
      aggregation: "latest",
      bucket: "day",
      source: "Scale",
      days: 3,
      points: [
        { bucket: "2026-07-15", value: 92.15, trend: 92.15 },
        { bucket: "2026-07-16", value: 91.15 },
        { bucket: "2026-07-17", value: 90.95, trend: 91.8 },
      ],
    };
    const rows = mergeSeries([s]);
    expect(rows[0].trend0).toBe(92.15);
    expect("trend0" in rows[1]).toBe(false);
    expect(rows[2].trend0).toBe(91.8);
  });

  it("keeps two Metrics' trends on separate keys", () => {
    const mk = (metric: string, trend: number): Series => ({
      metric,
      unit: "kg",
      aggregation: "latest",
      bucket: "day",
      source: "Scale",
      days: 1,
      points: [{ bucket: "2026-07-15", value: 1, trend }],
    });
    const rows = mergeSeries([mk("body_mass", 90), mk("lean_body_mass", 66)]);
    expect(rows[0].trend0).toBe(90);
    expect(rows[0].trend1).toBe(66);
  });
});

describe("mergeSeries, Goal line", () => {
  const goal = {
    covered: 4,
    measured: 3,
    met: 1,
    segments: [
      { direction: "at_least" as const, value: 7000, from: "2024-01-01", to: "2024-01-03" },
      { direction: "at_least" as const, value: 7500, from: "2024-01-03", to: "2024-01-05" },
    ],
  };

  it("steps with the Goal in force on each day", () => {
    const data = mergeSeries([
      series({ goal, points: [p("2024-01-01", 1), p("2024-01-02", 1), p("2024-01-03", 1), p("2024-01-06", 1)] }),
    ]);
    expect(data.map((d) => d.goal0)).toEqual([7000, 7000, 7500, undefined]);
  });

  it("draws nothing above day grain, where a bar is not a day", () => {
    const data = mergeSeries([series({ goal, bucket: "week", points: [p("2024-01-01", 1)] })]);
    expect(data[0].goal0).toBeUndefined();
  });

  it("holds a Series' Goal on the rows another Series brought", () => {
    const data = mergeSeries([
      series({ goal, points: [p("2024-01-01", 1)] }),
      series({ metric: "sleep", points: [p("2024-01-02", 420)] }),
    ]);
    expect(data.map((d) => d.goal0)).toEqual([7000, 7000]);
    expect(data.every((d) => d.goal1 === undefined)).toBe(true);
  });
});

describe("mergeSeries, Usual", () => {
  const usual = (low: number, high: number, n = 28) => ({ low, high, n });

  it("carries a lone Metric's Usual as a range, and nothing where the point has none", () => {
    const data = mergeSeries([
      series({ points: [p("2024-01-01", 58, { usual: usual(46, 52) }), p("2024-01-02", 50)] }),
    ]);
    expect(data[0].usual0).toEqual([46, 52]);
    // The whole Usual rides beside the range the band is drawn from, so the tooltip
    // can say how many buckets it rests on.
    expect(data[0].usual).toEqual(usual(46, 52));
    // Absent rather than empty, so the band breaks there instead of bridging (ADR 0032).
    expect(data[1].usual0).toBeUndefined();
  });

  // In comparison the Baseline line is already the reference, and two references
  // behind one curve read as neither.
  it("draws no Usual in comparison", () => {
    const current = series({ points: [p("2024-02-01", 58, { usual: usual(46, 52) })] });
    const baseline = series({ points: [p("2024-01-01", 50)] });
    expect(mergeSeries([current], baseline)[0].usual0).toBeUndefined();
  });

  // Two bands on two axes read as nothing (ADR 0020); the server does not send one
  // for a combo, and a stray one must not be drawn either.
  it("draws no Usual on a multi-Metric Panel", () => {
    const a = series({ points: [p("2024-01-01", 58, { usual: usual(46, 52) })] });
    const b = series({ metric: "steps", points: [p("2024-01-01", 9000)] });
    expect(mergeSeries([a, b])[0].usual0).toBeUndefined();
  });
});
