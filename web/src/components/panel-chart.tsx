import * as React from "react";
import { format, parseISO } from "date-fns";
import {
  Area,
  Bar,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { AxisDomain } from "recharts/types/util/types";
import type { ChartType, Series } from "@/lib/types";

const VALUE = "hsl(var(--chart-1))";
const BAND = "hsl(var(--chart-2))";
const GRID = "hsl(var(--border))";
const AXIS = "hsl(var(--muted-foreground))";
// The Baseline is one recessed reference line, the same muted/dashed treatment on
// every chart type (ADR 0015) — never colored by sign or metric.
const BASELINE = "hsl(var(--muted-foreground))";
// Diverging-bar sign colors: surplus (≥ 0) warm, deficit (< 0) cool (ADR 0014).
const SURPLUS = "hsl(var(--chart-positive))";
const DEFICIT = "hsl(var(--chart-negative))";

/** ChartDatum is one x-position (ordinal within the period): the current bucket's
 *  value and band, plus the Baseline bucket keyed to that index with its own date
 *  for the tooltip (ADR 0015). */
interface ChartDatum {
  bucket: string;
  value: number;
  band?: number[];
  baselineValue?: number;
  baselineBucket?: string;
}

/** PanelChart renders one Series with the Panel's chart type, optionally overlaid
 *  with a Baseline in comparison mode (a few hundred points, ADR 0012). */
export function PanelChart({
  series,
  baseline,
  chartType,
}: {
  series: Series;
  baseline?: Series;
  chartType: ChartType;
}) {
  const data = React.useMemo<ChartDatum[]>(
    () =>
      series.points.map((p, i) => {
        // The Baseline is equal-length and index-aligned to the current series
        // (server-side, ADR 0015): baseline.points[i] is the ordinal counterpart
        // of series.points[i]. A dated gap carries no value, breaking the line.
        const bp = baseline?.points[i];
        return {
          bucket: p.bucket,
          value: p.value,
          // A range-area band needs the [low, high] pair as a single datum value.
          band: p.min !== undefined && p.max !== undefined ? [p.min, p.max] : undefined,
          baselineValue: bp && !bp.gap ? bp.value : undefined,
          baselineBucket: bp?.bucket,
        };
      }),
    [series.points, baseline],
  );

  if (data.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        No data in this range
      </div>
    );
  }

  const axisProps = { stroke: AXIS, fontSize: 11, tickLine: false, axisLine: false } as const;
  const xAxis = (
    <XAxis dataKey="bucket" tickFormatter={formatTick(series.bucket)} minTickGap={24} {...axisProps} />
  );
  // A diverging bar reads against a zero baseline, so the Y domain must span zero
  // even when every value shares a sign (ADR 0014); otherwise Recharts fits the
  // axis to the data and the "balance around zero" is lost.
  const yDomain: AxisDomain | undefined =
    chartType === "diverging_bar"
      ? [(min: number) => Math.min(0, min), (max: number) => Math.max(0, max)]
      : undefined;
  const yAxis = <YAxis width={40} domain={yDomain} {...axisProps} tickFormatter={formatValue} />;
  const grid = <CartesianGrid stroke={GRID} strokeDasharray="3 3" vertical={false} />;
  const tooltip = (
    <Tooltip content={<ChartTooltip unit={series.unit} bucket={series.bucket} />} cursor={{ stroke: GRID }} />
  );

  return (
    <ResponsiveContainer width="100%" height="100%">
      <ComposedChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        {grid}
        {xAxis}
        {yAxis}
        {tooltip}
        {marks(chartType, data)}
        {baseline && baselineLine}
      </ComposedChart>
    </ResponsiveContainer>
  );
}

// baselineLine is the single recessed overlay: a muted dashed line at each
// ordinal position, broken where the Baseline has no data (connectNulls off) so an
// empty baseline window simply draws nothing (ADR 0015).
const baselineLine = (
  <Line
    type="monotone"
    dataKey="baselineValue"
    stroke={BASELINE}
    strokeWidth={1.5}
    strokeDasharray="4 3"
    strokeOpacity={0.7}
    dot={false}
    connectNulls={false}
    isAnimationActive={false}
  />
);

