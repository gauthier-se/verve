import * as React from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Pin, PinOff } from "lucide-react";
import { useMetricMap } from "@/hooks/use-catalog";
import { usePins, useAddPin, useRemovePin } from "@/hooks/use-pins";
import { useSeries } from "@/hooks/use-series";
import { defaultChartType, metricLabel } from "@/lib/metrics";
import { formatExact, formatSummaryValue } from "@/lib/format";
import { RANGE_PRESETS, type RangeTokens } from "@/lib/time-range";
import type { Aggregation, Series } from "@/lib/types";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { CenteredSpinner } from "./spinner";
import { FormulaHint } from "./formula-hint";
import { LedgerDetailTable } from "./ledger-detail-table";
import { MetricIcon } from "./metric-icon";
import { formatBucket, PanelChart } from "./panel-chart";
import { PanelSummary } from "./panel-summary";

type Preset = (typeof RANGE_PRESETS)[number]["value"];

/** MetricPage is one Metric's own full page — Apple Health's per-metric screen: a
 *  big current figure, its trend chart, the window's highs and lows, and the
 *  chronological history underneath. It reuses the same Series (`GET /v1/series`)
 *  and the same chart/summary/table components a Panel and the Ledger already
 *  render — nothing here is re-aggregated client-side (ADR 0012). Reached from a
 *  Ledger row; carries no Baseline of its own (a Dashboard-wide concept, ADR 0015). */
export function MetricPage() {
  const { metric } = useParams({ from: "/data/$metric" });
  const catalog = useMetricMap();
  const [preset, setPreset] = React.useState<Preset>("3m");
  const range: RangeTokens = { preset, from: null, to: null };

  const query = useSeries({ metrics: [metric], range, bucket: null });
  const series = query.data?.series[0];
  const meta = catalog.map.get(metric);

  if (catalog.isLoading) {
    return <CenteredSpinner />;
  }

  if (!meta) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
        <p className="text-sm text-muted-foreground">“{metric}” isn’t in the Catalog.</p>
        <Button asChild variant="outline" size="sm">
          <Link to="/data">
            <ArrowLeft className="size-3.5" /> Back to Data
          </Link>
        </Button>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <div className="flex items-center gap-2">
          <Button asChild variant="ghost" size="icon" className="size-7" aria-label="Back to Data">
            <Link to="/data">
              <ArrowLeft className="size-4" />
            </Link>
          </Button>
          <MetricIcon slug={metric} className="size-5" />
          <h1 className="text-xl font-semibold">{metricLabel(metric)}</h1>
          {meta?.formula && <FormulaHint formula={meta.formula} />}
          <PinToggle metric={metric} />
        </div>
        <div className="flex items-center rounded-md border p-0.5">
          {RANGE_PRESETS.map((p) => (
            <Button
              key={p.value}
              variant={preset === p.value ? "secondary" : "ghost"}
              size="sm"
              className="h-7 px-2.5"
              onClick={() => setPreset(p.value)}
            >
              {p.label}
            </Button>
          ))}
        </div>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto p-6">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <p className="py-6 text-center text-sm text-destructive">Couldn’t load this metric.</p>
        ) : (
          <>
            <Card className="flex h-72 flex-col">
              {series && <PanelSummary series={series} metric={meta} />}
              <div className="min-h-0 flex-1 p-2">
                {series && (
                  <PanelChart list={[series]} metrics={[{ metric, chart_type: defaultChartType(meta) }]} />
                )}
              </div>
            </Card>

            {series && <HighsAndLows series={series} />}

            <LedgerDetailTable
              metric={metric}
              unit={series?.unit ?? meta.unit}
              aggregation={series?.aggregation ?? meta.aggregation ?? ""}
              range={range}
            />
          </>
        )}
      </div>
    </div>
  );
}

/** HighsAndLows is the window's highest and lowest bucket values — a plain
 *  `Math.min`/`Math.max` reduce over the Series' own points, which are already
 *  gap-free (a bucket with no data is simply absent, ADR 0014), so this needs no
 *  new aggregation beyond what the chart already fetched. */
function HighsAndLows({ series }: { series: Series }) {
  const { points, unit, aggregation, bucket } = series;
  if (points.length === 0) return null;

  let high = points[0];
  let low = points[0];
  for (const p of points) {
    if (p.value > high.value) high = p;
    if (p.value < low.value) low = p;
  }

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      <Stat label="Highest" value={high.value} date={high.bucket} bucket={bucket} unit={unit} aggregation={aggregation} />
      <Stat label="Lowest" value={low.value} date={low.bucket} bucket={bucket} unit={unit} aggregation={aggregation} />
    </div>
  );
}

function Stat({
  label,
  value,
  date,
  bucket,
  unit,
  aggregation,
}: {
  label: string;
  value: number;
  date: string;
  bucket: Series["bucket"];
  unit: string;
  aggregation: Aggregation | "";
}) {
  return (
    <Card className="px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="flex items-baseline gap-1.5">
        <span className="text-lg font-semibold tabular-nums" title={`${formatExact(value)} ${unit}`.trim()}>
          {formatSummaryValue(value, aggregation)}
        </span>
        {unit && <span className="text-xs text-muted-foreground">{unit}</span>}
      </div>
      <div className="text-xs text-muted-foreground">{formatBucket(date, bucket)}</div>
    </Card>
  );
}

/** PinToggle keeps this Metric in the sidebar, or takes it out (ADR 0025). It is
 *  the only place a Metric gets pinned, because the gesture is "pin this page";
 *  unpinning has a second entry point on the sidebar row itself, which is where
 *  you notice you no longer want it. */
function PinToggle({ metric }: { metric: string }) {
  const pins = usePins();
  const addPin = useAddPin();
  const removePin = useRemovePin();
  const pinned = (pins.data ?? []).some((p) => p.metric === metric);

  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-7"
      aria-pressed={pinned}
      aria-label={pinned ? "Unpin from sidebar" : "Pin to sidebar"}
      title={pinned ? "Unpin from sidebar" : "Pin to sidebar"}
      onClick={() => (pinned ? removePin.mutate(metric) : addPin.mutate(metric))}
    >
      {pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
    </Button>
  );
}
