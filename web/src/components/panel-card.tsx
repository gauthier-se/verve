import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronUp, GripVertical, Plus, Settings2, StickyNote, Trash2, X } from "lucide-react";
import { useDeletePanel, useUpdatePanel } from "@/hooks/use-dashboards";
import { useAnnotations } from "@/hooks/use-annotations";
import { useSeries, type BaselineParams } from "@/hooks/use-series";
import { NEGATIVE, POSITIVE } from "@/lib/chart";
import { CHART_TYPE_LABEL, compatibleChartTypes, metricLabel } from "@/lib/metrics";
import { isSleepSeries, stageColor, stageLabel, stagesPresent } from "@/lib/sleep";
import type { RangeTokens } from "@/lib/time-range";
import type { Bucket, ChartType, Metric, Panel, PanelMetric, Series } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { AnnotationDialog } from "./annotation-dialog";
import { Card } from "./ui/card";
import { LegendItem, Meta, SectionTitle } from "./ui/figure";
import { Label } from "./ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import { FormulaHint } from "./formula-hint";
import { MetricIcon } from "./metric-icon";
import { PanelChart } from "./panel-chart";
import { PanelLegend, PanelSummary } from "./panel-summary";
import { CenteredSpinner } from "./spinner";

// A Panel carries one to four Metrics spanning at most two units (ADR 0020);
// the editor mirrors the server rule so it can't build a Panel the save rejects.
const MAX_METRICS = 4;
const MAX_UNITS = 2;

interface PanelCardProps {
  panel: Panel;
  catalog: Map<string, Metric>;
  range: RangeTokens;
  baseline?: BaselineParams;
  /** showAnnotations is the Dashboard's own toggle (ADR 0030): the notes belong to
   *  the Account, drawing them here belongs to this Dashboard. */
  showAnnotations?: boolean;
  dragHandle?: React.ReactNode;
}

/** PanelCard renders one Panel: its Metrics combo-charted over the Dashboard's
 *  range at the server-resolved bucket — a single Metric overlaid with the
 *  Dashboard's Baseline in comparison mode (multi-Metric Panels cut it, ADR
 *  0020) — with a settings popover to edit the Metric list, per-Metric chart
 *  types, the bucket override, size, or remove it.
 *
 *  The card is a headline figure with a curve under it, in that order: the shape of
 *  a trend never reveals its magnitude, so the number is what the eye lands on and
 *  the chart is what it lands on next (ADR 0019). Everything else on the card —
 *  the rule it was folded by, the grain, the evidence count — is a mono note beside
 *  the title, quiet enough to skip and precise enough to trust. */
