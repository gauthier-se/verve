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
import { metricLabel } from "@/lib/metrics";
import type { ChartType, PanelMetric, Series } from "@/lib/types";

/** SERIES_COLORS is the fixed categorical order, assigned by position in the
 *  Panel (ADR 0020) — the legend swatches use the same array. */
export const SERIES_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
];

/** Swatch is the small square color key for series i, shared by the legend and
 *  the tooltip so identity reads the same everywhere. */
export function Swatch({ i }: { i: number }) {
  return (
    <span
      className="inline-block size-2 shrink-0 rounded-[2px]"
      style={{ background: SERIES_COLORS[i] ?? SERIES_COLORS[0] }}
    />
  );
}

const BAND = "hsl(var(--chart-2))";
const GRID = "hsl(var(--border))";
const AXIS = "hsl(var(--muted-foreground))";
// The Baseline is one recessed reference line, the same muted/dashed treatment on
// every chart type (ADR 0015) — never colored by sign or metric.
const BASELINE = "hsl(var(--muted-foreground))";
// Diverging-bar sign colors: surplus (≥ 0) warm, deficit (< 0) cool (ADR 0014).
const SURPLUS = "hsl(var(--chart-positive))";
const DEFICIT = "hsl(var(--chart-negative))";

/** ChartDatum is one x-position: per-series values keyed v0…v3 (band0… for the
 *  min/max band), sparse — a Series without data in that bucket has no key (a gap,
 *  ADR 0014). Single-Metric comparison adds the Baseline bucket keyed to the same
 *  ordinal index with its own date for the tooltip (ADR 0015). */
interface ChartDatum {
  bucket: string;
  baselineValue?: number;
  baselineBucket?: string;
  [seriesKey: `v${number}` | `band${number}`]: number | number[] | undefined;
}

/** PanelChart renders a Panel's Series as one combo chart: each Series with its
 *  own mark and color by position, on the Y axis of its unit group — the first
 *  Metric's unit takes the left axis, the other (if any) the right, so every curve
 *  keeps its true scale (ADR 0020). Single-Metric Panels may carry a Baseline
 *  overlay in comparison mode (ADR 0015); the server never sends one for more. */
export function PanelChart({
  list,
  metrics,
  baseline,
}: {
  list: Series[];
  metrics: PanelMetric[];
  baseline?: Series;
}) {
  const data = React.useMemo<ChartDatum[]>(() => mergeSeries(list, baseline), [list, baseline]);

  if (data.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        No data in this range
      </div>
    );
  }

  const leftUnit = list[0]?.unit ?? "";
  const rightUnit = list.find((s) => s.unit !== leftUnit)?.unit;
  const axisOf = (s: Series) => (s.unit === leftUnit ? "left" : "right");

  const axisProps = { stroke: AXIS, fontSize: 11, tickLine: false, axisLine: false } as const;
  const xAxis = (
    <XAxis dataKey="bucket" tickFormatter={formatTick(list[0].bucket)} minTickGap={24} {...axisProps} />
  );
  const grid = <CartesianGrid stroke={GRID} strokeDasharray="3 3" vertical={false} />;
  const tooltip = (
    <Tooltip
      content={<ChartTooltip list={list} bucket={list[0].bucket} />}
      cursor={{ stroke: GRID }}
    />
  );

  return (
    <ResponsiveContainer width="100%" height="100%">
      <ComposedChart data={data} margin={{ top: 8, right: rightUnit ? 0 : 8, bottom: 0, left: 0 }}>
        {grid}
        {xAxis}
        <YAxis
          yAxisId="left"
          width={40}
          domain={axisDomain(list, metrics, "left", axisOf)}
          {...axisProps}
          tickFormatter={formatValue}
        />
        {rightUnit && (
          <YAxis
            yAxisId="right"
            orientation="right"
            width={40}
            domain={axisDomain(list, metrics, "right", axisOf)}
            {...axisProps}
            tickFormatter={formatValue}
          />
        )}
        {tooltip}
        {list.map((s, i) =>
          marks(metrics[i]?.chart_type ?? "line", i, axisOf(s), data, list.length > 1),
        )}
        {baseline && baselineLine}
      </ComposedChart>
    </ResponsiveContainer>
  );
}

/** mergeSeries folds sparse Series into per-bucket rows, keyed by the shared
 *  bucket grid's dates (the server resolves the time axis once, ADR 0020). With a
 *  single Series the rows are its points in order, so the Baseline stays
 *  index-aligned exactly as the server built it (ADR 0015). */
