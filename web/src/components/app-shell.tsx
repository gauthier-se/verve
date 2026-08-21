import * as React from "react";
import { Link, useLocation, useParams } from "@tanstack/react-router";
import { useHotkeys } from "react-hotkeys-hook";
import {
  Download,
  Dumbbell,
  History,
  LogOut,
  Plus,
  Table2,
  Target,
  Waypoints,
} from "lucide-react";
import { useLogout, useMe } from "@/hooks/use-auth";
import { useDashboards } from "@/hooks/use-dashboards";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Eyebrow } from "./ui/figure";
import { NewDashboardDialog } from "./new-dashboard-dialog";
import { SummaryPrefsMenu } from "./panel-prefs";
import { AppearanceMenu } from "./appearance";
import { PinnedNav } from "./pinned-nav";

/** TOOLS is the fixed lower half of the navigation: the pages that are not a
 *  Dashboard. Declared once so the sidebar and the narrow-screen tab bar cannot
 *  drift apart — a page reachable on a desktop and not on a phone is the failure
 *  mode a hand-written second list produces. */
const TOOLS = [
  { to: "/data", label: "Data", short: "Data", icon: Table2 },
  { to: "/cross", label: "Cross-metric", short: "Cross", icon: Waypoints },
  { to: "/history", label: "History", short: "History", icon: History },
  { to: "/workouts", label: "Workouts", short: "Workouts", icon: Dumbbell },
  { to: "/plan", label: "Plan", short: "Plan", icon: Target },
  { to: "/import", label: "Import data", short: "Import", icon: Download },
] as const;

/** AppShell is the persistent frame: a sidebar listing the Account's dashboards
 *  (the switcher) with create / appearance / logout controls, and the routed
 *  content. Below the sidebar breakpoint the sidebar is replaced by a slim bar of
 *  account controls at the top and a tab bar at the bottom — the same destinations,
 *  reached with a thumb. */
export function AppShell({ children }: { children: React.ReactNode }) {
  const [createOpen, setCreateOpen] = React.useState(false);

  // Hotkey: "n" opens the new-dashboard dialog (react-hotkeys-hook, ADR 0013).
  useHotkeys("n", () => setCreateOpen(true), { preventDefault: true });

  return (
    // h-screen + overflow-hidden pins the shell to the viewport so the sidebar and the
    // routed content each own their scroll — the page itself never scrolls as one block.
    <div className="flex h-screen overflow-hidden">
      <Sidebar onCreate={() => setCreateOpen(true)} />

      <div className="flex min-w-0 flex-1 flex-col">
        <NarrowBar />
        <main className="min-h-0 flex-1 overflow-x-hidden">{children}</main>
        <TabBar />
      </div>

      <NewDashboardDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}

function Sidebar({ onCreate }: { onCreate: () => void }) {
  const me = useMe();
  const dashboards = useDashboards();
  const logout = useLogout();
  const params = useParams({ strict: false }) as { dashboardId?: string };
  const activeId = params.dashboardId;

  return (
    <aside className="hidden w-60 shrink-0 flex-col border-r bg-card/40 lg:flex">
      <div className="flex items-center gap-2.5 px-4 pb-3.5 pt-4">
        <Mark />
        <div className="flex min-w-0 flex-col leading-tight">
          <span className="text-[0.9375rem] font-semibold tracking-screen">Verve</span>
          {/* The host, in mono, because it is the answer to "where is my data": on
              this machine, at this address, and nowhere else. */}
          <span className="truncate font-mono text-3xs text-muted-foreground">
            {window.location.host}
          </span>
        </div>
      </div>

      <div className="flex items-center justify-between px-4 pb-1.5 pt-2.5">
        <Eyebrow>Dashboards</Eyebrow>
        <button
          type="button"
          onClick={onCreate}
          aria-label="New dashboard"
          className="text-muted-foreground transition-colors hover:text-foreground"
        >
          <Plus className="size-3.5" />
        </button>
      </div>

      <nav className="flex-1 space-y-px overflow-y-auto px-2">
        {dashboards.data?.length === 0 && (
          <p className="px-2 py-1 text-2xs text-muted-foreground">No dashboards yet.</p>
        )}
        {dashboards.data?.map((d) => (
          <Link
            key={d.id}
            to="/d/$dashboardId"
            params={{ dashboardId: String(d.id) }}
            className={cn(navRow, activeId === String(d.id) ? navRowActive : navRowIdle)}
          >
            <span className="truncate">{d.name}</span>
          </Link>
        ))}
      </nav>

      <PinnedNav />

      <div className="space-y-px border-t px-2 py-2">
        {TOOLS.map((tool) => (
          <ToolLink key={tool.to} to={tool.to} label={tool.label} icon={tool.icon} />
        ))}
      </div>

      <div className="flex items-center justify-between gap-2 border-t px-3 py-2.5">
        <span className="truncate font-mono text-3xs text-muted-foreground" title={me.data?.email}>
          {me.data?.email}
        </span>
        <div className="flex shrink-0 items-center">
          <SummaryPrefsMenu />
          <AppearanceMenu />
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={() => logout.mutate()}
            aria-label="Sign out"
          >
            <LogOut className="size-4" />
          </Button>
        </div>
      </div>
    </aside>
  );
}

/** Mark is the product mark: a rounded square in the Palette's own accent carrying
 *  a mono V. It is drawn rather than imported because Verve ships no image assets —
 *  and because a mark tinted by the active Palette belongs to the interface it sits
 *  in, which a fixed-colour logo would not. */
function Mark({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary font-mono text-heading font-semibold text-primary-foreground",
        className,
      )}
      aria-hidden
    >
      V
    </div>
  );
}

