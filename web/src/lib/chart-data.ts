// Folding Series into the per-bucket rows a chart is drawn from.
//
// This is a pure data transform, and it lives here rather than beside the chart
// because it is where a Panel's data is actually decided: which buckets appear,
// what counts as a gap, where a Baseline point is placed. A component can be read
// by eye; this has to be read by test.
import type { Series } from "./types";

/** ChartDatum is one x-position: per-series values keyed v0…v3 (band0… for the
 *  min/max band), sparse — a Series without data in that bucket has no key (a gap,
 *  ADR 0014). Single-Metric comparison adds the Baseline bucket keyed to the same
 *  ordinal index with its own date for the tooltip (ADR 0015). */
export interface ChartDatum {
  bucket: string;
  baselineValue?: number;
  baselineBucket?: string;
  [seriesKey: `v${number}` | `band${number}` | `stage:${string}`]: number | number[] | undefined;
}

/** stageKey namespaces a Stage's minutes on the datum, so a Stage slug can never
 *  collide with a series key. */
export function stageKey(stage: string): `stage:${string}` {
  return `stage:${stage}`;
}

/** mergeSeries folds sparse Series into per-bucket rows, keyed by the shared
 *  bucket the server resolved for all of them.
 *
 *  The single-Metric path pairs the Baseline **by position**, not by date: the two
 *  windows are aligned by ordinal server-side (ADR 0015) and arrive equal length,
 *  so index i of one belongs beside index i of the other, and each keeps its own
 *  date for the tooltip. A multi-Metric Panel takes no Baseline at all — comparing
 *  periods and comparing Metrics are different questions and are not superposed
 *  (ADR 0020) — so that path merges on the bucket key instead. */
export function mergeSeries(list: Series[], baseline?: Series): ChartDatum[] {
  if (list.length === 1) {
    return list[0].points.map((p, i) => {
      const bp = baseline?.points[i];
      const d: ChartDatum = { bucket: p.bucket, v0: p.value };
      if (p.min !== undefined && p.max !== undefined) d.band0 = [p.min, p.max];
      // The Stage breakdown a stacked bar reads, carried beside the value the
      // tooltip and the Baseline still use (ADR 0027).
      for (const [stage, minutes] of Object.entries(p.states ?? {})) d[stageKey(stage)] = minutes;
      if (bp) {
        d.baselineBucket = bp.bucket;
        if (!bp.gap) d.baselineValue = bp.value;
      }
      return d;
    });
  }

  const rows = new Map<string, ChartDatum>();
  list.forEach((s, i) => {
    for (const p of s.points) {
      let row = rows.get(p.bucket);
      if (!row) {
        row = { bucket: p.bucket };
        rows.set(p.bucket, row);
      }
      row[`v${i}`] = p.value;
      if (p.min !== undefined && p.max !== undefined) row[`band${i}`] = [p.min, p.max];
    }
  });
  // Bucket dates are YYYY-MM-DD, so lexical order is chronological.
  return [...rows.values()].sort((a, b) => (a.bucket < b.bucket ? -1 : 1));
}