export function PanelCard({ panel, catalog, range, baseline, showAnnotations, dragHandle }: PanelCardProps) {
  const slugs = panel.metrics.map((m) => m.metric);
  const showNotes = showAnnotations !== false;
  // The last bucket the cursor was on, so an "Add a note" opened from this Panel's
  // menu prefills the day the person was looking at rather than today.
  const [hovered, setHovered] = React.useState<string | null>(null);
  const [noteOpen, setNoteOpen] = React.useState(false);
  const query = useSeries({ metrics: slugs, range, bucket: panel.bucket, baseline });
  // The Panel's own bucket override goes with the tokens, so the server folds the
  // notes onto this Panel's grid and not the Dashboard's default one. Panels sharing
  // an axis share one cached request. The toggle gates the render as well as the
  // fetch: a disabled query still serves whatever it cached while it was on.
  const notes = useAnnotations({ range, bucket: panel.bucket, enabled: showNotes });
  const list = query.data?.series;
  const multi = panel.metrics.length > 1;
  const comparing = baseline !== undefined && baseline.rule !== "none";
  // The effective bucket comes back on the series (the server auto-derives it from
  // the span unless the Panel overrides it); the override shows before the fetch.
  const bucket = list?.[0]?.bucket ?? panel.bucket;
  // A single derived Panel surfaces its Formula in a tooltip on the info icon
  // (ADR 0014), so the user understands how the number is computed.
  const metric = multi ? undefined : catalog.get(slugs[0] ?? "");
  const title = slugs.map(metricLabel).join(" · ");
  const series = multi ? undefined : list?.[0];
  const chartType = panel.metrics[0]?.chart_type;
  const stages = series && isSleepSeries(series) ? stagesPresent(series.points) : [];

  return (
    // A wider Panel is a taller Panel: a card given two columns was given them to
    // hold a longer curve, and a year of nights in 112px is a texture, not a series.
    <Card className={cn("flex flex-col", panel.width > 1 ? "h-80" : "h-72")}>
      <div className="flex items-start justify-between gap-3 px-4 pt-3.5">
        <div className="flex min-w-0 items-baseline gap-2">
          <div className="-ml-1.5 flex shrink-0 -translate-y-px items-center">{dragHandle}</div>
          {!multi && slugs[0] && <MetricIcon slug={slugs[0]} className="size-3.5 shrink-0 -translate-y-px" />}
          {!multi && slugs[0] ? (
            <Link to="/data/$metric" params={{ metric: slugs[0] }} className="min-w-0 hover:underline">
              <SectionTitle>{title}</SectionTitle>
            </Link>
          ) : (
            <SectionTitle title={multi ? title : undefined}>{title}</SectionTitle>
          )}
          {metric?.formula && <FormulaHint formula={metric.formula} />}
          <Meta className="hidden sm:inline">{panelNote({ series, metric, list, bucket, stages })}</Meta>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {stages.length > 0 && <StageLegend stages={stages} />}
          <PanelSettings panel={panel} catalog={catalog} onAddNote={() => setNoteOpen(true)} />
        </div>
      </div>

      {list &&
        (multi ? (
          <PanelLegend list={list} comparing={comparing} catalog={catalog} />
        ) : (
          list[0] && (
            <PanelSummary
              series={list[0]}
              baseline={query.data?.baseline}
              metric={metric}
              wide={panel.width > 1}
            />
          )
        ))}

      <div className="min-h-0 flex-1 px-2 pb-1 pt-2">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <div className="flex h-full items-center justify-center px-4 text-center text-xs text-destructive">
            Couldn’t load this panel
          </div>
        ) : list ? (
          <PanelChart
            list={list}
            metrics={panel.metrics}
            baseline={query.data?.baseline}
            annotations={showNotes ? notes.data : undefined}
            onHoverBucket={setHovered}
          />
        ) : null}
      </div>

      {/* A diverging bar is the one chart whose colours are not identities: they are
          the sign of the value, and the key for that has to be on the card. */}
      {!multi && chartType === "diverging_bar" && <SignLegend />}

      <AnnotationDialog open={noteOpen} onOpenChange={setNoteOpen} defaultDay={hovered} />
    </Card>
  );
}

/** panelNote is the mono line beside a Panel's title: how this figure was arrived
 *  at, in the fewest words that are still true.
 *
 *  It replaces the bare bucket name the card used to show. "week" alone answers a
 *  question nobody asked; "sum · weekly buckets" says both what was done to the
 *  numbers and over what — which is the difference between reading a chart and
 *  trusting it. */
function panelNote({
  series,
  metric,
  list,
  bucket,
  stages,
}: {
  series?: Series;
  metric?: Metric;
  list?: Series[];
  bucket: Bucket | null;
  stages: string[];
}): string {
  const grain = bucket ? `${grainWord(bucket)} buckets` : "";

  // A multi-Metric Panel says how many axes it is carrying, which is the one thing
  // about it that changes how its curves must be read (ADR 0020).
  if (list && list.length > 1) {
    const units = new Set(list.map((s) => s.unit));
    return [units.size > 1 ? "two axes" : `${list.length} metrics`, grain].filter(Boolean).join(" · ");
  }
  if (!series) return grain;

  if (stages.length > 0) {
    // Nights recorded, not days elapsed: the honest denominator behind the figure
    // above it, and the reason a sparse month is not a shortfall (ADR 0027).
    const nights = series.nights ?? 0;
    return ["stacked", nights > 0 ? `${nights} nights recorded` : grain].filter(Boolean).join(" · ");
  }
  if (metric?.nature === "derived") {
    const ratio = (metric.formula?.denominator?.length ?? 0) > 0;
    return ["derived", ratio ? "ratio" : series.unit || grain].filter(Boolean).join(" · ");
  }
  switch (series.aggregation) {
    case "sum":
      return ["sum", grain].filter(Boolean).join(" · ");
    case "average":
      return ["average", series.unit].filter(Boolean).join(" · ");
    case "latest":
      return ["latest", series.unit].filter(Boolean).join(" · ");
    default:
      return grain;
  }
}

