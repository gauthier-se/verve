import * as React from "react";
import { GripVertical, Info, Settings2, Trash2 } from "lucide-react";
import { useDeletePanel, useUpdatePanel } from "@/hooks/use-dashboards";
import { useSeries, type BaselineParams } from "@/hooks/use-series";
import { CHART_TYPE_LABEL, compatibleChartTypes, formatFormula, metricLabel } from "@/lib/metrics";
import type { RangeTokens } from "@/lib/time-range";
import type { Bucket, ChartType, Metric, Panel } from "@/lib/types";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { Label } from "./ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import { PanelChart } from "./panel-chart";
import { CenteredSpinner } from "./spinner";

interface PanelCardProps {
  panel: Panel;
  metric?: Metric;
  range: RangeTokens;
  baseline?: BaselineParams;
  dragHandle?: React.ReactNode;
}

/** PanelCard renders one Panel: its Metric charted over the Dashboard's range at
 *  the server-resolved bucket — overlaid with the Dashboard's Baseline in
 *  comparison mode — with a settings popover to switch chart type, override the
 *  bucket, resize, or remove it. */
export function PanelCard({ panel, metric, range, baseline, dragHandle }: PanelCardProps) {
  const query = useSeries({ metric: panel.metric, range, bucket: panel.bucket, baseline });
  const current = query.data?.series;
  // The effective bucket comes back on the series (the server auto-derives it from
  // the span unless the Panel overrides it); the override shows before the fetch.
  const bucket = current?.bucket ?? panel.bucket;
  // A derived Panel surfaces its Formula on hover of the title (ADR 0014), so the
  // user understands what the number is.
  const formulaTip = metric?.formula ? `Formula: ${formatFormula(metric.formula)}` : undefined;

  return (
    <Card className="flex h-72 flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <div className="flex min-w-0 items-center gap-1">
          {dragHandle}
          <div className="min-w-0">
            <div
              className="flex items-center gap-1"
              title={formulaTip}
              aria-label={formulaTip}
            >
              <span className="truncate text-sm font-medium">{metricLabel(panel.metric)}</span>
              {metric?.formula && <Info className="size-3.5 shrink-0 text-muted-foreground/70" />}
            </div>
            <div className="text-xs text-muted-foreground">
              {current?.unit ?? metric?.unit ?? ""} · {bucket}
              {panel.bucket ? "" : " (auto)"}
            </div>
          </div>
        </div>
        <PanelSettings panel={panel} metric={metric} />
      </div>

      <div className="min-h-0 flex-1 p-2">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <div className="flex h-full items-center justify-center px-4 text-center text-sm text-destructive">
            Couldn’t load this metric
          </div>
        ) : current ? (
          <PanelChart series={current} baseline={query.data?.baseline} chartType={panel.chart_type} />
        ) : null}
      </div>
    </Card>
  );
}

/** PanelSettings is the per-Panel controls popover. */
function PanelSettings({ panel, metric }: { panel: Panel; metric?: Metric }) {
  const update = useUpdatePanel();
  const remove = useDeletePanel();
  const chartTypes = metric ? compatibleChartTypes(metric) : (["bar", "line", "area"] as ChartType[]);

  const patch = (body: Parameters<typeof update.mutate>[0]["patch"]) => update.mutate({ id: panel.id, patch: body });

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="size-7 shrink-0" aria-label="Panel settings">
          <Settings2 className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">Chart type</Label>
          <Select value={panel.chart_type} onValueChange={(v) => patch({ chart_type: v as ChartType })}>
            <SelectTrigger className="h-8">
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
