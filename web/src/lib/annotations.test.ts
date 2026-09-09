import { describe, expect, it } from "vitest";

import { projectAnnotations } from "./annotations";
import type { Annotation } from "./types";

// A marker is placed by matching a bucket key against the categories a chart drew.
// Recharts matches a reference element's x by equality, so a placement that is one
// boundary off does not error: it renders nothing, silently, on exactly the days
// the two rules disagree. That is why this is tested by value rather than by
// reading the source for banned date arithmetic.

const note = (id: number, bucket?: string, endBucket?: string): Annotation => ({
  id,
  label: `note ${id}`,
  body: null,
  starts_on: "2024-01-01",
  ends_on: null,
  bucket,
  end_bucket: endBucket,
});

const week = ["2024-01-01", "2024-01-08", "2024-01-15", "2024-01-22"];

describe("projectAnnotations", () => {
  it("places a marker on the bucket the server named", () => {
    const { markers, bands, byBucket } = projectAnnotations([note(1, "2024-01-08")], week);
    expect(markers).toEqual([{ bucket: "2024-01-08", count: 1 }]);
    expect(bands).toEqual([]);
    expect(byBucket.get("2024-01-08")).toHaveLength(1);
  });

  it("collapses several notes in one bucket onto one marker carrying a count", () => {
    const { markers, byBucket } = projectAnnotations(
      [note(1, "2024-01-08"), note(2, "2024-01-08")],
      week,
    );
    // Stacked labels at bar width are illegible by the second one, so the mark
    // carries a count and the labels live in the tooltip.
    expect(markers).toEqual([{ bucket: "2024-01-08", count: 2 }]);
    expect(byBucket.get("2024-01-08")).toHaveLength(2);
  });

  it("draws a band for a span covering more than one drawn bucket", () => {
    const { markers, bands, byBucket } = projectAnnotations(
      [note(1, "2024-01-08", "2024-01-15")],
      week,
    );
    expect(bands).toEqual([{ id: 1, from: "2024-01-08", to: "2024-01-15" }]);
    // The band still carries one mark at its start, and its label repeats on every
    // bucket it covers: that repetition is what makes a band readable at all.
    expect(markers).toEqual([{ bucket: "2024-01-08", count: 1 }]);
    expect(byBucket.get("2024-01-08")).toHaveLength(1);
    expect(byBucket.get("2024-01-15")).toHaveLength(1);
  });

  it("collapses a span narrower than the gap between two drawn buckets", () => {
    // Three days inside one week bucket: there is no second category to reach, so
    // it is a marker rather than a band of zero width.
    const { markers, bands } = projectAnnotations([note(1, "2024-01-09", "2024-01-11")], week);
    expect(bands).toEqual([]);
    expect(markers).toEqual([{ bucket: "2024-01-15", count: 1 }]);
  });

  it("nudges a note in an empty bucket onto the first bucket drawn after it", () => {
    // A Series is sparse: a bucket with no data is absent from the payload and so
    // from the axis, and the illness that flattens a curve is often the week that
    // empties it. The mark moves; the tooltip still names the real dates.
    const sparse = ["2024-01-01", "2024-01-22"];
    const { markers } = projectAnnotations([note(1, "2024-01-08")], sparse);
    expect(markers).toEqual([{ bucket: "2024-01-22", count: 1 }]);
  });

  it("draws nothing for a note past the last drawn bucket", () => {
    const { markers, bands, byBucket } = projectAnnotations([note(1, "2024-06-01")], week);
    expect(markers).toEqual([]);
    expect(bands).toEqual([]);
    expect(byBucket.size).toBe(0);
  });

  it("ignores an unfolded note, which has no place on an axis", () => {
    // A list fetched with no time axis carries no bucket. Placing it would mean
    // deriving one here, which is the server's job.
    const { markers } = projectAnnotations([note(1, undefined)], week);
    expect(markers).toEqual([]);
  });

  it("returns an empty overlay for no notes and for no categories", () => {
    expect(projectAnnotations([], week).markers).toEqual([]);
    expect(projectAnnotations([note(1, "2024-01-08")], []).markers).toEqual([]);
    expect(projectAnnotations(undefined, week).markers).toEqual([]);
  });
});