/** marks renders the Panel's native mark(s) for its chart type, to be composed
 *  under the optional Baseline overlay. All types share one ComposedChart so the
 *  baseline line lays over any of them identically. */
function marks(chartType: ChartType, data: ChartDatum[]): React.ReactNode {
  switch (chartType) {
    case "line":
      return <Line type="monotone" dataKey="value" stroke={VALUE} strokeWidth={2} dot={false} />;
    case "area":
      return (
        <Area type="monotone" dataKey="value" stroke={VALUE} strokeWidth={2} fill={VALUE} fillOpacity={0.15} />
      );
    case "band":
      return (
        <>
          <Area type="monotone" dataKey="band" stroke="none" fill={BAND} fillOpacity={0.18} />
          <Line type="monotone" dataKey="value" stroke={VALUE} strokeWidth={2} dot={false} />
        </>
      );
    // diverging_bar is the signed-Metric variant (calorie_balance, ADR 0014):
    // bars grow from a zero baseline and are colored by sign — surplus above,
    // deficit below. Gap buckets are already absent from the Series, so they
    // simply draw no bar (never a zero bar). A Cell per datum sets the sign color.
    case "diverging_bar":
      return (
        <>
          <ReferenceLine y={0} stroke={AXIS} strokeWidth={1} />
          <Bar dataKey="value" radius={[3, 3, 0, 0]} isAnimationActive={false}>
            {data.map((d) => (
              <Cell key={d.bucket} fill={d.value < 0 ? DEFICIT : SURPLUS} />
            ))}
          </Bar>
        </>
      );
    // stacked_bar is the sleep (duration_by_state) variant. That aggregation is
    // not served yet — the query engine defers it and no Catalog Metric uses it
    // (internal/query/query.go) — so this Series never carries per-state values
    // to stack. Until it does, the branch renders the single value as a plain
    // bar rather than pretending to stack; the true stacked rendering lands with
    // the sleep slice.
    case "stacked_bar":
    case "bar":
    default:
      return <Bar dataKey="value" fill={VALUE} radius={[3, 3, 0, 0]} />;
  }
}

/** formatTick labels the X axis by the bucket granularity: a day/week bucket
 *  shows "Mar 4", a month bucket "Mar ’24". */
function formatTick(bucket: Series["bucket"]) {
  return (value: string) => formatBucket(value, bucket);
}

/** formatBucket renders a bucket date for the given granularity, falling back to
 *  the raw string if it can't be parsed. Shared by the axis tick and the tooltip. */
function formatBucket(value: string, bucket: Series["bucket"]): string {
  try {
    const d = parseISO(value);
    return bucket === "month" ? format(d, "MMM ''yy") : format(d, "MMM d");
  } catch {
    return value;
  }
}

function formatValue(v: number): string {
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)}k`;
  return Number.isInteger(v) ? String(v) : v.toFixed(1);
}

interface TooltipProps {
  active?: boolean;
  payload?: { payload: ChartDatum }[];
  unit: string;
  bucket: Series["bucket"];
}

function ChartTooltip({ active, payload, unit, bucket }: TooltipProps) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload;
  // In comparison mode the tooltip shows both buckets' own real dates side by side
  // (ADR 0015): the current above, the Baseline below, each with its value.
  const hasBaseline = d.baselineBucket !== undefined;
  return (
    <div className="rounded-md border bg-popover px-2.5 py-1.5 text-xs shadow-md">
      <div className="font-medium">{formatBucket(d.bucket, bucket)}</div>
      <div className="text-muted-foreground">
        {formatValue(d.value)} {unit}
        {d.band && (
          <span className="ml-1 opacity-70">
            ({formatValue(d.band[0])}–{formatValue(d.band[1])})
          </span>
        )}
      </div>
      {hasBaseline && (
        <div className="mt-1 border-t pt-1">
          <div className="font-medium text-muted-foreground">{formatBucket(d.baselineBucket!, bucket)}</div>
          <div className="text-muted-foreground">
            {d.baselineValue !== undefined ? `${formatValue(d.baselineValue)} ${unit}` : "no data"}
          </div>
        </div>
      )}
    </div>
  );
}
