import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { BaselineRule, Bucket, ChartType, Dashboard, Panel, RangePreset } from "@/lib/types";

const KEY = ["dashboards"];

/** useDashboards loads the Account's dashboards (each with its panels). */
export function useDashboards() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const { dashboards } = await api<{ dashboards: Dashboard[] }>("/v1/dashboards");
      return dashboards;
    },
  });
}

function useInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: KEY });
}

export function useCreateDashboard() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (input: { name: string }) =>
      api<{ dashboard: Dashboard }>("/v1/dashboards", { method: "POST", body: input }),
    onSuccess: invalidate,
  });
}

export interface DashboardPatch {
  name?: string;
  range_preset?: RangePreset;
  range_from?: string | null;
  range_to?: string | null;
  baseline_rule?: BaselineRule;
  baseline_from?: string | null;
  baseline_to?: string | null;
}

export function useUpdateDashboard() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: ({ id, patch }: { id: number; patch: DashboardPatch }) =>
      api<{ dashboard: Dashboard }>(`/v1/dashboards/${id}`, { method: "PATCH", body: patch }),
    onSuccess: invalidate,
  });
}

export function useDeleteDashboard() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api(`/v1/dashboards/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}

export interface PanelInput {
  metric: string;
  chart_type?: ChartType;
  bucket?: Bucket | null;
  width?: number;
}

export function useCreatePanel() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: ({ dashboardId, panel }: { dashboardId: number; panel: PanelInput }) =>
      api<{ panel: Panel }>(`/v1/dashboards/${dashboardId}/panels`, { method: "POST", body: panel }),
    onSuccess: invalidate,
  });
}

export interface PanelPatch {
  chart_type?: ChartType;
  bucket?: Bucket | null;
  width?: number;
}

export function useUpdatePanel() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: ({ id, patch }: { id: number; patch: PanelPatch }) =>
      api<{ panel: Panel }>(`/v1/panels/${id}`, { method: "PATCH", body: patch }),
    onSuccess: invalidate,
  });
}

export function useDeletePanel() {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api(`/v1/panels/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
}

/** useReorderPanels persists a drag-reordered grid, updating the cache
 *  optimistically so the cards stay where they were dropped instead of snapping
 *  back while the request is in flight. */
export function useReorderPanels() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ dashboardId, panelIds }: { dashboardId: number; panelIds: number[] }) =>
      api<{ dashboard: Dashboard }>(`/v1/dashboards/${dashboardId}/panels/order`, {
        method: "PATCH",
        body: { panel_ids: panelIds },
      }),
    onMutate: async ({ dashboardId, panelIds }) => {
      await qc.cancelQueries({ queryKey: KEY });
      const previous = qc.getQueryData<Dashboard[]>(KEY);
      qc.setQueryData<Dashboard[]>(KEY, (dashboards) =>
        dashboards?.map((d) =>
          d.id === dashboardId ? { ...d, panels: reorderPanels(d.panels, panelIds) } : d,
        ),
      );
      return { previous };
    },
    onError: (_err, _vars, context) => {
      if (context?.previous) qc.setQueryData(KEY, context.previous);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}

/** reorderPanels returns panels sorted to match the given id order. */
function reorderPanels(panels: Panel[], order: number[]): Panel[] {
  const byId = new Map(panels.map((p) => [p.id, p]));
  const ordered = order.map((id) => byId.get(id)).filter((p): p is Panel => p !== undefined);
  // Keep any panel not named in the order (shouldn't happen) at the end.
  for (const p of panels) if (!order.includes(p.id)) ordered.push(p);
  return ordered.map((p, i) => ({ ...p, position: i }));
}
