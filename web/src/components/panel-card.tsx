import * as React from "react";
import { ChevronDown, ChevronUp, GripVertical, Info, Plus, Settings2, Trash2, X } from "lucide-react";
import { useDeletePanel, useUpdatePanel } from "@/hooks/use-dashboards";
import { useSeries, type BaselineParams } from "@/hooks/use-series";
import { CHART_TYPE_LABEL, compatibleChartTypes, formatFormula, metricLabel } from "@/lib/metrics";
import type { RangeTokens } from "@/lib/time-range";
import type { Bucket, ChartType, Metric, Panel, PanelMetric } from "@/lib/types";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { Label } from "./ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
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
  dragHandle?: React.ReactNode;
}

/** PanelCard renders one Panel: its Metrics combo-charted over the Dashboard's
 *  range at the server-resolved bucket — a single Metric overlaid with the
 *  Dashboard's Baseline in comparison mode (multi-Metric Panels cut it, ADR
 *  0020) — with a settings popover to edit the Metric list, per-Metric chart
 *  types, the bucket override, size, or remove it. */
export function PanelCard({ panel, catalog, range, baseline, dragHandle }: PanelCardProps) {
  const slugs = panel.metrics.map((m) => m.metric);
  const query = useSeries({ metrics: slugs, range, bucket: panel.bucket, baseline });
  const list = query.data?.series;
  const multi = panel.metrics.length > 1;
  const comparing = baseline !== undefined && baseline.rule !== "none";
  // The effective bucket comes back on the series (the server auto-derives it from
  // the span unless the Panel overrides it); the override shows before the fetch.
  const bucket = list?.[0]?.bucket ?? panel.bucket;
  // A single derived Panel surfaces its Formula on hover of the title (ADR 0014),
  // so the user understands what the number is.
  const metric = multi ? undefined : catalog.get(slugs[0] ?? "");
  const formulaTip = metric?.formula ? `Formula: ${formatFormula(metric.formula)}` : undefined;
  const title = slugs.map(metricLabel).join(" · ");

  return (
    <Card className="flex h-72 flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <div className="flex min-w-0 items-center gap-1">
          {dragHandle}
          <div className="min-w-0">
            <div
              className="flex items-center gap-1"
              title={formulaTip ?? (multi ? title : undefined)}
              aria-label={formulaTip}
            >
              <span className="truncate text-sm font-medium">{title}</span>
              {metric?.formula && <Info className="size-3.5 shrink-0 text-muted-foreground/70" />}
            </div>
            <div className="text-xs text-muted-foreground">
              {bucket}
              {panel.bucket ? "" : " (auto)"}
            </div>
          </div>
        </div>
        <PanelSettings panel={panel} catalog={catalog} />
      </div>

      {list &&
        (multi ? (
          <PanelLegend list={list} comparing={comparing} />
        ) : (
          list[0] && <PanelSummary series={list[0]} baseline={query.data?.baseline} metric={metric} />
        ))}

      <div className="min-h-0 flex-1 p-2">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <div className="flex h-full items-center justify-center px-4 text-center text-sm text-destructive">
            Couldn’t load this panel
          </div>
        ) : list ? (
          <PanelChart list={list} metrics={panel.metrics} baseline={query.data?.baseline} />
        ) : null}
      </div>
    </Card>
  );
}

/** PanelSettings is the per-Panel controls popover: the ordered Metric list
 *  (add / remove / reorder, per-Metric chart type), the bucket override, and the
 *  width. Every list edit PATCHes the whole metrics list (ADR 0020). */
function PanelSettings({ panel, catalog }: { panel: Panel; catalog: Map<string, Metric> }) {
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
        <Button variant="ghost" size="icon" className="size-7 shrink-0" aria-label="Panel settings">
          <Settings2 className="size-4" />
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

        <Button
          variant="ghost"
          size="sm"
          className="w-full justify-start text-destructive hover:text-destructive"
          onClick={() => remove.mutate(panel.id)}
        >
          <Trash2 className="size-4" /> Remove panel
        </Button>
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
      className="flex size-6 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground hover:text-foreground active:cursor-grabbing"
      aria-label="Drag to reorder"
      {...props}
    >
      <GripVertical className="size-4" />
    </button>
  );
}
