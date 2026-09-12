import { describe, expect, it } from "vitest";
import { archiveHref, seriesCsvHref, seriesParams } from "./series-url";
import type { RangeTokens } from "./time-range";

const preset: RangeTokens = { preset: "30d", from: null, to: null };
const custom: RangeTokens = { preset: "custom", from: "2026-06-01", to: "2026-07-01" };

describe("seriesParams", () => {
  it("repeats the metric parameter, one per Metric", () => {
    const qs = seriesParams({ metrics: ["steps", "body_mass"], range: preset, bucket: null });
    expect(qs.getAll("metric")).toEqual(["steps", "body_mass"]);
    expect(qs.get("range_preset")).toBe("30d");
  });

  it("sends bounds only for a custom range, since a preset resolves server-side", () => {
    expect(seriesParams({ metrics: ["steps"], range: preset, bucket: null }).has("range_from")).toBe(false);

    const qs = seriesParams({ metrics: ["steps"], range: custom, bucket: "week" });
    expect(qs.get("range_from")).toBe("2026-06-01");
    expect(qs.get("range_to")).toBe("2026-07-01");
    expect(qs.get("bucket")).toBe("week");
  });

  it("carries a comparison Baseline, with bounds only for a custom one", () => {
    const relative = seriesParams({
      metrics: ["steps"],
      range: preset,
      bucket: null,
      baseline: { rule: "previous" },
    });
    expect(relative.get("baseline_rule")).toBe("previous");
    expect(relative.has("baseline_from")).toBe(false);

    const frozen = seriesParams({
      metrics: ["steps"],
      range: preset,
      bucket: null,
      baseline: { rule: "custom", from: "2025-06-01", to: "2025-07-01" },
    });
    expect(frozen.get("baseline_from")).toBe("2025-06-01");
    expect(frozen.get("baseline_to")).toBe("2025-07-01");
  });

  it("treats the `none` rule as no comparison at all", () => {
    const qs = seriesParams({ metrics: ["steps"], range: preset, bucket: null, baseline: { rule: "none" } });
    expect(qs.has("baseline_rule")).toBe(false);
  });
});

describe("seriesCsvHref", () => {
  it("asks the CSV endpoint the question the chart is asking", () => {
    expect(seriesCsvHref({ metrics: ["steps"], range: custom, bucket: "day" })).toBe(
      "/v1/series.csv?range_preset=custom&metric=steps&range_from=2026-06-01&range_to=2026-07-01&bucket=day",
    );
  });

  // A CSV holds one window and the server answers 422 for a Baseline, so the
  // link never offers one: the refusal is avoided rather than provoked.
  it("cannot carry a Baseline", () => {
    const href = seriesCsvHref({ metrics: ["steps"], range: preset, bucket: null });
    expect(href).not.toContain("baseline");
  });
});

describe("archiveHref", () => {
  it("is the export endpoint, as a plain link", () => {
    expect(archiveHref).toBe("/v1/export/archive");
  });
});
