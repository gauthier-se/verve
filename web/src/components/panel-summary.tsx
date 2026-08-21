import { Link } from "@tanstack/react-router";
import { computeDelta, formatDuration, formatExact, formatSummaryValue } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import type { Metric, Series } from "@/lib/types";
import { FormulaHint } from "./formula-hint";
import { Figure, Unit } from "./ui/figure";
import { MetricIcon } from "./metric-icon";
import { useSummaryPrefs } from "./panel-prefs";
import { Swatch, formatBucket } from "./panel-chart";

/** isExtensive reports whether a Metric's value scales with the window length — a
 *  `sum`, or a derived Metric that is a plain weighted sum (no denominator, e.g. total
 *  energy = active + basal, calorie balance) — so a per-day average is meaningful. A
 *  ratio (a Formula with a denominator) and the `average`/`latest` rules are intensive
 *  and keep their value. */
function isExtensive(aggregation: Series["aggregation"], metric?: Metric): boolean {
  if (aggregation === "sum" || aggregation === "duration_by_state") return true;
  const formula = metric?.formula;
  return formula !== undefined && (formula.denominator?.length ?? 0) === 0;
}

/** SummaryMode is how a Series summary is shown: the plain window total, an extensive
 *  Metric's per-day average, sleep's per-night average, or a `latest` Metric's window
 *  mean. */
type SummaryMode = "total" | "per_day" | "per_night" | "mean";

/** summaryMode picks the mode from the global "average" toggle: an extensive Metric
 *  averages per day — per *night* for sleep, whose window accumulates over Nights and
 *  not over days (ADR 0027) — a `latest` Metric shows its window mean, and an
 *  intensive Metric (an `average` rate, a ratio) has no meaningful average and stays
 *  as its total. */
function summaryMode(aggregation: Series["aggregation"], metric: Metric | undefined, average: boolean): SummaryMode {
  if (!average) return "total";
  if (aggregation === "duration_by_state") return "per_night";
  if (isExtensive(aggregation, metric)) return "per_day";
  if (aggregation === "latest") return "mean";
  return "total";
}

/** summaryFigureValue is the number a Series shows in a mode: the per-day average
 *  (total ÷ its own window days), the window mean, or the plain summary total.
 *  Undefined is a gap — no data, or a missing figure. */
function summaryFigureValue(s: Series, mode: SummaryMode): number | undefined {
  switch (mode) {
    case "per_day":
      return s.summary && (s.days ?? 0) > 0 ? s.summary.value / (s.days as number) : undefined;
    // Nights with data, never the window's days: the Watch spends nights on a charger,
    // and dividing by 30 would report a shortfall the Account never had (ADR 0027).
    case "per_night":
      return s.summary && (s.nights ?? 0) > 0 ? s.summary.value / (s.nights as number) : undefined;
    case "mean":
      return s.mean;
    default:
      return s.summary?.value;
  }
}

/** figureText renders a summary figure: minutes of sleep read as a duration ("7h 12m"),
 *  everything else through the number formats the prefs choose. */
function figureText(value: number, aggregation: Series["aggregation"], exact: boolean): string {
  if (aggregation === "duration_by_state") return formatDuration(value);
  return exact ? formatExact(value) : formatSummaryValue(value, aggregation);
}

/** modeNote is the words a mode adds to a title attribute. */
function modeNote(mode: SummaryMode): string {
  switch (mode) {
    case "per_day":
      return " per day";
    case "per_night":
      return " per night";
    case "mean":
      return " average";
    default:
      return "";
  }
}

/** FigureUnit is the small unit marker beside a figure. A duration carries its own
 *  unit in its text, so it shows only what the plain number cannot say: the divisor. */
function FigureUnit({ unit, aggregation, mode }: { unit: string; aggregation: Series["aggregation"]; mode: SummaryMode }) {
  if (aggregation === "duration_by_state") {
    return mode === "per_night" ? <Unit divisor="/night" /> : null;
  }
  if (!unit) return null;
  return (
    <Unit divisor={mode === "per_day" ? "/day" : mode === "mean" ? " avg" : undefined}>{unit}</Unit>
  );
}

/** PanelSummary is the headline band above a Panel's curve (ADR 0019): the large
 *  primary figure (the Metric folded over the whole range), the small most-recent
 *  bucket beside it, and — in comparison mode — a neutral delta against the Baseline.
 *  Universal on every Panel; the summary itself is computed server-side. The global
 *  summary prefs (per-day average for `sum`, exact vs compact numbers) only change how
 *  the server figure is *shown* — a `sum` total is divided by the window's own day
 *  count (server-provided), so nothing is re-aggregated client-side. */
