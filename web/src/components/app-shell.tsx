import * as React from "react";
import { Link, useLocation, useParams } from "@tanstack/react-router";
import { useHotkeys } from "react-hotkeys-hook";
import {
  Activity,
  Download,
  Dumbbell,
  History,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Table2,
  Target,
  Waypoints,
} from "lucide-react";
import { useLogout, useMe } from "@/hooks/use-auth";
import { useDashboards } from "@/hooks/use-dashboards";
import { dashboardInitial, readCollapsed, writeCollapsed } from "@/lib/sidebar";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Eyebrow } from "./ui/figure";
import { RailTip } from "./rail-tip";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";
import { NewDashboardDialog } from "./new-dashboard-dialog";
import { SummaryPrefsMenu } from "./panel-prefs";
import { AppearanceMenu } from "./appearance";
import { PinnedNav } from "./pinned-nav";
import { Mark } from "./mark";

/** TOOLS is the fixed lower half of the navigation: the pages that are not a
 *  Dashboard. Declared once so the sidebar and the narrow-screen tab bar cannot
 *  drift apart: a page reachable on a desktop and not on a phone is the failure
 *  mode a hand-written second list produces. `tab` marks the two that get a tab of
 *  their own on a phone; the rest are one tap away under More. */
const TOOLS = [
  { to: "/data", label: "Data", short: "Data", icon: Table2, tab: true },
  { to: "/cross", label: "Cross-metric", short: "Cross", icon: Waypoints, tab: false },
  { to: "/history", label: "History", short: "History", icon: History, tab: false },
  { to: "/workouts", label: "Workouts", short: "Workouts", icon: Dumbbell, tab: true },
  { to: "/plan", label: "Plan", short: "Plan", icon: Target, tab: false },
  { to: "/import", label: "Import & export", short: "Import", icon: Download, tab: false },
] as const;

/** AppShell is the persistent frame: a sidebar listing the Account's dashboards
 *  (the switcher) with create / appearance / logout controls, and the routed
 *  content. Below the sidebar breakpoint the sidebar is replaced by a slim bar of
 *  account controls at the top and a tab bar at the bottom — the same destinations,
 *  reached with a thumb. */
