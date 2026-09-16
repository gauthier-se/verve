import { describe, expect, it } from "vitest";
import { SERIES_COLORS, seriesColor, standaloneColorOffset } from "./chart";

describe("seriesColor", () => {
  it("is position within the ramp, and wraps", () => {
    expect(seriesColor(0)).toBe(SERIES_COLORS[0]);
    expect(seriesColor(4)).toBe(SERIES_COLORS[0]);
  });

  it("rotates uniformly, so an offset can never fold two Series onto one colour", () => {
    // The property ADR 0036 rests on: whatever the offset, four positions stay four
    // colours. A non-uniform scheme would collide and the ADR 0026 separation
    // guarantee would stop meaning anything.
    for (let offset = 0; offset < 8; offset += 1) {
      const drawn = [0, 1, 2, 3].map((i) => seriesColor(i, offset));
      expect(new Set(drawn).size).toBe(SERIES_COLORS.length);
    }
  });
});

describe("standaloneColorOffset", () => {
  it("is stable for a slug", () => {
    expect(standaloneColorOffset("body_mass")).toBe(standaloneColorOffset("body_mass"));
  });

  it("stays inside the ramp", () => {
    for (const slug of ["steps", "body_mass", "dietary_energy", "heart_rate", ""]) {
      const offset = standaloneColorOffset(slug);
      expect(offset).toBeGreaterThanOrEqual(0);
      expect(offset).toBeLessThan(SERIES_COLORS.length);
    }
  });

  it("separates slugs that share a prefix", () => {
    // The reason this is FNV-1a and not a sum of char codes: the Catalog is full of
    // `dietary_*` families, and a sum would land the whole family on neighbouring
    // slots, which is the thing this helper exists to avoid.
    const family = [
      "dietary_energy",
      "dietary_protein",
      "dietary_fat_total",
      "dietary_carbohydrates",
    ];
    expect(new Set(family.map(standaloneColorOffset)).size).toBeGreaterThan(1);
  });

  it("spreads a realistic Catalog across every colour", () => {
    const slugs = [
      "steps",
      "body_mass",
      "body_fat_percentage",
      "lean_body_mass",
      "heart_rate",
      "resting_heart_rate",
      "dietary_energy",
      "dietary_protein",
      "dietary_fat_total",
      "dietary_carbohydrates",
      "active_energy",
      "basal_energy",
      "sleep",
      "vo2_max",
      "walking_speed",
      "running_speed",
    ];
    const used = new Set(slugs.map(standaloneColorOffset));
    expect(used.size).toBe(SERIES_COLORS.length);
  });
});