function grainWord(bucket: Bucket): string {
  return bucket === "day" ? "daily" : bucket === "week" ? "weekly" : "monthly";
}

/** StageLegend names the segments of a stacked Night. A stacked bar is the one
 *  chart whose parts cannot be told apart by eye, so its key is not optional. */
function StageLegend({ stages }: { stages: string[] }) {
  return (
    <div className="hidden items-center gap-2.5 md:flex">
      {stages.map((stage, i) => (
        <LegendItem key={stage} color={stageColor(stage, i)}>
          {stageLabel(stage)}
        </LegendItem>
      ))}
    </div>
  );
}

/** SignLegend names the two sides of a diverging bar (ADR 0014). Warm above zero
 *  and cool below is a reading of the *sign*, not of the news: Verve does not know
 *  whether a surplus is what you wanted. */
function SignLegend() {
  return (
    <div className="flex items-center gap-3.5 px-4 pb-3 pt-1">
      <LegendItem color={POSITIVE}>surplus</LegendItem>
      <LegendItem color={NEGATIVE}>deficit</LegendItem>
    </div>
  );
}

/** PanelSettings is the per-Panel controls popover: the ordered Metric list
 *  (add / remove / reorder, per-Metric chart type), the bucket override, and the
 *  width. Every list edit PATCHes the whole metrics list (ADR 0020). */