// One row of the sidebar's navigation, in its two states.
const navRow =
  "flex items-center gap-2 truncate rounded-md px-2 py-1.5 text-[0.8125rem] transition-colors hover:bg-accent";
const navRowActive = "bg-accent font-medium text-accent-foreground";
const navRowIdle = "text-muted-foreground";

function ToolLink({
  to,
  label,
  icon: Icon,
}: {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
}) {
  // A tool page with a detail route of its own (a workout) keeps its entry lit while
  // the detail is open: the reader has not left the section.
  const active = useLocation({
    select: (l) => l.pathname === to || l.pathname.startsWith(`${to}/`),
  });
  return (
    <Link to={to} className={cn(navRow, active ? navRowActive : navRowIdle)}>
      <Icon className="size-4 shrink-0" /> {label}
    </Link>
  );
}

/** NarrowBar carries what the hidden sidebar owned and the tab bar cannot: the
 *  mark, and the account controls. It appears only below the sidebar breakpoint. */
function NarrowBar() {
  const logout = useLogout();
  return (
    <div className="flex items-center justify-between border-b px-3 py-2 lg:hidden">
      <Link to="/" className="flex items-center gap-2">
        <Mark className="size-6 text-2xs" />
        <span className="text-[0.9375rem] font-semibold tracking-screen">Verve</span>
      </Link>
      <div className="flex items-center">
        <SummaryPrefsMenu />
        <AppearanceMenu />
        <Button
          variant="ghost"
          size="icon"
          className="size-8"
          onClick={() => logout.mutate()}
          aria-label="Sign out"
        >
          <LogOut className="size-4" />
        </Button>
      </div>
    </div>
  );
}

/** TabBar is the narrow-screen navigation: one tab per destination, the active one
 *  marked by a 3px rule in the Palette's accent. It scrolls sideways rather than
 *  dropping entries into a menu — six destinations is the whole app, and a "more"
 *  menu would hide exactly the two pages a new reader has not found yet. */
function TabBar() {
  return (
    <nav className="flex shrink-0 overflow-x-auto border-t bg-background/95 backdrop-blur lg:hidden">
      <Tab to="/" label="Dashboards" exact />
      {TOOLS.map((tool) => (
        <Tab key={tool.to} to={tool.to} label={tool.short} />
      ))}
    </nav>
  );
}

function Tab({ to, label, exact }: { to: string; label: string; exact?: boolean }) {
  const active = useLocation({
    select: (l) => (exact ? l.pathname === to : l.pathname === to || l.pathname.startsWith(`${to}/`)),
  });
  return (
    <Link
      to={to}
      className={cn(
        "flex min-h-11 min-w-[4.5rem] flex-1 flex-col items-center justify-end gap-1.5 px-2 pb-2.5 pt-2 text-2xs transition-colors",
        active ? "text-foreground" : "text-muted-foreground",
      )}
    >
      <span
        className={cn("h-[3px] w-4 rounded-full", active ? "bg-primary" : "bg-transparent")}
        aria-hidden
      />
      {label}
    </Link>
  );
}
