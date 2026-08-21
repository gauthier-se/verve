import * as React from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Download, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { useDeleteDashboard, useUpdateDashboard, useDashboards } from "@/hooks/use-dashboards";
import { useImportStatus } from "@/hooks/use-import";
import { useMetricMap } from "@/hooks/use-catalog";
import { useTimeAxis } from "@/hooks/use-time-axis";
import type { BaselineParams } from "@/hooks/use-series";
import { formatDayRange } from "@/lib/format";
import { rangeTokens } from "@/lib/time-range";
import type { Dashboard, TimeAxis } from "@/lib/types";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "./ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "./ui/dropdown-menu";
import { Input } from "./ui/input";
import { ScreenTitle } from "./ui/figure";
import { AddPanelDialog } from "./add-panel-dialog";
import { DashboardGrid } from "./dashboard-grid";
import { CenteredSpinner } from "./spinner";
import { AnnotationsControl } from "./annotations-control";
import { ComparisonControl } from "./comparison-control";
import { TimeRangeControl } from "./time-range-control";

/** DashboardView renders one Dashboard: its header (name, controls, global Time
 *  range) and its grid of Panels. */
export function DashboardView() {
  const { dashboardId } = useParams({ from: "/d/$dashboardId" });
  const dashboards = useDashboards();
  const metrics = useMetricMap();

  const dashboard = dashboards.data?.find((d) => String(d.id) === dashboardId);
  const range = dashboard ? rangeTokens(dashboard) : { preset: "1y" as const, from: null, to: null };
  const baseline = dashboard ? resolveBaseline(dashboard) : undefined;
  // The axis is resolved server-side for the meta line under the header — the same
  // resolution the Panels' own queries run, so the dates named there are the dates
  // the buckets came from (ADR 0012).
  const axis = useTimeAxis({ range, baseline });

  if (dashboards.isLoading) return <CenteredSpinner />;

  if (!dashboard) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        This dashboard doesn’t exist.
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      {/* Translucent and sticky: the controls stay reachable while a year of panels
          scrolls under them, and the blur keeps the curves legible behind it. */}
      <header className="sticky top-0 z-10 flex flex-wrap items-center justify-between gap-3 border-b bg-background/85 px-6 py-3.5 backdrop-blur">
        <DashboardHeading dashboard={dashboard} />
        <div className="flex flex-wrap items-center gap-2">
          <AnnotationsControl dashboard={dashboard} />
          <ComparisonControl dashboard={dashboard} />
          <TimeRangeControl dashboard={dashboard} />
        </div>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        <MetaLine dashboard={dashboard} axis={axis.data} />
        <ImportCta />
        {dashboard.panels.length === 0 ? (
          <EmptyPanels dashboardId={dashboard.id} />
        ) : (
          <DashboardGrid
            dashboardId={dashboard.id}
            panels={dashboard.panels}
            metrics={metrics.map}
            range={range}
            baseline={baseline}
            showAnnotations={dashboard.annotations}
          />
        )}
      </div>
    </div>
  );
}

/** MetaLine states what is on screen, in one row of mono: the window the panels are
 *  read over, what they are compared against, and how much is being drawn.
 *
 *  It is the line that makes a Dashboard readable at a glance without hovering
 *  anything — "these are the last 12 months, against the 12 before, in weekly
 *  buckets" — and every date in it comes from the server. */
function MetaLine({ dashboard, axis }: { dashboard: Dashboard; axis?: TimeAxis }) {
  const panels = dashboard.panels.length;
  const metrics = dashboard.panels.reduce((n, p) => n + p.metrics.length, 0);

  return (
    <div className="mb-4 flex flex-wrap items-baseline gap-x-5 gap-y-1 text-2xs text-muted-foreground">
      <span className="font-mono tabular-nums">
        {axis ? formatDayRange(axis.range.from, axis.range.last) : " "}
      </span>
      <span>{comparisonNote(axis)}</span>
      <span className="ml-auto font-mono tabular-nums">
        {panels} {panels === 1 ? "panel" : "panels"} · {metrics} {metrics === 1 ? "metric" : "metrics"}
        {axis && ` · bucket ${axis.bucket}`}
      </span>
    </div>
  );
}

