import { describe, expect, it } from "vitest";
import { clockTime, efficiencyBasisLabel, hourTicks, nightBands, nightSpan } from "./night";
import type { NightInterval } from "./types";

const night: NightInterval[] = [
  { state: "asleep_core", start_at: "2026-03-01T23:00:00Z", end_at: "2026-03-02T02:00:00Z" },
  { state: "awake", start_at: "2026-03-02T02:00:00Z", end_at: "2026-03-02T02:30:00Z" },
  { state: "asleep_deep", start_at: "2026-03-02T02:30:00Z", end_at: "2026-03-02T05:00:00Z" },
];

describe("nightSpan", () => {
  it("is the night's own bounds, not the day's", () => {
    const span = nightSpan(night);
    expect(span).not.toBeNull();
    expect(new Date(span!.from).toISOString()).toBe("2026-03-01T23:00:00.000Z");
    expect(new Date(span!.to).toISOString()).toBe("2026-03-02T05:00:00.000Z");
  });

  it("is null for a night with nothing in it", () => {
    expect(nightSpan([])).toBeNull();
  });
});

describe("nightBands", () => {
  it("places each interval as a share of the span", () => {
    const bands = nightBands(night);
    expect(bands).toHaveLength(3);
    expect(bands[0].leftPct).toBe(0);
    expect(bands[0].widthPct).toBeCloseTo(50, 5); // three hours of six
    expect(bands[1].leftPct).toBeCloseTo(50, 5);
    expect(bands[2].leftPct + bands[2].widthPct).toBeCloseTo(100, 5);
  });

  it("drops a zero-length interval rather than drawing a stage that had no time", () => {
    const bands = nightBands([
      ...night,
      { state: "asleep_rem", start_at: "2026-03-02T03:00:00Z", end_at: "2026-03-02T03:00:00Z" },
    ]);
    expect(bands.map((b) => b.state)).not.toContain("asleep_rem");
  });

  it("has nothing to place for an empty night", () => {
    expect(nightBands([])).toEqual([]);
  });
});

describe("hourTicks", () => {
  it("labels the whole hours inside the span", () => {
    expect(hourTicks(night).map((t) => t.label)).toEqual(["00:00", "01:00", "02:00", "03:00", "04:00"]);
  });
});

describe("clockTime", () => {
  it("reads a timestamp as the hour of the night", () => {
    expect(clockTime("2026-03-01T23:14:00Z")).toBe("23:14");
    expect(clockTime(undefined)).toBe("");
    expect(clockTime("not a time")).toBe("");
  });
});

describe("efficiencyBasisLabel", () => {
  it("says what the percentage was divided by", () => {
    expect(efficiencyBasisLabel("onset_to_wake")).toBe("from falling asleep to waking");
    expect(efficiencyBasisLabel(undefined)).toBe("");
  });
});
