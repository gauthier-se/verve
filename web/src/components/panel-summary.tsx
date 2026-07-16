import { computeDelta, formatExact, formatSummaryValue } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import type { Metric, Series } from "@/lib/types";
import { Swatch, formatBucket } from "./panel-chart";

/** PanelSummary is the headline band above a Panel's curve (ADR 0019): the large
 *  primary figure (the Metric folded over the whole range), the small most-recent
 *  bucket beside it, and — in comparison mode — a neutral delta against the Baseline.
 *  Universal on every Panel; the summary itself is computed server-side. */
export function PanelSummary({
  series,
  baseline,
  metric,
}: {
  series: Series;
  baseline?: Series;
  metric?: Metric;
}) {
  const { summary, points, unit, aggregation, bucket } = series;

  // The primary figure; a gap (no summary) shows "—". Its exact value goes in a tooltip.
  const primary = summary ? formatSummaryValue(summary.value, aggregation) : "—";
  const primaryTitle = summary ? `${formatExact(summary.value)} ${unit}`.trim() : undefined;

  // The secondary is the most recent bucket — a plain read, not a summary. It is
  // hidden for a `latest` Metric, where it coincides with the summary.
  const last = points.length > 0 ? points[points.length - 1] : undefined;
  const showSecondary = last !== undefined && aggregation !== "latest";

  // The delta exists only in comparison mode, and only when both sides are real —
  // a gap on either window has nothing to compare.
  const delta =
    summary && baseline?.summary
      ? computeDelta(summary.value, baseline.summary.value, aggregation, metric?.signed ?? false)
      : undefined;

  return (
    // panel-summary is a query container (index.css) so the secondary figure drops by
    // the band's own width — narrow card, not viewport — without touching the chart.
    <div className="panel-summary flex items-baseline gap-x-2 px-3 pt-1.5">
      <span className="text-2xl font-semibold leading-none tabular-nums" title={primaryTitle}>
        {primary}
      </span>
      {summary && unit && <span className="text-xs text-muted-foreground">{unit}</span>}
      {delta && (
        <span
          className="text-xs tabular-nums text-muted-foreground"
          title={`${delta.arrow} ${delta.exact} ${unit} vs baseline`.trim()}
        >
          {delta.arrow} {delta.label}
        </span>
      )}
      {showSecondary && (
        // panel-summary-secondary is dropped on a narrow card by a container query
        // (index.css) — the first thing to go when space is tight (ADR 0019).
        <span className="panel-summary-secondary ml-auto whitespace-nowrap text-xs tabular-nums text-muted-foreground">
          <span className="opacity-70">{formatBucket(last.bucket, bucket)}</span>{" "}
          {formatSummaryValue(last.value, aggregation)}
        </span>
      )}
    </div>
  );
}

/** PanelLegend is the multi-Metric counterpart of the summary band (ADR 0020):
 *  one entry per Series — its position color, name, and Panel summary folded over
 *  the window ("—" for a gap) — doubling as the chart's color key. The summary
 *  stays universal (ADR 0019); only the single-Metric rendering keeps the large
 *  headline figure. In comparison mode a muted hint says why there is no Baseline
 *  here rather than leaving the control looking broken. */
export function PanelLegend({ list, comparing }: { list: Series[]; comparing?: boolean }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 pt-1.5 text-xs">
      {list.map((s, i) => (
        <span key={s.metric} className="flex items-center gap-1.5">
          <Swatch i={i} />
          <span className="text-muted-foreground">{metricLabel(s.metric)}</span>
          <span
            className="font-medium tabular-nums"
            title={s.summary ? `${formatExact(s.summary.value)} ${s.unit}`.trim() : undefined}
          >
            {s.summary ? formatSummaryValue(s.summary.value, s.aggregation) : "—"}
          </span>
          {s.summary && s.unit && <span className="text-muted-foreground">{s.unit}</span>}
        </span>
      ))}
      {comparing && (
        <span
          className="ml-auto text-muted-foreground/70"
          title="Period comparison overlays a Baseline on single-metric panels only — co-variation and comparison don't stack."
        >
          no baseline
        </span>
      )}
    </div>
  );
}
