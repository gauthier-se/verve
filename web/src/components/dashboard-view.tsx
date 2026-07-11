import * as React from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Download, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { useDeleteDashboard, useUpdateDashboard, useDashboards } from "@/hooks/use-dashboards";
import { useImportStatus } from "@/hooks/use-import";
import { useMetricMap } from "@/hooks/use-catalog";
import type { BaselineParams } from "@/hooks/use-series";
import { rangeTokens } from "@/lib/time-range";
import type { Dashboard } from "@/lib/types";
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
import { AddPanelDialog } from "./add-panel-dialog";
import { DashboardGrid } from "./dashboard-grid";
import { CenteredSpinner } from "./spinner";
import { ComparisonControl } from "./comparison-control";
import { TimeRangeControl } from "./time-range-control";

/** DashboardView renders one Dashboard: its header (name, controls, global Time
 *  range) and its grid of Panels. */
export function DashboardView() {
  const { dashboardId } = useParams({ from: "/d/$dashboardId" });
  const dashboards = useDashboards();
  const metrics = useMetricMap();

  if (dashboards.isLoading) return <CenteredSpinner />;

  const dashboard = dashboards.data?.find((d) => String(d.id) === dashboardId);
  if (!dashboard) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        This dashboard doesn’t exist.
      </div>
    );
  }

  const range = rangeTokens(dashboard);
  const baseline = resolveBaseline(dashboard);

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <DashboardHeading dashboard={dashboard} />
        <div className="flex flex-wrap items-center gap-2">
          <ComparisonControl dashboard={dashboard} />
          <TimeRangeControl dashboard={dashboard} />
        </div>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
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
          />
        )}
      </div>
    </div>
  );
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
    <div className="flex items-center gap-2">
      <h1 className="text-xl font-semibold">{dashboard.name}</h1>
      <Button size="sm" variant="outline" className="h-8" onClick={() => setAddOpen(true)}>
        <Plus className="size-4" /> Add panel
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="size-8" aria-label="Dashboard menu">
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
    <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed bg-card/40 px-5 py-4">
      <div>
        <p className="font-medium">No data yet</p>
        <p className="text-sm text-muted-foreground">
          Import your Apple Health export to fill these panels.
        </p>
      </div>
      <Button asChild>
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
      <p className="text-sm text-muted-foreground">This dashboard has no panels yet.</p>
      <Button onClick={() => setAddOpen(true)}>
        <Plus className="size-4" /> Add your first panel
      </Button>
      <AddPanelDialog dashboardId={dashboardId} open={addOpen} onOpenChange={setAddOpen} />
    </div>
  );
}
