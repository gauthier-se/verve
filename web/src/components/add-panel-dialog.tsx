import * as React from "react";
import { Search } from "lucide-react";
import { useCreatePanel } from "@/hooks/use-dashboards";
import { useMetrics } from "@/hooks/use-catalog";
import { metricLabel } from "@/lib/metrics";
import type { Metric } from "@/lib/types";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";
import { Input } from "./ui/input";

/** AddPanelDialog lists the Catalog so the user can add a Metric as a Panel. The
 *  new Panel takes the Metric's aggregation-derived default chart type (the
 *  server fills it in), which the user can change afterward. */
export function AddPanelDialog({
  dashboardId,
  open,
  onOpenChange,
}: {
  dashboardId: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const metrics = useMetrics();
  const create = useCreatePanel();
  const [query, setQuery] = React.useState("");

  // Group derived Metrics ahead of imported ones so calorie_balance, the macro
  // shares, and protein_per_kg are discoverable as a distinct family (issue 04).
  const groups = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    const matches = (m: Metric) =>
      !q || m.slug.includes(q) || metricLabel(m.slug).toLowerCase().includes(q);
    const all = [...(metrics.data ?? [])]
      .filter(matches)
      .sort((a, b) => a.slug.localeCompare(b.slug));
    return [
      { label: "Derived", metrics: all.filter((m) => m.nature === "derived") },
      { label: "Imported", metrics: all.filter((m) => m.nature !== "derived") },
    ].filter((g) => g.metrics.length > 0);
  }, [metrics.data, query]);

  const add = (metric: Metric) => {
    create.mutate(
      { dashboardId, panel: { metrics: [{ metric: metric.slug }] } },
      { onSuccess: () => onOpenChange(false) },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Add a panel</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
            <Input
              autoFocus
              placeholder="Search metrics…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="pl-8"
            />
          </div>
          <div className="max-h-80 space-y-2 overflow-y-auto">
            {groups.map((group) => (
              <div key={group.label} className="space-y-0.5">
                <div className="px-2.5 pb-0.5 pt-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {group.label}
                </div>
                {group.metrics.map((m) => (
                  <button
                    key={m.slug}
                    type="button"
                    disabled={create.isPending}
                    onClick={() => add(m)}
                    className="flex w-full items-center justify-between rounded-md px-2.5 py-1.5 text-left text-sm transition-colors hover:bg-accent disabled:opacity-50"
                  >
                    <span>{metricLabel(m.slug)}</span>
                    <span className="text-xs text-muted-foreground">{m.unit}</span>
                  </button>
                ))}
              </div>
            ))}
            {groups.length === 0 && <p className="px-2 py-4 text-center text-sm text-muted-foreground">No metrics match.</p>}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
