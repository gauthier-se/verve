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

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
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
  dashboardRoute,
  dataRoute,
  metricRoute,
  crossRoute,
  historyRoute,
  workoutsRoute,
  workoutRoute,
  planRoute,
  importRoute,
]);

export const router = createRouter({ routeTree, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
