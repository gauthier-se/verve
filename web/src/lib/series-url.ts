// The query a Series read carries, in one place because two callers send it: the
// chart's fetch (useSeries) and the CSV download (ADR 0039). The file and the
// curve have to be the same read, and two builders is how they stop being one.
import type { BaselineParams } from "@/hooks/use-series";
import type { RangeTokens } from "./time-range";
import type { Bucket } from "./types";

/** seriesParams builds the query string /v1/series and /v1/series.csv share:
 *  one repeated `metric` per Metric so the server resolves the time axis once
 *  for all of them (ADR 0020), the range tokens (bounds only for a custom
 *  range, since relative presets resolve server-side), the optional bucket
 *  override, and the comparison Baseline when there is one. */
export function seriesParams(params: {
  metrics: string[];
  range: RangeTokens;
  bucket: Bucket | null;
  baseline?: BaselineParams;
}): URLSearchParams {
  const { metrics, range, bucket, baseline } = params;
  const comparing = baseline !== undefined && baseline.rule !== "none";

  const qs = new URLSearchParams({ range_preset: range.preset });
  for (const metric of metrics) qs.append("metric", metric);
  if (range.preset === "custom") {
    if (range.from) qs.set("range_from", range.from);
    if (range.to) qs.set("range_to", range.to);
  }
  if (bucket) qs.set("bucket", bucket);
  if (comparing) {
    qs.set("baseline_rule", baseline.rule);
    if (baseline.rule === "custom") {
      if (baseline.from) qs.set("baseline_from", baseline.from);
      if (baseline.to) qs.set("baseline_to", baseline.to);
    }
  }
  return qs;
}

/** seriesCsvHref is the download link for the numbers behind a curve: the same
 *  window and the same bucket as the screen, served by the same read module so
 *  the file cannot disagree with the chart.
 *
 *  It never carries a Baseline, and not because it is inconvenient: a CSV holds
 *  one window, and the server answers 422 rather than quietly returning one of
 *  the two a caller asked for. */
export function seriesCsvHref(params: {
  metrics: string[];
  range: RangeTokens;
  bucket: Bucket | null;
}): string {
  return `/v1/series.csv?${seriesParams(params).toString()}`;
}

/** archiveHref is the whole Account as a Verve Archive (ADR 0039). It is a plain
 *  link rather than a fetch: the session cookie is same-origin, the browser owns
 *  the download, and pulling a hundred megabytes through JavaScript to hand it
 *  back as a blob would buy nothing but a memory ceiling. */
export const archiveHref = "/v1/export/archive";
