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
