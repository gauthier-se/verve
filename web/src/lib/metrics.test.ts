import { describe, expect, it } from "vitest";

import { compatibleChartTypes, defaultChartType } from "./metrics";
import type { Metric } from "./types";

// These rules exist twice: here, and in internal/api/dashboardhandlers.go, which
// refuses a Panel the client would have offered. Nothing compiles the two against
// each other, so this at least pins this side: a change here that the server has
// not made shows up as a 422 the user cannot act on.

const metric = (over: Partial<Metric>): Metric => ({
  slug: "x",
  unit: "u",
  nature: "imported",
  ...over,
});

describe("defaultChartType", () => {
  it("gives a signed Metric the diverging bar, whatever it aggregates", () => {
    // Signed wins over the aggregation: a calorie balance is read around zero, and
    // that reading is lost if the axis fits to same-sign data (ADR 0014).
    expect(defaultChartType(metric({ signed: true, aggregation: "sum" }))).toBe("diverging_bar");
    expect(defaultChartType(metric({ signed: true, nature: "derived" }))).toBe("diverging_bar");
  });

  it("follows the aggregation rule otherwise", () => {
    expect(defaultChartType(metric({ aggregation: "sum" }))).toBe("bar");
    expect(defaultChartType(metric({ aggregation: "average" }))).toBe("band");
    expect(defaultChartType(metric({ aggregation: "duration_by_state" }))).toBe("stacked_bar");
    expect(defaultChartType(metric({ aggregation: "latest" }))).toBe("line");
  });

  it("gives an unsigned derived Metric a line, since it has no aggregation of its own", () => {
    // A derived Metric aggregates each operand by its own rule and applies the
    // Formula per bucket, so it carries no rule to key off (ADR 0014).
    expect(defaultChartType(metric({ nature: "derived" }))).toBe("line");
  });
});

describe("compatibleChartTypes", () => {
  it("always offers the default among the choices", () => {
    const cases: Metric[] = [
      metric({ aggregation: "sum" }),
      metric({ aggregation: "average" }),
      metric({ aggregation: "duration_by_state" }),
      metric({ aggregation: "latest" }),
      metric({ signed: true, aggregation: "sum" }),
      metric({ nature: "derived" }),
    ];
    for (const m of cases) {
      expect(compatibleChartTypes(m)).toContain(defaultChartType(m));
    }
  });

  it("keeps the band variant to average Metrics, which are the only ones with a band", () => {
    expect(compatibleChartTypes(metric({ aggregation: "average" }))).toContain("band");
    expect(compatibleChartTypes(metric({ aggregation: "sum" }))).not.toContain("band");
    expect(compatibleChartTypes(metric({ aggregation: "latest" }))).not.toContain("band");
  });

  it("keeps the stacked bar to duration_by_state, and offers nothing else", () => {
    // Sleep's segments are its Stages; there is no other reading of that shape.
    expect(compatibleChartTypes(metric({ aggregation: "duration_by_state" }))).toEqual([
      "stacked_bar",
    ]);
  });

  it("keeps the diverging bar to signed Metrics", () => {
    expect(compatibleChartTypes(metric({ signed: true, aggregation: "sum" }))).toContain(
      "diverging_bar",
    );
    for (const agg of ["sum", "average", "latest"] as const) {
      expect(compatibleChartTypes(metric({ aggregation: agg }))).not.toContain("diverging_bar");
    }
  });
});