function mergeSeries(list: Series[], baseline?: Series): ChartDatum[] {
  if (list.length === 1) {
    return list[0].points.map((p, i) => {
      const bp = baseline?.points[i];
      const d: ChartDatum = { bucket: p.bucket, v0: p.value };
      if (p.min !== undefined && p.max !== undefined) d.band0 = [p.min, p.max];
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

/** axisDomain spans zero for an axis carrying a diverging bar, whose "balance
 *  around zero" reading is lost if Recharts fits the axis to same-sign data
 *  (ADR 0014). Other axes auto-fit. */
function axisDomain(
  list: Series[],
  metrics: PanelMetric[],
  axis: "left" | "right",
  axisOf: (s: Series) => "left" | "right",
): AxisDomain | undefined {
  const diverging = list.some((s, i) => axisOf(s) === axis && metrics[i]?.chart_type === "diverging_bar");
  return diverging
    ? [(min: number) => Math.min(0, min), (max: number) => Math.max(0, max)]
    : undefined;
}

// baselineLine is the single recessed overlay: a muted dashed line at each
// ordinal position, broken where the Baseline has no data (connectNulls off) so an
// empty baseline window simply draws nothing (ADR 0015).
const baselineLine = (
  <Line
    yAxisId="left"
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

/** marks renders one Series' mark(s) for its chart type at its position color,
 *  on its unit group's axis. On a multi-Metric Panel identity wins over polarity:
 *  a diverging bar keeps its zero line but wears the series color, since sign
 *  colors would collide with the other curves' identities. */
function marks(
  chartType: ChartType,
  i: number,
  yAxisId: "left" | "right",
  data: ChartDatum[],
  multi: boolean,
): React.ReactNode {
  const color = SERIES_COLORS[i] ?? SERIES_COLORS[0];
  const key: `v${number}` = `v${i}`;
  switch (chartType) {
    case "line":
      return (
        <Line key={key} yAxisId={yAxisId} type="monotone" dataKey={key} stroke={color} strokeWidth={2} dot={false} />
      );
    case "area":
      return (
        <Area
          key={key}
          yAxisId={yAxisId}
          type="monotone"
          dataKey={key}
          stroke={color}
          strokeWidth={2}
          fill={color}
          fillOpacity={0.15}
        />
      );
    case "band":
      return (
        <React.Fragment key={key}>
          <Area
            yAxisId={yAxisId}
            type="monotone"
            dataKey={`band${i}`}
            stroke="none"
            fill={multi ? color : BAND}
            fillOpacity={0.18}
          />
          <Line yAxisId={yAxisId} type="monotone" dataKey={key} stroke={color} strokeWidth={2} dot={false} />
        </React.Fragment>
      );
    // diverging_bar is the signed-Metric variant (calorie_balance, ADR 0014):
    // bars grow from a zero baseline. Alone, they are colored by sign — surplus
    // above, deficit below; in a combo the series color carries identity. Gap
    // buckets are already absent from the Series, so they draw no bar.
    case "diverging_bar":
      return (
        <React.Fragment key={key}>
          <ReferenceLine yAxisId={yAxisId} y={0} stroke={AXIS} strokeWidth={1} />
          <Bar yAxisId={yAxisId} dataKey={key} fill={color} radius={[3, 3, 0, 0]} isAnimationActive={false}>
            {!multi &&
              data.map((d) => (
                <Cell key={d.bucket} fill={typeof d[key] === "number" && (d[key] as number) < 0 ? DEFICIT : SURPLUS} />
              ))}
          </Bar>
        </React.Fragment>
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
      return <Bar key={key} yAxisId={yAxisId} dataKey={key} fill={color} radius={[3, 3, 0, 0]} />;
  }
}

/** formatTick labels the X axis by the bucket granularity: a day/week bucket
 *  shows "Mar 4", a month bucket "Mar ’24". */
function formatTick(bucket: Series["bucket"]) {
  return (value: string) => formatBucket(value, bucket);
}

/** formatBucket renders a bucket date for the given granularity, falling back to
 *  the raw string if it can't be parsed. Shared by the axis tick, the tooltip, and
 *  the Panel summary's secondary figure. */
export function formatBucket(value: string, bucket: Series["bucket"]): string {
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
  list: Series[];
  bucket: Series["bucket"];
}

/** ChartTooltip lists every Series' value (with its unit and color swatch) for
 *  the hovered bucket; a Series without data there shows nothing — a gap is never
 *  a zero (ADR 0014). Single-Metric comparison keeps both windows' own real dates
 *  side by side (ADR 0015). */
function ChartTooltip({ active, payload, list, bucket }: TooltipProps) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload;
  const hasBaseline = d.baselineBucket !== undefined;
  const multi = list.length > 1;
  return (
    <div className="rounded-md border bg-popover px-2.5 py-1.5 text-xs shadow-md">
      <div className="font-medium">{formatBucket(d.bucket, bucket)}</div>
      {list.map((s, i) => {
        const value = d[`v${i}`];
        if (typeof value !== "number") return null;
        const band = d[`band${i}`];
        return (
          <div key={s.metric} className="flex items-center gap-1.5 text-muted-foreground">
            {multi && <Swatch i={i} />}
            {multi && <span className="truncate">{metricLabel(s.metric)}</span>}
            <span className="tabular-nums">
              {formatValue(value)} {s.unit}
            </span>
            {Array.isArray(band) && (
              <span className="opacity-70">
                ({formatValue(band[0])}–{formatValue(band[1])})
              </span>
            )}
          </div>
        );
      })}
      {hasBaseline && (
        <div className="mt-1 border-t pt-1">
          <div className="font-medium text-muted-foreground">{formatBucket(d.baselineBucket!, bucket)}</div>
          <div className="text-muted-foreground">
            {d.baselineValue !== undefined ? `${formatValue(d.baselineValue)} ${list[0].unit}` : "no data"}
          </div>
        </div>
      )}
    </div>
  );
}
