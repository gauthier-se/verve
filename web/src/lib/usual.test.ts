import { describe, expect, it } from "vitest";

import { usualLine } from "./usual";

const show = (v: number) => `${v}`;

describe("usualLine", () => {
  it("places the value against the band, bounds included, and names where it came from", () => {
    const u = { low: 46, high: 52, n: 28 };
    expect(usualLine(58, u, "day", show)).toBe("above your usual 46–52 (28 days before)");
    expect(usualLine(52, u, "day", show)).toBe("within your usual 46–52 (28 days before)");
    expect(usualLine(46, u, "day", show)).toBe("within your usual 46–52 (28 days before)");
    expect(usualLine(44, u, "day", show)).toBe("below your usual 46–52 (28 days before)");
  });

  it("reads weeks at week grain", () => {
    expect(usualLine(9, { low: 7, high: 8, n: 12 }, "week", show)).toBe(
      "above your usual 7–8 (12 weeks before)",
    );
  });

  // A band from 17 of 28 days reads differently from one from all of them, and the
  // gap is the owner's to see.
  it("says how many of the window's buckets it was drawn from when some were missing", () => {
    expect(usualLine(50, { low: 46, high: 52, n: 17 }, "day", show)).toBe(
      "within your usual 46–52 (from 17 of the 28 days before)",
    );
  });

  it("formats each bound as the value is, and names the unit once", () => {
    expect(usualLine(58, { low: 46, high: 52, n: 28 }, "day", show, "bpm")).toBe(
      "above your usual 46–52 bpm (28 days before)",
    );
    expect(usualLine(390, { low: 420, high: 450, n: 28 }, "day", (v) => `${v / 60}h`)).toBe(
      "below your usual 7h–7.5h (28 days before)",
    );
  });
});
