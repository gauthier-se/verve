import * as React from "react";
import { format, parseISO } from "date-fns";
import { StickyNote } from "lucide-react";
import {
  Area,
  Bar,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { CategoricalChartState } from "recharts/types/chart/types";
import type { AxisDomain } from "recharts/types/util/types";
import { projectAnnotations, type AnnotationOverlay } from "@/lib/annotations";
import { AXIS, GRID, NEGATIVE, POSITIVE, SERIES_COLORS, seriesColor } from "@/lib/chart";
import { mergeSeries, stageKey, type ChartDatum } from "@/lib/chart-data";
import { formatAxisValue, formatDuration } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { drawsGoalLine } from "@/lib/goals";
import { usualLine } from "@/lib/usual";
import { useActivityMap } from "@/hooks/use-catalog";
import { buildSegments, type Segment } from "@/lib/breakdown";
import { Key } from "./ui/figure";
import type { Annotation, ChartType, PanelMetric, Series } from "@/lib/types";

export { SERIES_COLORS };

/** Swatch is the small square color key for series i, shared by the legend and
 *  the tooltip so identity reads the same everywhere. It takes the Panel's ramp
 *  offset because a key that does not name the colour actually drawn is worse than
 *  no key at all. */
export function Swatch({ i, offset = 0 }: { i: number; offset?: number }) {
  return <Key color={seriesColor(i, offset)} />;
}
// The Baseline is one recessed reference line, the same muted/dashed treatment on
// every chart type (ADR 0015) — never colored by sign or metric.
const BASELINE = AXIS;
// An Annotation is context, not a series: it wears the Baseline's recessed tone,
// never a chart colour, and it draws behind the marks (ADR 0030).
const ANNOTATION = AXIS;
// The Usual is the owner's own past, not a Metric: it takes no slot of the ramp
// (ADR 0036, ADR 0042) and wears the recessed tone of the other references.
const USUAL = AXIS;
// Diverging-bar sign colors: surplus (≥ 0) warm, deficit (< 0) cool (ADR 0014).
const SURPLUS = POSITIVE;
const DEFICIT = NEGATIVE;

/** PanelChart renders a Panel's Series as one combo chart: each Series with its
 *  own mark and color by position, on the Y axis of its unit group — the first
 *  Metric's unit takes the left axis, the other (if any) the right, so every curve
 *  keeps its true scale (ADR 0020). Single-Metric Panels may carry a Baseline
 *  overlay in comparison mode (ADR 0015); the server never sends one for more. */
export function PanelChart({
  list,
  metrics,
  baseline,
  annotations,
  onHoverBucket,
  onSelectBucket,
  colorOffset = 0,
}: {
  list: Series[];
  metrics: PanelMetric[];
  baseline?: Series;
  annotations?: Annotation[];
  /** colorOffset is where this Panel starts reading the categorical ramp, so a grid
   *  of single-Metric Panels cycles through the four instead of drawing every card in
   *  chart-1. Zero for a chart that stands alone on its own page. */
  colorOffset?: number;
  /** onHoverBucket reports the category under the cursor, so an "Add a note" opened
   *  afterwards can prefill the day the person was actually looking at. */
  onHoverBucket?: (bucket: string | null) => void;
  /** onSelectBucket reports the category that was clicked, the symmetric event to
   *  the hover above. Present only when that bucket has somewhere to go, which is
   *  the caller's decision and not this chart's: it knows nothing about routing and
   *  nothing about which Metric it is drawing (ADR 0043). */
  onSelectBucket?: (bucket: string) => void;
}) {
  const data = React.useMemo<ChartDatum[]>(() => mergeSeries(list, baseline), [list, baseline]);
  // The notes to draw, placed on the categories this chart actually has (ADR 0030).
  const overlay = React.useMemo(
    () => projectAnnotations(annotations, data.map((d) => d.bucket)),
    [annotations, data],
  );
  // The segments to stack, empty for every Panel that is not a lone Metric with a
  // breakdown: a Night's Stages, or a bucket's Activities (ADR 0027, ADR 0040).
  // They are resolved once, name and colour together, so the bars, the tooltip and
  // the legend cannot disagree about which swatch is which.
  const activities = useActivityMap();
  const segments = React.useMemo(
    // A decomposition owns the colour ramp only when it owns the Panel, so a combo
    // stacks nothing (ADR 0020).
    () => buildSegments(list.length === 1 ? list[0] : undefined, activities),
    [list, activities],
  );

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

  // 10px mono ticks: an axis label is a coordinate, not prose, and it has to stay
  // out of the way of the curve it is labelling.
  const axisProps = { stroke: AXIS, fontSize: 10, tickLine: false, axisLine: false } as const;
  // An axis carrying minutes is labelled as a duration: "7h 12m", not "432". The
  // unit decides and not the Metric, because two Metrics now read in minutes and a
  // third breaks down into kilometres.
  // One axis is one unit, so its ticks are read in that unit's display scale.
  const tickFormatter = (axis: "left" | "right") => {
    const unit = axis === "left" ? leftUnit : (rightUnit ?? "");
    return unit === "min" ? formatDuration : (v: number) => formatAxisValue(v, unit);
  };
  const xAxis = (
    <XAxis dataKey="bucket" tickFormatter={formatTick(list[0].bucket)} minTickGap={24} {...axisProps} />
  );
  const grid = <CartesianGrid stroke={GRID} strokeDasharray="3 3" vertical={false} />;
  const tooltip = (
    <Tooltip
      content={
        <ChartTooltip
          list={list}
          bucket={list[0].bucket}
          segments={segments}
          unit={list[0].unit}
          totalLabel={list[0].metric === "sleep" ? "Asleep" : "Total"}
          notes={overlay.byBucket}
          offset={colorOffset}
        />
      }
      cursor={{ stroke: GRID }}
    />
  );

  return (
    <ResponsiveContainer width="100%" height="100%">
      <ComposedChart
        data={data}
        // Recharts writes `cursor: default` inline on its own wrapper, and its
        // `style` prop is spread after it: a class would lose, this wins. The
        // affordance has to be there before the click, not after it.
        style={onSelectBucket ? { cursor: "pointer" } : undefined}
        margin={{ top: 8, right: rightUnit ? 0 : 8, bottom: 0, left: 0 }}
        onMouseMove={(s: CategoricalChartState) =>
          onHoverBucket?.(typeof s.activeLabel === "string" ? s.activeLabel : null)
        }
        onClick={(s: CategoricalChartState) => {
          if (typeof s.activeLabel === "string") onSelectBucket?.(s.activeLabel);
        }}
      >
        {grid}
        {xAxis}
        <YAxis
          yAxisId="left"
          width={40}
          domain={axisDomain(list, metrics, "left", axisOf)}
          {...axisProps}
          tickFormatter={tickFormatter("left")}
        />
        {rightUnit && (
          <YAxis
            yAxisId="right"
            orientation="right"
            width={40}
            domain={axisDomain(list, metrics, "right", axisOf)}
            {...axisProps}
            tickFormatter={tickFormatter("right")}
          />
        )}
        {tooltip}
        {annotationOverlay(overlay)}
        {list.length === 1 && usualBand(axisOf(list[0]))}
        {list.map((s, i) =>
          marks(metrics[i]?.chart_type ?? "line", i, axisOf(s), data, list.length > 1, segments, colorOffset),
        )}
        {list.map((s, i) => drawsGoalLine(s) && goalLine(i, axisOf(s), colorOffset))}
        {baseline && baselineLine}
      </ComposedChart>
    </ResponsiveContainer>
  );
}

/** axisDomain spans zero for an axis carrying a diverging bar, whose "balance
 *  around zero" reading is lost if Recharts fits the axis to same-sign data
 *  (ADR 0014). Other axes auto-fit, which is also what keeps a Goal in view: its
 *  line is a series on its Metric's axis, so the fit already reaches it however far
 *  it sits from the data (ADR 0044). */
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

/** goalLine draws a Series' Goal as a dashed step in the Series' own colour, on its
 *  own axis (ADR 0044). It is the only thing a Goal changes on a chart: no point is
 *  recoloured, nothing either side of it is shaded, because the owner declared a
 *  direction, not a wish to be graded. It steps where the Goal changed and breaks
 *  where none was in force, and it is a plateau, so it cannot be read as the
 *  Baseline, which is a recessed curve. */
function goalLine(i: number, yAxisId: "left" | "right", offset: number): React.ReactNode {
  return (
    <Line
      key={`goal${i}`}
      yAxisId={yAxisId}
      type="stepAfter"
      dataKey={`goal${i}`}
      stroke={seriesColor(i, offset)}
      strokeWidth={1.25}
      strokeDasharray="6 4"
      dot={false}
      activeDot={false}
      connectNulls={false}
      isAnimationActive={false}
    />
  );
}

/** usualBand draws a lone Metric's Usual, the p25 to p75 of its own past (ADR 0046),
 *  behind every other mark, including the min/max fill of the band chart type: that
 *  one is the spread inside a bucket, this the spread across the buckets before it.
 *  A centred step, because each bucket has its own and a curve between them would
 *  claim values nobody computed; and broken where the server sent none (ADR 0032). */
function usualBand(yAxisId: "left" | "right"): React.ReactNode {
  return (
    <Area
      key="usual0"
      yAxisId={yAxisId}
      type="step"
      dataKey="usual0"
      stroke="none"
      fill={USUAL}
      fillOpacity={0.14}
      activeDot={false}
      connectNulls={false}
      isAnimationActive={false}
    />
  );
}

/** annotationOverlay draws the Account's notes behind the marks: a band for a span
 *  covering more than one bucket, a thin dashed rule for everything else, both in
 *  the Baseline's recessed tone so the chart stays about the data. Several notes in
 *  one bucket share one rule carrying a count; their labels live in the tooltip,
 *  which is the one hover target this chart has.
 *
 *  Every x here is a category the server named and the chart drew. Nothing is
 *  computed from a date: Recharts matches x by equality, so a mark derived from a
 *  second boundary rule would silently draw nothing at all. */
function annotationOverlay(overlay: AnnotationOverlay): React.ReactNode {
  if (overlay.markers.length === 0) return null;
  return (
    <>
      {overlay.bands.map((b) => (
        <ReferenceArea
          key={`band-${b.id}`}
          yAxisId="left"
          x1={b.from}
          x2={b.to}
          fill={ANNOTATION}
          fillOpacity={0.08}
          strokeOpacity={0}
          isFront={false}
        />
      ))}
      {overlay.markers.map((m) => (
        <ReferenceLine
          key={`mark-${m.bucket}`}
          yAxisId="left"
          x={m.bucket}
          stroke={ANNOTATION}
          strokeWidth={1}
          strokeDasharray="2 3"
          strokeOpacity={0.6}
          isFront={false}
          label={
            m.count > 1
              ? { value: String(m.count), position: "top", fill: ANNOTATION, fontSize: 10 }
              : undefined
          }
        />
      ))}
    </>
  );
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
  segments: Segment[],
  offset: number,
): React.ReactNode {
  const color = seriesColor(i, offset);
  // The min/max band of a lone Series takes the next slot of the ramp rather than the
  // Series' own colour, so the spread reads as a second thing and not as a washed-out
  // copy of the line. In a combo it takes the identity colour instead: a neighbouring
  // slot there is already another Series.
  const band = multi ? color : seriesColor(i + 1, offset);
  const key: `v${number}` = `v${i}`;
  const trendKey: `trend${number}` = `trend${i}`;
  // A sampled Metric's smoothed line, when the server sent one. It is the same hue
  // rather than a second colour: a different colour is the grammar for a different
  // Metric (ADR 0020), and these are one Metric read two ways.
  const hasTrend = data.some((d) => d[trendKey] !== undefined);
  const trendLine = hasTrend ? (
    <Line
      key={trendKey}
      yAxisId={yAxisId}
      type="monotone"
      dataKey={trendKey}
      stroke={color}
      strokeWidth={2}
      dot={false}
      // The whole point of ADR 0032 at this layer: Recharts bridges nulls by default,
      // which would draw the smoothed line straight across months nobody weighed.
      connectNulls={false}
      isAnimationActive={false}
    />
  ) : null;
  switch (chartType) {
    case "line":
      // With a trend drawn, the readings are demoted to the scatter they are: thin and
      // faint, still present as the evidence, no longer competing to be read as the
      // signal.
      return trendLine ? (
        <React.Fragment key={key}>
          <Line
            yAxisId={yAxisId}
            type="monotone"
            dataKey={key}
            stroke={color}
            strokeWidth={1}
            strokeOpacity={0.35}
            dot={false}
          />
          {trendLine}
        </React.Fragment>
      ) : (
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
            fill={band}
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
    // stacked_bar is the sleep (duration_by_state) variant: one segment per Stage,
    // each on its own fixed colour (ADR 0027). It stacks only when sleep is the
    // Panel's sole Metric — a decomposition can own the colour ramp only when it
    // owns the Panel, or ADR 0020's "colour by position" would stop being true the
    // moment a second Metric joined. In a combo it is one plain bar of time asleep
    // in its own position colour, which is what the `bar` branch below already does.
    case "stacked_bar":
      if (multi || segments.length === 0) break;
      return (
        <React.Fragment key={key}>
          {segments.map((segment, s) => (
            <Bar
              key={segment.key}
              yAxisId={yAxisId}
              dataKey={stageKey(segment.key)}
              stackId="stages"
              fill={segment.color}
              radius={s === segments.length - 1 ? [3, 3, 0, 0] : undefined}
              isAnimationActive={false}
            />
          ))}
        </React.Fragment>
      );
  }
  return <Bar key={key} yAxisId={yAxisId} dataKey={key} fill={color} radius={[3, 3, 0, 0]} />;
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

interface TooltipProps {
  active?: boolean;
  payload?: { payload: ChartDatum }[];
  list: Series[];
  bucket: Series["bucket"];
  segments: Segment[];
  /** unit is the stacked Metric's unit, which is what decides whether a segment
   *  reads as a duration or as a figure. */
  unit?: string;
  /** totalLabel names the row under the segments: a Night's is "Asleep", because
   *  the figure deliberately excludes the awake segment above it (ADR 0027). */
  totalLabel?: string;
  /** notes are the Annotations covering each drawn bucket (ADR 0030). They belong
   *  in this tooltip rather than beside the marks: one hover target, not two. */
  notes?: Map<string, Annotation[]>;
  /** offset is the Panel's start in the ramp, so the tooltip's swatches match the
   *  marks they are naming. */
  offset?: number;
}

/** SegmentRows lists a stacked bucket's parts with their values, then the bucket's
 *  own figure. A stacked bar is the one chart whose segments cannot be read by eye,
 *  so the hover has to name them; and the figure is listed separately because for
 *  sleep it is not the height of the bar, `awake` being stacked and never counted
 *  (ADR 0027). For a volume the two agree and the row reads as the total it is. */
function SegmentRows({
  d,
  total,
  segments,
  unit,
  totalLabel,
}: {
  d: ChartDatum;
  total: number | undefined;
  segments: Segment[];
  unit: string;
  totalLabel: string;
}) {
  // Minutes read as a duration, everything else as a figure with its unit: what
  // decides is the unit, not the Metric, now that two of them stack.
  const show = (v: number) => (unit === "min" ? formatDuration(v) : `${formatAxisValue(v, unit)} ${unit}`.trim());
  return (
    <>
      {segments.map((segment) => {
        const value = d[stageKey(segment.key)];
        if (typeof value !== "number") return null;
        return (
          <div key={segment.key} className="flex items-center gap-1.5 text-muted-foreground">
            <span
              className="inline-block size-2 shrink-0 rounded-[2px]"
              style={{ background: segment.color }}
            />
            <span className="truncate">{segment.label}</span>
            <span className="ml-auto tabular-nums">{show(value)}</span>
          </div>
        );
      })}
      {typeof total === "number" && (
        <div className="mt-1 flex items-center gap-3 border-t pt-1">
          <span>{totalLabel}</span>
          <span className="ml-auto tabular-nums">{show(total)}</span>
        </div>
      )}
    </>
  );
}

/** ChartTooltip lists every Series' value (with its unit and color swatch) for
 *  the hovered bucket; a Series without data there shows nothing — a gap is never
 *  a zero (ADR 0014). Single-Metric comparison keeps both windows' own real dates
 *  side by side (ADR 0015). */
function ChartTooltip({
  active,
  payload,
  list,
  bucket,
  segments,
  unit = "",
  totalLabel = "Total",
  notes,
  offset = 0,
}: TooltipProps) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload;
  const covering = notes?.get(d.bucket) ?? [];
  const hasBaseline = d.baselineBucket !== undefined;
  const multi = list.length > 1;
  const stacked = segments.length > 0;
  return (
    <div className="rounded-md border bg-popover px-2.5 py-1.5 text-xs shadow-md">
      <div className="font-medium">{formatBucket(d.bucket, bucket)}</div>
      {stacked && (
        <SegmentRows
          d={d}
          total={d.v0 as number | undefined}
          segments={segments}
          unit={unit}
          totalLabel={totalLabel}
        />
      )}
      {!stacked &&
        list.map((s, i) => {
          const value = d[`v${i}`];
          if (typeof value !== "number") return null;
          const band = d[`band${i}`];
          return (
            <div key={s.metric} className="flex items-center gap-1.5 text-muted-foreground">
              {multi && <Swatch i={i} offset={offset} />}
              {multi && <span className="truncate">{metricLabel(s.metric)}</span>}
              <span className="tabular-nums">
                {formatAxisValue(value, s.unit)} {s.unit}
              </span>
              {Array.isArray(band) && (
                <span className="opacity-70">
                  ({formatAxisValue(band[0], s.unit)}–{formatAxisValue(band[1], s.unit)})
                </span>
              )}
            </div>
          );
        })}
      {!multi && d.usual && typeof d.v0 === "number" && (
        <div className="max-w-56 text-muted-foreground opacity-80">
          {stacked || unit === "min"
            ? usualLine(d.v0, d.usual, bucket, formatDuration)
            : usualLine(d.v0, d.usual, bucket, (v) => formatAxisValue(v, list[0].unit), list[0].unit)}
        </div>
      )}
      {covering.length > 0 && (
        <div className="mt-1 max-w-56 space-y-0.5 border-t pt-1">
          {covering.map((a) => (
            <div key={a.id} className="flex items-start gap-1.5 text-muted-foreground">
              <StickyNote className="mt-px size-3 shrink-0 opacity-70" aria-hidden />
              <span className="truncate">{a.label}</span>
            </div>
          ))}
        </div>
      )}
      {hasBaseline && (
        <div className="mt-1 border-t pt-1">
          <div className="font-medium text-muted-foreground">{formatBucket(d.baselineBucket!, bucket)}</div>
          <div className="text-muted-foreground">
            {d.baselineValue === undefined
              ? "no data"
              : stacked
                ? formatDuration(d.baselineValue)
                : `${formatAxisValue(d.baselineValue, list[0].unit)} ${list[0].unit}`}
          </div>
        </div>
      )}
    </div>
  );
}