/** comparisonNote names the compared window in words, or says plainly that nothing
 *  is being compared. "vs the previous period" without dates is the sentence that
 *  makes a reader wonder which period; this one answers it. */
function comparisonNote(axis?: TimeAxis): string {
  if (!axis) return "";
  if (!axis.baseline) return "no comparison";
  return `against ${formatDayRange(axis.baseline.from, axis.baseline.last)}`;
}

/** resolveBaseline forwards the Dashboard's Baseline, forced off for the `all`
 *  range (nothing precedes "all", ADR 0015) to match the greyed-out control. */
function resolveBaseline(d: Dashboard): BaselineParams {
  if (d.range_preset === "all") return { rule: "none" };
  return { rule: d.baseline_rule, from: d.baseline_from, to: d.baseline_to };
}

/** DashboardHeading shows the name, an Add-panel button, and a menu to rename or
 *  delete the dashboard. */
function DashboardHeading({ dashboard }: { dashboard: Dashboard }) {
  const [addOpen, setAddOpen] = React.useState(false);
  const [renameOpen, setRenameOpen] = React.useState(false);
  const remove = useDeleteDashboard();
  const navigate = useNavigate();

  const onDelete = () => {
    remove.mutate(dashboard.id, { onSuccess: () => navigate({ to: "/" }) });
  };

  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <ScreenTitle className="truncate">{dashboard.name}</ScreenTitle>
      <Button size="sm" variant="outline" className="h-7 gap-1.5 px-2.5 text-xs" onClick={() => setAddOpen(true)}>
        <Plus className="size-3.5" /> Add panel
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="size-7 text-muted-foreground" aria-label="Dashboard menu">
            <MoreHorizontal className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onClick={() => setRenameOpen(true)}>
            <Pencil className="size-4" /> Rename
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onDelete} className="text-destructive focus:text-destructive">
            <Trash2 className="size-4" /> Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AddPanelDialog dashboardId={dashboard.id} open={addOpen} onOpenChange={setAddOpen} />
      <RenameDialog dashboard={dashboard} open={renameOpen} onOpenChange={setRenameOpen} />
    </div>
  );
}

function RenameDialog({
  dashboard,
  open,
  onOpenChange,
}: {
  dashboard: Dashboard;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [name, setName] = React.useState(dashboard.name);
  const update = useUpdateDashboard();

  // Reset the field to the current name whenever the dialog reopens.
  React.useEffect(() => {
    if (open) setName(dashboard.name);
  }, [open, dashboard.name]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    update.mutate({ id: dashboard.id, patch: { name: name.trim() } }, { onSuccess: () => onOpenChange(false) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Rename dashboard</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || update.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** ImportCta is the onboarding banner shown while the Account has no data yet: the
 *  seeded Panels render empty, so it points straight at the Import page (ADR 0018).
 *  It retires the moment the first import lands data. */
function ImportCta() {
  const status = useImportStatus();
  if (status.data === undefined || status.data.has_data) return null;

  return (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed bg-card/40 px-5 py-4">
      <div>
        <p className="text-heading font-medium">No data yet</p>
        <p className="text-xs text-muted-foreground">
          Import your Apple Health export to fill these panels.
        </p>
      </div>
      <Button asChild size="sm">
        <Link to="/import">
          <Download className="size-4" /> Import data
        </Link>
      </Button>
    </div>
  );
}

function EmptyPanels({ dashboardId }: { dashboardId: number }) {
  const [addOpen, setAddOpen] = React.useState(false);
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <p className="text-xs text-muted-foreground">This dashboard has no panels yet.</p>
      <Button size="sm" onClick={() => setAddOpen(true)}>
        <Plus className="size-4" /> Add your first panel
      </Button>
      <AddPanelDialog dashboardId={dashboardId} open={addOpen} onOpenChange={setAddOpen} />
    </div>
  );
}
