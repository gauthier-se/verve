import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppShell } from "./components/app-shell";
import { DashboardIndex } from "./components/dashboard-index";
import { DashboardView } from "./components/dashboard-view";
import { CrossMetricPage } from "./components/cross-metric-page";
import { DataPage } from "./components/data-page";
import { HistoryPage } from "./components/history-page";
import { ImportPage } from "./components/import-page";
import { MetricPage } from "./components/metric-page";
import { PlanPage } from "./components/plan-page";
import { SessionDetailPage } from "./components/session-detail";
import { SessionsPage } from "./components/sessions-page";
import { NightPage } from "./components/night-page";
import { DayPage } from "./components/day-page";
import { NowPage } from "./components/now-page";

// Code-based routes (no file router / codegen) keep the build a plain Vite SPA
// (ADR 0013). The Go server serves index.html on every non-/v1 path, so a deep
// link like /d/3 resolves client-side after a hard refresh.
const rootRoute = createRootRoute({
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
});

// "/" is Now: where the Account stands, rather than what a window looked like. The
// Dashboards moved under /d, which is where each of them already lived (ADR 0045).
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: NowPage,
});

const dashboardIndexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/d",
  component: DashboardIndex,
});

const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/d/$dashboardId",
  component: DashboardView,
});

const dataRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/data",
  component: DataPage,
});

const metricRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/data/$metric",
  component: MetricPage,
});

// Cross-metric and History are reads over the Account's whole data rather than over
// a Dashboard's window, so they are pages of their own beside Data rather than a
// Panel or a tab on one.
const crossRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/cross",
  component: CrossMetricPage,
});

const historyRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/history",
  component: HistoryPage,
});

const workoutsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workouts",
  component: SessionsPage,
});

// A Session is an entity, so it has a URL of its own: a workout can be linked to
// and reloaded, which a bucket on a Panel never can (ADR 0028).
const workoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workouts/$sessionId",
  component: SessionDetailPage,
});

// A Night is addressable, so it has a URL like any other entity: one night can
// be linked to and reloaded, which a bucket on a Panel never can (ADR 0041).
const nightRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/nights/$date",
  component: NightPage,
});

// A Day is addressable too, and for the opposite reason to a Night: a Night is an
// entity, a Day is the bucket every other read is already folded to. It takes no
// range and carries no axis of its own, which is what keeps it an index (ADR 0043).
const dayRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/days/$date",
  component: DayPage,
});

const planRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/plan",
  component: PlanPage,
});

const importRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/import",
  component: ImportPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  dashboardIndexRoute,
  dashboardRoute,
  dataRoute,
  metricRoute,
  crossRoute,
  historyRoute,
  workoutsRoute,
  workoutRoute,
  nightRoute,
  dayRoute,
  planRoute,
  importRoute,
]);

export const router = createRouter({ routeTree, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
