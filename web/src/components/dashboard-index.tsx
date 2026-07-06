import * as React from "react";
import { Navigate } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { useDashboards } from "@/hooks/use-dashboards";
import { Button } from "./ui/button";
import { NewDashboardDialog } from "./new-dashboard-dialog";
import { CenteredSpinner } from "./spinner";

/** DashboardIndex is the "/" landing: it forwards to the first dashboard, or
 *  shows an empty state that opens the create dialog when there are none. */
export function DashboardIndex() {
  const dashboards = useDashboards();
  const [createOpen, setCreateOpen] = React.useState(false);

  if (dashboards.isLoading) return <CenteredSpinner />;

  const first = dashboards.data?.[0];
  if (first) {
    return <Navigate to="/d/$dashboardId" params={{ dashboardId: String(first.id) }} replace />;
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
      <div className="flex size-14 items-center justify-center rounded-2xl bg-muted">
        <LayoutDashboard className="size-7 text-muted-foreground" />
      </div>
      <div>
        <h2 className="text-lg font-semibold">No dashboards yet</h2>
        <p className="text-sm text-muted-foreground">Create one to start charting your health metrics.</p>
      </div>
      <Button onClick={() => setCreateOpen(true)}>
        <Plus className="size-4" /> New dashboard
      </Button>
      <NewDashboardDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}