export function AppShell({ children }: { children: React.ReactNode }) {
  const [createOpen, setCreateOpen] = React.useState(false);
  const [collapsed, setCollapsed] = React.useState(() => readCollapsed(window.localStorage));
  const toggleSidebar = () => setCollapsed((c) => !c);
  React.useEffect(() => writeCollapsed(window.localStorage, collapsed), [collapsed]);

  // Hotkey: "n" opens the new-dashboard dialog (react-hotkeys-hook, ADR 0013).
  useHotkeys("n", () => setCreateOpen(true), { preventDefault: true });
  // "[" collapses or expands the sidebar, the key editors already use for a panel.
  useHotkeys("BracketLeft", toggleSidebar, { preventDefault: true });

  return (
    // h-screen + overflow-hidden pins the shell to the viewport so the sidebar and the
    // routed content each own their scroll — the page itself never scrolls as one block.
    <div className="flex h-screen overflow-hidden">
      <Sidebar onCreate={() => setCreateOpen(true)} collapsed={collapsed} onToggle={toggleSidebar} />

      <div className="flex min-w-0 flex-1 flex-col">
        <NarrowBar />
        <main className="min-h-0 flex-1 overflow-x-hidden">{children}</main>
        <TabBar onCreate={() => setCreateOpen(true)} />
      </div>

      <NewDashboardDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}

function Sidebar({
  onCreate,
  collapsed,
  onToggle,
}: {
  onCreate: () => void;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const me = useMe();
  const dashboards = useDashboards();
  const logout = useLogout();
  const params = useParams({ strict: false }) as { dashboardId?: string };
  const activeId = params.dashboardId;
  const nowActive = useLocation({ select: (l) => l.pathname === "/" });
  const ToggleIcon = collapsed ? PanelLeftOpen : PanelLeftClose;
  const toggleLabel = collapsed ? "Expand the sidebar" : "Collapse the sidebar";

  return (
    <aside
      className={cn(
        "hidden shrink-0 flex-col border-r bg-card/40 transition-[width] duration-150 lg:flex",
        collapsed ? "w-14" : "w-60",
      )}
    >
      <div className={cn("flex items-center gap-2.5 pb-3.5 pt-4", collapsed ? "flex-col px-2" : "px-4")}>
        <Mark />
        {!collapsed && (
          <div className="flex min-w-0 flex-1 flex-col leading-tight">
            <span className="text-[0.9375rem] font-semibold tracking-screen">Verve</span>
            {/* The host, in mono, because it is the answer to "where is my data": on
                this machine, at this address, and nowhere else. */}
            <span className="truncate font-mono text-3xs text-muted-foreground">
              {window.location.host}
            </span>
          </div>
        )}
        <RailTip label={`${toggleLabel} ([)`} show>
          <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={onToggle} aria-label={toggleLabel}>
            <ToggleIcon className="size-4" />
          </Button>
        </RailTip>
      </div>

      {/* Now sits above everything because it is where the app opens: the one page
          that says where the Account stands rather than what a window looked like. */}
      <div className="px-2 pb-1">
        <RailTip label="Now" show={collapsed}>
          <Link
            to="/"
            aria-label="Now"
            className={cn(navRow, collapsed && navRowRail, nowActive ? navRowActive : navRowIdle)}
          >
            <Activity className="size-4 shrink-0" /> {!collapsed && "Now"}
          </Link>
        </RailTip>
      </div>

      <div className={cn("flex items-center pb-1.5 pt-2.5", collapsed ? "justify-center" : "justify-between px-4")}>
        {!collapsed && <Eyebrow>Dashboards</Eyebrow>}
        <RailTip label="New dashboard (n)" show={collapsed}>
          <button
            type="button"
            onClick={onCreate}
            aria-label="New dashboard"
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            <Plus className="size-3.5" />
          </button>
        </RailTip>
      </div>

      <nav className="flex-1 space-y-px overflow-y-auto px-2">
        {dashboards.data?.length === 0 && !collapsed && (
          <p className="px-2 py-1 text-2xs text-muted-foreground">No dashboards yet.</p>
        )}
        {dashboards.data?.map((d) => (
          <RailTip key={d.id} label={d.name} show={collapsed}>
            <Link
              to="/d/$dashboardId"
              params={{ dashboardId: String(d.id) }}
              aria-label={collapsed ? d.name : undefined}
              className={cn(
                navRow,
                collapsed && navRowRail,
                activeId === String(d.id) ? navRowActive : navRowIdle,
              )}
            >
              {collapsed ? (
                <span className="text-xs font-semibold">{dashboardInitial(d.name)}</span>
              ) : (
                <span className="truncate">{d.name}</span>
              )}
            </Link>
          </RailTip>
        ))}
      </nav>

      <PinnedNav collapsed={collapsed} />

      <div className="space-y-px border-t px-2 py-2">
        {TOOLS.map((tool) => (
          <ToolLink key={tool.to} to={tool.to} label={tool.label} icon={tool.icon} collapsed={collapsed} />
        ))}
      </div>

      <div
        className={cn(
          "flex items-center gap-2 border-t py-2.5",
          collapsed ? "flex-col px-2" : "justify-between px-3",
        )}
      >
        {!collapsed && (
          <span className="truncate font-mono text-3xs text-muted-foreground" title={me.data?.email}>
            {me.data?.email}
          </span>
        )}
        <div className={cn("flex shrink-0 items-center", collapsed && "flex-col")}>
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

// One row of the sidebar's navigation, in its two states.
const navRow =
  "flex items-center gap-2 truncate rounded-md px-2 py-1.5 text-[0.8125rem] transition-colors hover:bg-accent";
const navRowActive = "bg-accent font-medium text-accent-foreground";
const navRowIdle = "text-muted-foreground";
// A row on the collapsed rail: the icon or initial alone, centred in the 56px rail.
const navRowRail = "justify-center px-0";

function ToolLink({
  to,
  label,
  icon: Icon,
  collapsed,
}: {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  collapsed: boolean;
}) {
  // A tool page with a detail route of its own (a workout) keeps its entry lit while
  // the detail is open: the reader has not left the section.
  const active = useLocation({
    select: (l) => l.pathname === to || l.pathname.startsWith(`${to}/`),
  });
  return (
    <RailTip label={label} show={collapsed}>
      <Link
        to={to}
        aria-label={collapsed ? label : undefined}
        className={cn(navRow, collapsed && navRowRail, active ? navRowActive : navRowIdle)}
      >
        <Icon className="size-4 shrink-0" /> {!collapsed && label}
      </Link>
    </RailTip>
  );
}

/** NarrowBar carries what the hidden sidebar owned and the tab bar cannot: the
 *  mark, and the account controls. It appears only below the sidebar breakpoint. */
function NarrowBar() {
  const logout = useLogout();
  return (
    <div className="flex items-center justify-between border-b px-3 py-2 lg:hidden">
      <Link to="/" className="flex items-center gap-2">
        <Mark className="size-6" />
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

/** TabBar is the narrow-screen navigation: five tabs that always fit a phone, the
 *  active one marked by a 3px rule in the Palette's accent. Dashboards opens the
 *  list of them, since a phone has no sidebar to switch from, and More opens the
 *  tools that do not get a tab. Eight tabs used to scroll sideways, which hid the
 *  last two behind a gesture nobody guesses. */
function TabBar({ onCreate }: { onCreate: () => void }) {
  const [sheet, setSheet] = React.useState<"dashboards" | "more" | null>(null);
  const path = useLocation({ select: (l) => l.pathname });
  const onDashboard = path === "/d" || path.startsWith("/d/");
  const more = TOOLS.filter((t) => !t.tab);
  const onMore = more.some((t) => path === t.to || path.startsWith(`${t.to}/`));

  return (
    <nav className="flex shrink-0 border-t bg-background/95 backdrop-blur lg:hidden">
      <Tab to="/" label="Now" exact />
      <TabButton label="Dashboards" active={onDashboard} onClick={() => setSheet("dashboards")} />
      {TOOLS.filter((t) => t.tab).map((tool) => (
        <Tab key={tool.to} to={tool.to} label={tool.short} />
      ))}
      <TabButton label="More" active={onMore} onClick={() => setSheet("more")} />

      <NavSheet open={sheet === "dashboards"} onClose={() => setSheet(null)} title="Dashboards">
        <DashboardList onPick={() => setSheet(null)} />
        <button
          type="button"
          onClick={() => {
            setSheet(null);
            onCreate();
          }}
          className={cn(sheetRow, "text-muted-foreground")}
        >
          <Plus className="size-4 shrink-0" /> New dashboard
        </button>
      </NavSheet>

      <NavSheet open={sheet === "more"} onClose={() => setSheet(null)} title="More">
        {more.map((tool) => (
          <Link key={tool.to} to={tool.to} onClick={() => setSheet(null)} className={cn(sheetRow, "text-foreground")}>
            <tool.icon className="size-4 shrink-0 text-muted-foreground" /> {tool.label}
          </Link>
        ))}
      </NavSheet>
    </nav>
  );
}

// One row of a narrow-screen sheet: a thumb-sized target.
const sheetRow = "flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-sm transition-colors hover:bg-accent";

/** NavSheet is a list that rises from the bottom of a phone screen, where the
 *  thumb already is. It is a Dialog, so focus, Escape and the backdrop behave as
 *  every other overlay in Verve does. */
function NavSheet({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="bottom-0 left-0 top-auto max-h-[80svh] max-w-none translate-x-0 translate-y-0 gap-2 overflow-y-auto rounded-t-xl p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:rounded-b-none">
        <DialogHeader className="px-3 pt-1 text-left">
          <DialogTitle className="text-sm">{title}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col">{children}</div>
      </DialogContent>
    </Dialog>
  );
}

/** DashboardList is the Account's Dashboards as sheet rows, the current one marked. */
function DashboardList({ onPick }: { onPick: () => void }) {
  const dashboards = useDashboards();
  const params = useParams({ strict: false }) as { dashboardId?: string };
  return (
    <>
      {dashboards.data?.map((d) => (
        <Link
          key={d.id}
          to="/d/$dashboardId"
          params={{ dashboardId: String(d.id) }}
          onClick={onPick}
          className={cn(sheetRow, params.dashboardId === String(d.id) ? "bg-accent font-medium" : "text-foreground")}
        >
          <span className="flex size-6 shrink-0 items-center justify-center rounded-md border text-2xs font-semibold">
            {dashboardInitial(d.name)}
          </span>
          <span className="truncate">{d.name}</span>
        </Link>
      ))}
    </>
  );
}

function TabButton({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className={cn(tabClass, active ? "text-foreground" : "text-muted-foreground")}>
      <span className={cn("h-[3px] w-4 rounded-full", active ? "bg-primary" : "bg-transparent")} aria-hidden />
      {label}
    </button>
  );
}

const tabClass =
  "flex min-h-11 min-w-0 flex-1 flex-col items-center justify-end gap-1.5 px-1 pb-2.5 pt-2 text-2xs transition-colors";

function Tab({ to, label, exact }: { to: string; label: string; exact?: boolean }) {
  const active = useLocation({
    select: (l) => (exact ? l.pathname === to : l.pathname === to || l.pathname.startsWith(`${to}/`)),
  });
  return (
    <Link
      to={to}
      className={cn(tabClass, active ? "text-foreground" : "text-muted-foreground")}
    >
      <span
        className={cn("h-[3px] w-4 rounded-full", active ? "bg-primary" : "bg-transparent")}
        aria-hidden
      />
      {label}
    </Link>
  );
}
