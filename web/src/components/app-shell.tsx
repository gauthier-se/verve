import * as React from "react";
import { Link, useParams } from "@tanstack/react-router";
import { useHotkeys } from "react-hotkeys-hook";
import { Activity, LogOut, Plus } from "lucide-react";
import { useLogout, useMe } from "@/hooks/use-auth";
import { useDashboards } from "@/hooks/use-dashboards";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { NewDashboardDialog } from "./new-dashboard-dialog";
import { ThemeToggle } from "./theme";

/** AppShell is the persistent frame: a sidebar listing the Account's dashboards
 *  (the switcher) with create / theme / logout controls, and the routed content. */
export function AppShell({ children }: { children: React.ReactNode }) {
  const me = useMe();
  const dashboards = useDashboards();
  const logout = useLogout();
  const [createOpen, setCreateOpen] = React.useState(false);
  const params = useParams({ strict: false }) as { dashboardId?: string };
  const activeId = params.dashboardId;

  // Hotkey: "n" opens the new-dashboard dialog (react-hotkeys-hook, ADR 0013).
  useHotkeys("n", () => setCreateOpen(true), { preventDefault: true });

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-60 shrink-0 flex-col border-r bg-card/40">
        <div className="flex items-center gap-2 px-4 py-4">
          <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Activity className="size-5" />
          </div>
          <span className="text-lg font-semibold">Verve</span>
        </div>

        <div className="flex items-center justify-between px-4 pb-2">
          <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Dashboards</span>
          <Button variant="ghost" size="icon" className="size-6" onClick={() => setCreateOpen(true)} aria-label="New dashboard">
            <Plus className="size-4" />
          </Button>
        </div>

        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2">
          {dashboards.data?.length === 0 && (
            <p className="px-2 py-1 text-sm text-muted-foreground">No dashboards yet.</p>
          )}
          {dashboards.data?.map((d) => (
            <Link
              key={d.id}
              to="/d/$dashboardId"
              params={{ dashboardId: String(d.id) }}
              className={cn(
                "block truncate rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent",
                activeId === String(d.id) ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground",
              )}
            >
              {d.name}
            </Link>
          ))}
        </nav>

        <div className="flex items-center justify-between border-t px-3 py-3">
          <span className="truncate text-xs text-muted-foreground" title={me.data?.email}>
            {me.data?.email}
          </span>
          <div className="flex items-center">
            <ThemeToggle />
            <Button variant="ghost" size="icon" onClick={() => logout.mutate()} aria-label="Sign out">
              <LogOut className="size-4" />
            </Button>
          </div>
        </div>
      </aside>

      <main className="flex-1 overflow-x-hidden">{children}</main>

      <NewDashboardDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}
