import * as React from "react";
import { format, parseISO } from "date-fns";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  LineChart,
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
// Diverging-bar sign colors: surplus (≥ 0) warm, deficit (< 0) cool (ADR 0014).
const SURPLUS = "hsl(var(--chart-positive))";
const DEFICIT = "hsl(var(--chart-negative))";

/** PanelChart renders one Series with the Panel's chosen chart type. Because the
 *  API only ever returns a few hundred bucketed points (ADR 0012), the chart
 *  library's performance is a non-issue and Recharts renders directly. */
export function PanelChart({ series, chartType }: { series: Series; chartType: ChartType }) {
  const data = React.useMemo(
    () =>
      series.points.map((p) => ({
        bucket: p.bucket,
        value: p.value,
        // A range-area band needs the [low, high] pair as a single datum value.
        band: p.min !== undefined && p.max !== undefined ? [p.min, p.max] : undefined,
      })),
    [series.points],
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
  const tooltip = <Tooltip content={<ChartTooltip unit={series.unit} />} cursor={{ stroke: GRID }} />;

  return (
    <ResponsiveContainer width="100%" height="100%">
      {renderChart(chartType, data, { grid, xAxis, yAxis, tooltip })}
    </ResponsiveContainer>
  );
}

type ChartData = { bucket: string; value: number; band?: number[] }[];
type Axes = { grid: React.ReactElement; xAxis: React.ReactElement; yAxis: React.ReactElement; tooltip: React.ReactElement };

function renderChart(chartType: ChartType, data: ChartData, axes: Axes): React.ReactElement {
  const { grid, xAxis, yAxis, tooltip } = axes;
  const margin = { top: 8, right: 8, bottom: 0, left: 0 };

  switch (chartType) {
    case "line":
      return (
        <LineChart data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          <Line type="monotone" dataKey="value" stroke={VALUE} strokeWidth={2} dot={false} />
        </LineChart>
      );
    case "area":
      return (
        <AreaChart data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          <Area type="monotone" dataKey="value" stroke={VALUE} strokeWidth={2} fill={VALUE} fillOpacity={0.15} />
        </AreaChart>
      );
    case "band":
      return (
        <ComposedChart data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          <Area type="monotone" dataKey="band" stroke="none" fill={BAND} fillOpacity={0.18} />
          <Line type="monotone" dataKey="value" stroke={VALUE} strokeWidth={2} dot={false} />
        </ComposedChart>
      );
    // diverging_bar is the signed-Metric variant (calorie_balance, ADR 0014):
    // bars grow from a zero baseline and are colored by sign — surplus above,
    // deficit below. Gap buckets are already absent from the Series, so they
    // simply draw no bar (never a zero bar). A Cell per datum sets the sign color.
    case "diverging_bar":
      return (
        <BarChart data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          <ReferenceLine y={0} stroke={AXIS} strokeWidth={1} />
          <Bar dataKey="value" radius={[3, 3, 0, 0]} isAnimationActive={false}>
            {data.map((d) => (
              <Cell key={d.bucket} fill={d.value < 0 ? DEFICIT : SURPLUS} />
            ))}
          </Bar>
        </BarChart>
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
      return (
        <BarChart data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          <Bar dataKey="value" fill={VALUE} radius={[3, 3, 0, 0]} />
        </BarChart>
      );
  }
}

/** formatTick labels the X axis by the bucket granularity: a day/week bucket
 *  shows "Mar 4", a month bucket "Mar ’24". */
function formatTick(bucket: Series["bucket"]) {
  return (value: string) => {
    try {
      const d = parseISO(value);
      return bucket === "month" ? format(d, "MMM ''yy") : format(d, "MMM d");
    } catch {
      return value;
    }
  };
}

function formatValue(v: number): string {
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)}k`;
  return Number.isInteger(v) ? String(v) : v.toFixed(1);
}

interface TooltipProps {
  active?: boolean;
  payload?: { payload: { bucket: string; value: number; band?: number[] } }[];
  unit: string;
}

function ChartTooltip({ active, payload, unit }: TooltipProps) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload;
  return (
    <div className="rounded-md border bg-popover px-2.5 py-1.5 text-xs shadow-md">
      <div className="font-medium">{d.bucket}</div>
      <div className="text-muted-foreground">
        {formatValue(d.value)} {unit}
        {d.band && (
          <span className="ml-1 opacity-70">
            ({formatValue(d.band[0])}–{formatValue(d.band[1])})
          </span>
        )}
      </div>
    </div>
  );
}