export function PanelSummary({
  series,
  baseline,
  metric,
  wide,
  size,
}: {
  series: Series;
  baseline?: Series;
  metric?: Metric;
  /** wide raises the headline to a two-column Panel's size. */
  wide?: boolean;
  /** size overrides the headline outright — the Metric page's hero. */
  size?: "hero" | "wide" | "panel";
}) {
  const { prefs } = useSummaryPrefs();
  const { points, unit, aggregation, bucket } = series;

  // Average mode shows each Metric as its period average, the better trend view:
  //  - extensive (a `sum`, or a derived plain weighted sum like total energy / calorie
  //    balance) → the server total ÷ its own window days (a per-day average);
  //  - `latest` (body mass) → the server window mean, not the last reading;
  //  - everything else (an `average` rate, a ratio) is already intensive — unchanged.
  // Nothing is re-aggregated client-side: both the total and the mean come from the
  // server, per window (so a Baseline of a different length compares fairly).
  const mode = summaryMode(aggregation, metric, prefs.average);
  const figure = (s: Series) => summaryFigureValue(s, mode);
  const fmt = (value: number) => figureText(value, aggregation, prefs.exact);

  // The primary figure; a gap (no value) shows "—". Its exact value goes in a tooltip.
  const primaryValue = figure(series);
  const primary = primaryValue !== undefined ? fmt(primaryValue) : "—";
  const noteLabel = modeNote(mode);
  const primaryTitle =
    primaryValue !== undefined ? `${formatExact(primaryValue)} ${unit}${noteLabel}`.trim() : undefined;

  // The secondary is the most recent bucket — a plain read. It is hidden for a `latest`
  // Metric in total mode (it coincides with the summary), but shown in mean mode where
  // the primary is the window mean, so the latest reading is extra information.
  const last = points.length > 0 ? points[points.length - 1] : undefined;
  const showSecondary = last !== undefined && (aggregation !== "latest" || mode === "mean");

  // The delta exists only in comparison mode, and only when both sides are real. Each
  // period is folded on the same basis, so the comparison is average-vs-average (or
  // total-vs-total). The compared period's own figure is always shown beside the delta.
  let delta: ReturnType<typeof computeDelta> | undefined;
  let baselineShown: string | undefined;
  const baseValue = baseline ? figure(baseline) : undefined;
  if (primaryValue !== undefined && baseValue !== undefined) {
    delta = computeDelta(primaryValue, baseValue, aggregation, metric?.signed ?? false);
    // Show both numbers, so "↓ 18 %" / "→ 0 %" is legible — not a bare percentage
    // against an invisible reference.
    baselineShown = fmt(baseValue);
  }

  return (
    // panel-summary is a query container (index.css) so the secondary figure drops by
    // the band's own width — narrow card, not viewport — without touching the chart.
    <div className="panel-summary flex items-baseline gap-x-2 px-4 pt-2">
      <Figure size={size ?? (wide ? "wide" : "panel")} title={primaryTitle}>
        {primary}
      </Figure>
      {primaryValue !== undefined && <FigureUnit unit={unit} aggregation={aggregation} mode={mode} />}
      {delta && (
        // Never green, never red: a delta is a direction and a magnitude, and Verve
        // does not know which direction is good for your Metric (ADR 0019).
        <span
          className="font-mono text-2xs tabular-nums text-muted-foreground"
          title={`${delta.arrow} ${delta.exact} ${unit} vs the compared period`.trim()}
        >
          {delta.arrow} {delta.label}
          {baselineShown && <span className="opacity-70"> (vs {baselineShown})</span>}
        </span>
      )}
      {showSecondary && (
        // panel-summary-secondary is dropped on a narrow card by a container query
        // (index.css) — the first thing to go when space is tight (ADR 0019).
        <span className="panel-summary-secondary ml-auto whitespace-nowrap font-mono text-2xs tabular-nums text-muted-foreground">
          <span className="opacity-70">{formatBucket(last.bucket, bucket)}</span> {fmt(last.value)}
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
export function PanelLegend({
  list,
  comparing,
  catalog,
}: {
  list: Series[];
  comparing?: boolean;
  catalog?: Map<string, Metric>;
}) {
  const { prefs } = useSummaryPrefs();
  return (
    <div className="flex flex-wrap items-baseline gap-x-3.5 gap-y-1 px-4 pt-2 text-2xs">
      {list.map((s, i) => {
        const m = catalog?.get(s.metric);
        const formula = m?.formula;
        const mode = summaryMode(s.aggregation, m, prefs.average);
        const value = summaryFigureValue(s, mode);
        const shown = value === undefined ? "—" : figureText(value, s.aggregation, prefs.exact);
        const note = modeNote(mode);
        return (
        <span key={s.metric} className="flex items-baseline gap-1.5">
          <Swatch i={i} />
          <MetricIcon slug={s.metric} className="size-3 -translate-y-px" />
          <Link to="/data/$metric" params={{ metric: s.metric }} className="text-muted-foreground hover:text-foreground hover:underline">
            {metricLabel(s.metric)}
          </Link>
          {formula && <FormulaHint formula={formula} />}
          <span
            className="font-mono font-medium tabular-nums"
            title={value !== undefined ? `${formatExact(value)} ${s.unit}${note}`.trim() : undefined}
          >
            {shown}
          </span>
          {value !== undefined && <FigureUnit unit={s.unit} aggregation={s.aggregation} mode={mode} />}
        </span>
        );
      })}
      {comparing && (
        <span
          className="ml-auto font-mono text-3xs text-muted-foreground/70"
          title="Period comparison overlays a Baseline on single-metric panels only — co-variation and comparison don't stack."
        >
          no baseline
        </span>
      )}
    </div>
  );
}