function PanelSettings({
  panel,
  catalog,
  onAddNote,
}: {
  panel: Panel;
  catalog: Map<string, Metric>;
  onAddNote: () => void;
}) {
  const update = useUpdatePanel();
  const remove = useDeletePanel();

  const patch = (body: Parameters<typeof update.mutate>[0]["patch"]) => update.mutate({ id: panel.id, patch: body });
  const patchMetrics = (metrics: PanelMetric[]) =>
    patch({ metrics: metrics.map((m) => ({ metric: m.metric, chart_type: m.chart_type })) });

  const setChartType = (i: number, chartType: ChartType) =>
    patchMetrics(panel.metrics.map((m, j) => (j === i ? { ...m, chart_type: chartType } : m)));
  const removeMetric = (i: number) => patchMetrics(panel.metrics.filter((_, j) => j !== i));
  const moveMetric = (i: number, delta: -1 | 1) => {
    const next = [...panel.metrics];
    const j = i + delta;
    if (j < 0 || j >= next.length) return;
    [next[i], next[j]] = [next[j], next[i]];
    patchMetrics(next);
  };
  // The new entry omits its chart type so the server fills the Metric's
  // aggregation-derived default, exactly like panel creation.
  const addMetric = (slug: string) =>
    patch({
      metrics: [...panel.metrics.map((m) => ({ metric: m.metric, chart_type: m.chart_type })), { metric: slug }],
    });

  // Mirror of the server rule (ADR 0020): a candidate must not be a 5th Metric
  // nor introduce a 3rd unit. The server stays the authority; this only keeps the
  // editor from offering choices the save would reject.
  const units = new Set(panel.metrics.map((m) => catalog.get(m.metric)?.unit).filter(Boolean));
  const addable = [...catalog.values()]
    .filter((m) => !panel.metrics.some((pm) => pm.metric === m.slug))
    .filter((m) => units.has(m.unit) || units.size < MAX_UNITS)
    .sort((a, b) => a.slug.localeCompare(b.slug));

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="size-6 shrink-0 text-muted-foreground"
          aria-label="Panel settings"
        >
          <Settings2 className="size-3.5" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">Metrics</Label>
          {panel.metrics.map((pm, i) => {
            const m = catalog.get(pm.metric);
            const chartTypes = m ? compatibleChartTypes(m) : (["bar", "line", "area"] as ChartType[]);
            return (
              <div key={pm.metric} className="flex items-center gap-1">
                <div className="flex flex-col">
                  <ReorderButton
                    label={`Move ${metricLabel(pm.metric)} up`}
                    disabled={i === 0 || update.isPending}
                    onClick={() => moveMetric(i, -1)}
                  >
                    <ChevronUp className="size-3" />
                  </ReorderButton>
                  <ReorderButton
                    label={`Move ${metricLabel(pm.metric)} down`}
                    disabled={i === panel.metrics.length - 1 || update.isPending}
                    onClick={() => moveMetric(i, 1)}
                  >
                    <ChevronDown className="size-3" />
                  </ReorderButton>
                </div>
                <span className="min-w-0 flex-1 truncate text-sm" title={m ? `${metricLabel(pm.metric)} (${m.unit})` : undefined}>
                  {metricLabel(pm.metric)}
                </span>
                <Select value={pm.chart_type} onValueChange={(v) => setChartType(i, v as ChartType)}>
                  <SelectTrigger className="h-7 w-28 shrink-0 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {chartTypes.map((t) => (
                      <SelectItem key={t} value={t}>
                        {CHART_TYPE_LABEL[t]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-6 shrink-0 text-muted-foreground"
                  aria-label={`Remove ${metricLabel(pm.metric)}`}
                  disabled={panel.metrics.length === 1 || update.isPending}
                  onClick={() => removeMetric(i)}
                >
                  <X className="size-3.5" />
                </Button>
              </div>
            );
          })}
          {panel.metrics.length < MAX_METRICS ? (
            <Select value="" onValueChange={addMetric}>
              <SelectTrigger className="h-8 text-xs text-muted-foreground">
                <span className="flex items-center gap-1">
                  <Plus className="size-3.5" /> Add a metric
                </span>
              </SelectTrigger>
              <SelectContent>
                {addable.map((m) => (
                  <SelectItem key={m.slug} value={m.slug}>
                    {metricLabel(m.slug)} <span className="text-muted-foreground">({m.unit})</span>
                  </SelectItem>
                ))}
                {addable.length === 0 && (
                  <div className="px-2 py-1.5 text-xs text-muted-foreground">
                    No compatible metric — a panel spans at most two units.
                  </div>
                )}
              </SelectContent>
            </Select>
          ) : (
            <p className="text-xs text-muted-foreground">A panel carries at most {MAX_METRICS} metrics.</p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">Bucket</Label>
          <Select
            value={panel.bucket ?? "auto"}
            onValueChange={(v) => patch({ bucket: v === "auto" ? null : (v as Bucket) })}
          >
            <SelectTrigger className="h-8">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">Auto</SelectItem>
              <SelectItem value="day">Day</SelectItem>
              <SelectItem value="week">Week</SelectItem>
              <SelectItem value="month">Month</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">Width</Label>
          <Select value={String(panel.width)} onValueChange={(v) => patch({ width: Number(v) })}>
            <SelectTrigger className="h-8">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1">1 column</SelectItem>
              <SelectItem value="2">2 columns</SelectItem>
              <SelectItem value="3">3 columns</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1 border-t pt-2">
          <Button variant="ghost" size="sm" className="w-full justify-start" onClick={onAddNote}>
            <StickyNote className="size-4" /> Add a note
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start text-destructive hover:text-destructive"
            onClick={() => remove.mutate(panel.id)}
          >
            <Trash2 className="size-4" /> Remove panel
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** ReorderButton is one tiny chevron of the metric-row reorder pair. */
function ReorderButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className="flex h-3.5 w-4 items-center justify-center text-muted-foreground hover:text-foreground disabled:opacity-30"
    >
      {children}
    </button>
  );
}

/** DragHandle is the grip the sortable grid wires drag listeners onto. */
export function DragHandle(props: React.HTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className="flex size-5 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground/60 transition-colors hover:text-foreground active:cursor-grabbing"
      aria-label="Drag to reorder"
      {...props}
    >
      <GripVertical className="size-3.5" />
    </button>
  );
}
