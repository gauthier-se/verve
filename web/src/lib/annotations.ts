// Projecting Annotations onto a chart's own X categories. The server has already
// folded each note onto the resolved bucket grid (ADR 0030); this module only
// decides where, among the categories a given chart actually drew, each one lands.
//
// Nothing here parses or shifts a date. Bucket keys are YYYY-MM-DD, so lexical
// order is chronological and a comparison is all it takes. Any date arithmetic in
// this file would be a second implementation of "which bucket holds this day", and
// its disagreements with the server would be invisible: Recharts matches a
// reference element's `x` against a category by equality, so a marker one boundary
// rule off simply does not draw.
import type { Annotation } from "./types";

/** AnnotationMarker is one vertical mark: a category, and how many notes collapsed
 *  onto it. Several notes in one bucket share a mark carrying a count, because
 *  stacked labels at bar width are illegible by the second one. */
export interface AnnotationMarker {
  bucket: string;
  count: number;
}

/** AnnotationBand is a span wide enough to cover more than one drawn category. */
export interface AnnotationBand {
  id: number;
  from: string;
  to: string;
}

/** AnnotationOverlay is everything a chart needs to draw its notes: the marks, the
 *  bands, and, per category, the notes covering it. A span's label repeats on
 *  every bucket it covers, which is what makes a band readable at all. */
export interface AnnotationOverlay {
  markers: AnnotationMarker[];
  bands: AnnotationBand[];
  byBucket: Map<string, Annotation[]>;
}

const EMPTY: AnnotationOverlay = { markers: [], bands: [], byBucket: new Map() };

/** projectAnnotations places folded Annotations on the categories a chart drew.
 *
 *  A Series is sparse: its points come from a `GROUP BY`, so a bucket holding no
 *  data is absent from the payload and therefore absent from the axis, and the case
 *  is not hypothetical: the illness that flattens a curve is often the week that
 *  empties it. A note whose bucket is one of those is placed on the first drawn
 *  category at or after it, and the tooltip still names its real dates, so a mark
 *  nudged one bucket along never misstates when the thing happened. A note past the
 *  last drawn category has nowhere to go and draws nothing. */
export function projectAnnotations(
  annotations: Annotation[] | undefined,
  categories: string[],
): AnnotationOverlay {
  if (!annotations?.length || categories.length === 0) return EMPTY;

  const counts = new Map<string, number>();
  const bands: AnnotationBand[] = [];
  const byBucket = new Map<string, Annotation[]>();

  for (const a of annotations) {
    // An unfolded Annotation (a list fetched with no time axis) has no place here.
    if (!a.bucket) continue;
    const start = firstAtOrAfter(categories, a.bucket);
    if (start < 0) continue;

    // A span narrower than the gap between two drawn categories collapses onto its
    // start: end < start means no drawn bucket falls inside it.
    let end = a.end_bucket ? lastAtOrBefore(categories, a.end_bucket) : start;
    if (end < start) end = start;

    counts.set(categories[start], (counts.get(categories[start]) ?? 0) + 1);
    if (end > start) bands.push({ id: a.id, from: categories[start], to: categories[end] });
    for (let i = start; i <= end; i++) {
      const bucket = categories[i];
      const notes = byBucket.get(bucket);
      if (notes) notes.push(a);
      else byBucket.set(bucket, [a]);
    }
  }

  const markers = [...counts.entries()].map(([bucket, count]) => ({ bucket, count }));
  return { markers, bands, byBucket };
}

/** firstAtOrAfter is the index of the first category not before bucket, or -1. */
function firstAtOrAfter(categories: string[], bucket: string): number {
  for (let i = 0; i < categories.length; i++) {
    if (categories[i] >= bucket) return i;
  }
  return -1;
}

/** lastAtOrBefore is the index of the last category not after bucket, or -1. */
function lastAtOrBefore(categories: string[], bucket: string): number {
  for (let i = categories.length - 1; i >= 0; i--) {
    if (categories[i] <= bucket) return i;
  }
  return -1;
}
