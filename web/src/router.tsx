import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppShell } from "./components/app-shell";
import { DashboardIndex } from "./components/dashboard-index";
import { DashboardView } from "./components/dashboard-view";

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

const routeTree = rootRoute.addChildren([indexRoute, dashboardRoute]);

export const router = createRouter({ routeTree, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
