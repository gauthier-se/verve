import * as React from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Pin, PinOff, StickyNote } from "lucide-react";
import { useMetricMap } from "@/hooks/use-catalog";
import { usePins, useAddPin, useRemovePin } from "@/hooks/use-pins";
import { useAllAnnotations, useAnnotations } from "@/hooks/use-annotations";
import { useSeries } from "@/hooks/use-series";
import { useTimeAxis } from "@/hooks/use-time-axis";
import { SERIES_COLORS } from "@/lib/chart";
import { defaultChartType, metricLabel } from "@/lib/metrics";
import { formatDay, formatDayRange, formatDuration, formatExact, formatSummaryValue } from "@/lib/format";
import { RANGE_PRESETS, type RangeTokens } from "@/lib/time-range";
import { cn } from "@/lib/utils";
import type { Aggregation, Annotation, Metric, Series, TimeAxis } from "@/lib/types";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { CenteredSpinner } from "./spinner";
import { Chip, Dot, Eyebrow, Figure, ScreenTitle, Unit } from "./ui/figure";
import { FormulaHint } from "./formula-hint";
import { LedgerDetailTable } from "./ledger-detail-table";
import { MetricIcon } from "./metric-icon";
import { AnnotationDialog } from "./annotation-dialog";
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

  // A Metric page has no persisted time axis (ADR 0025), so its notes toggle is
  // local state: there is nowhere to store one, and it must not grow a store for it.
  const [showNotes, setShowNotes] = React.useState(true);
  const [hovered, setHovered] = React.useState<string | null>(null);
  const [noteOpen, setNoteOpen] = React.useState(false);
  const query = useSeries({ metrics: [metric], range, bucket: null });
  const notes = useAnnotations({ range, bucket: null, enabled: showNotes });
  const axis = useTimeAxis({ range });
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

  const shownNotes = showNotes ? notes.data : undefined;

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3.5">
        <div className="flex min-w-0 items-center gap-2.5">
          <Button asChild variant="ghost" size="icon" className="size-7 text-muted-foreground" aria-label="Back to Data">
            <Link to="/data">
              <ArrowLeft className="size-4" />
            </Link>
          </Button>
          <MetricIcon slug={metric} className="size-4 shrink-0" />
          <ScreenTitle className="truncate">{metricLabel(metric)}</ScreenTitle>
          <Chip>{metricRule(meta)}</Chip>
          {meta.formula && <FormulaHint formula={meta.formula} />}
          <PinToggle metric={metric} />
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" className="h-7 gap-1.5 px-2.5 text-xs" onClick={() => setNoteOpen(true)}>
            <StickyNote className="size-3.5" /> Add a note
          </Button>
          <NotesToggle on={showNotes} onToggle={() => setShowNotes((v) => !v)} />
          <RangePresets value={preset} onChange={setPreset} />
        </div>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto p-6">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <p className="py-6 text-center text-sm text-destructive">Couldn’t load this metric.</p>
        ) : (
          <>
            <Card className="flex flex-col">
              {/* The hero: the largest figure in the app, and under it the only
                  chart on the page tall enough to read a year in. */}
              {series && <PanelSummary series={series} metric={meta} size="hero" />}
              <div className="h-[15.5rem] px-3 pb-1 pt-3">
                {series && (
                  <PanelChart
                    list={[series]}
                    metrics={[{ metric, chart_type: defaultChartType(meta) }]}
                    annotations={shownNotes}
                    onHoverBucket={setHovered}
                  />
                )}
              </div>
              <AxisMarks axis={axis.data} />
              {shownNotes && shownNotes.length > 0 && <AnnotationStrip notes={shownNotes} />}
            </Card>

            {series && <WindowStats series={series} axis={axis.data} metric={meta} />}

            <LedgerDetailTable
              metric={metric}
              unit={series?.unit ?? meta.unit}
              aggregation={series?.aggregation ?? meta.aggregation ?? ""}
              range={range}
            />
          </>
        )}
      </div>

      <AnnotationDialog open={noteOpen} onOpenChange={setNoteOpen} defaultDay={hovered} />
    </div>
  );
}

