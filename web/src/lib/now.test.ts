import { describe, expect, it } from "vitest";
import { ageText, lagText, splitSources } from "./now";
import type { SourceFreshness } from "./types";

describe("ageText", () => {
  it.each([
    [0, "today"],
    [1, "yesterday"],
    [2, "2 days ago"],
    [20, "20 days ago"],
    [21, "3 weeks ago"],
    [35, "5 weeks ago"],
    [59, "8 weeks ago"],
    [60, "2 months ago"],
    [400, "13 months ago"],
    [800, "2 years ago"],
  ])("%i days reads %s", (days, want) => {
    expect(ageText(days)).toBe(want);
  });

  it("never reads a future row as a negative age", () => {
    expect(ageText(-3)).toBe("today");
  });
});

describe("lagText", () => {
  it("says nothing about the Source that recorded last", () => {
    expect(lagText(0)).toBeUndefined();
  });
  it("counts days, then weeks", () => {
    expect(lagText(1)).toBe("1 day behind");
    expect(lagText(12)).toBe("12 days behind");
    expect(lagText(22)).toBe("3 weeks behind");
  });
});

describe("splitSources", () => {
  const src = (source: string, lag: number, retired = false): SourceFreshness => ({
    source,
    last_day: "2026-09-01",
    lag_days: lag,
    retired,
  });

  it("keeps the server's order on both sides", () => {
    const { active, retired } = splitSources([
      src("Apple Watch", 0),
      src("iPhone", 3),
      src("Zepp Life", 40, true),
      src("Old iPhone", 2000, true),
    ]);
    expect(active.map((s) => s.source)).toEqual(["Apple Watch", "iPhone"]);
    expect(retired.map((s) => s.source)).toEqual(["Zepp Life", "Old iPhone"]);
  });
});