/** metricRule is the chip beside the title: the unit and the rule the Metric is
 *  folded by. Two words that decide how every number on the page must be read —
 *  "bpm · average" is not the same claim as "kcal · sum". */
function metricRule(metric: Metric): string {
  if (metric.nature === "derived") {
    return [metric.unit, "derived"].filter(Boolean).join(" · ");
  }
  return [metric.unit, metric.aggregation].filter(Boolean).join(" · ");
}

/** RangePresets is this page's own time control. It is deliberately not persisted
 *  (ADR 0025): a Metric page is a shortcut, and storing an axis on it would be the
 *  first step toward a one-Panel Dashboard. */
function RangePresets({ value, onChange }: { value: Preset; onChange: (p: Preset) => void }) {
  return (
    <div className="flex items-center gap-0.5 rounded-md border p-0.5">
      {RANGE_PRESETS.map((p) => (
        <button
          key={p.value}
          type="button"
          onClick={() => onChange(p.value)}
          className={cn(
            "rounded px-2 py-1 font-mono text-2xs tabular-nums transition-colors",
            value === p.value
              ? "bg-secondary font-medium text-secondary-foreground"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}

/** AxisMarks names the ends of the window under the chart, in mono.
 *
 *  Recharts labels its own ticks, but it labels them by bucket and drops what does
 *  not fit; the window's real bounds are a different fact, and they come from the
 *  server (ADR 0012) rather than from the first and last point that happened to
 *  survive the tick spacing. */
function AxisMarks({ axis }: { axis?: TimeAxis }) {
  if (!axis) return null;
  return (
    <div className="flex justify-between px-4 pb-3.5 font-mono text-3xs tabular-nums text-muted-foreground/70">
      <span>{formatDay(axis.range.from)}</span>
      <span>{formatDay(axis.range.last)}</span>
    </div>
  );
}

/** AnnotationStrip lists the notes drawn on the chart above it. The chart shows
 *  *where* they fall; only this strip can show what they say, because a label at
 *  bucket width is illegible and a tooltip has to be hunted for (ADR 0030). */
function AnnotationStrip({ notes }: { notes: Annotation[] }) {
  return (
    <div className="flex flex-wrap gap-2 border-t px-4 py-3">
      {notes.map((note) => (
        <span
          key={note.id}
          className="flex items-center gap-2 rounded border px-2 py-1 text-2xs text-muted-foreground"
          title={note.body ?? undefined}
        >
          <Dot color={SERIES_COLORS[0]} />
          <span className="font-mono tabular-nums opacity-70">
            {note.ends_on ? formatDayRange(note.starts_on, note.ends_on) : formatDay(note.starts_on)}
          </span>
          <span className="text-foreground">{note.label}</span>
        </span>
      ))}
    </div>
  );
}

/** WindowStats is the four-figure strip under the chart: the window's extremes, how
 *  much evidence it rests on, and how much of it was actually recorded.
 *
 *  Highest and lowest are a plain reduce over the Series' own points — already
 *  gap-free, so nothing is re-aggregated (ADR 0012). Readings is the server's own
 *  count of the rows behind the window. Coverage is the pair that makes the rest
 *  honest: fourteen buckets out of ninety is a different claim from ninety out of
 *  ninety, and no figure above it says so. */
function WindowStats({ series, axis, metric }: { series: Series; axis?: TimeAxis; metric: Metric }) {
  const { points, unit, aggregation, bucket, summary } = series;
  if (points.length === 0) return null;

  let high = points[0];
  let low = points[0];
  for (const p of points) {
    if (p.value > high.value) high = p;
    if (p.value < low.value) low = p;
  }

  // The window's bucket count comes from its own day span and grain — both
  // server-resolved — so "of N" is never a guess made from the points that arrived.
  const total = axis ? bucketsInWindow(axis) : undefined;
  const readings = summary?.count ?? 0;
  const derived = metric.nature === "derived";

  return (
    <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(10rem,1fr))]">
      <Stat label="Highest" figure={statValue(high.value, aggregation)} unit={statUnit(unit, aggregation)}>
        {formatBucket(high.bucket, bucket)}
      </Stat>
      <Stat label="Lowest" figure={statValue(low.value, aggregation)} unit={statUnit(unit, aggregation)}>
        {formatBucket(low.bucket, bucket)}
      </Stat>
      {/* A derived Metric has no reading count of its own: each operand has one, and
          a combined figure would name no row set at all (ADR 0014). */}
      <Stat
        label={aggregation === "duration_by_state" ? "Nights" : "Readings"}
        figure={derived ? "—" : formatExact(aggregation === "duration_by_state" ? (series.nights ?? 0) : readings)}
      >
        {derived ? "computed from its operands" : `from ${series.source || "one source"}`}
      </Stat>
      <Stat label="Coverage" figure={`${points.length}`} unit={total ? <Unit divisor={` of ${total}`} /> : undefined}>
        {bucket === "day" ? "days with data" : `${bucket}s with data`}
      </Stat>
    </div>
  );
}

/** bucketsInWindow is how many buckets of the resolved grain fit the resolved
 *  window. Both numbers are the server's; only the division happens here. */
function bucketsInWindow(axis: TimeAxis): number {
  const days = axis.range.days;
  if (axis.bucket === "day") return days;
  if (axis.bucket === "week") return Math.ceil(days / 7);
  return Math.ceil(days / 30.44);
}

function statValue(value: number, aggregation: Aggregation | ""): string {
  return aggregation === "duration_by_state" ? formatDuration(value) : formatSummaryValue(value, aggregation);
}

function statUnit(unit: string, aggregation: Aggregation | ""): React.ReactNode {
  // A duration carries its unit inside its own text ("7h 12m"), so repeating it
  // would read as "7h 12m min" (ADR 0027).
  if (!unit || aggregation === "duration_by_state") return undefined;
  return <Unit>{unit}</Unit>;
}

function Stat({
  label,
  figure,
  unit,
  children,
}: {
  label: string;
  figure: string;
  unit?: React.ReactNode;
  children?: React.ReactNode;
}) {
  return (
    <Card className="px-3.5 py-3">
      <Eyebrow>{label}</Eyebrow>
      <div className="flex items-baseline gap-1.5 pt-1">
        <Figure size="stat">{figure}</Figure>
        {unit}
      </div>
      <div className="truncate pt-0.5 font-mono text-3xs tabular-nums text-muted-foreground/70">{children}</div>
    </Card>
  );
}

/** PinToggle keeps this Metric in the sidebar, or takes it out (ADR 0025). It is
 *  the only place a Metric gets pinned, because the gesture is "pin this page";
 *  unpinning has a second entry point on the sidebar row itself, which is where
 *  you notice you no longer want it. A pinned Metric is also what the cross-metric
 *  page pairs, so this is the one control that puts a Metric on that screen. */
function PinToggle({ metric }: { metric: string }) {
  const pins = usePins();
  const addPin = useAddPin();
  const removePin = useRemovePin();
  const pinned = (pins.data ?? []).some((p) => p.metric === metric);

  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("size-7", pinned ? "text-primary" : "text-muted-foreground")}
      aria-pressed={pinned}
      aria-label={pinned ? "Unpin from sidebar" : "Pin to sidebar"}
      title={pinned ? "Unpin from sidebar and cross-metric" : "Pin to the sidebar and cross-metric"}
      onClick={() => (pinned ? removePin.mutate(metric) : addPin.mutate(metric))}
    >
      {pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
    </Button>
  );
}

/** NotesToggle shows or hides the Account's Annotations on this page's chart
 *  (ADR 0030). Unlike the Dashboard's, this toggle is not persisted: a Metric page
 *  has no stored time axis to hang it on, and giving it one would be the first step
 *  toward the one-Panel Dashboard ADR 0025 refused. It hides itself while the
 *  Account has no notes at all, for the same reason the Dashboard's does. */
function NotesToggle({ on, onToggle }: { on: boolean; onToggle: () => void }) {
  const all = useAllAnnotations();
  if (!all.data?.length) return null;

  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("size-7", on ? "text-foreground" : "text-muted-foreground")}
      aria-pressed={on}
      aria-label={on ? "Hide notes" : "Show notes"}
      title={on ? "Hide notes" : "Show notes"}
      onClick={onToggle}
    >
      <StickyNote className="size-3.5" />
    </Button>
  );
}
