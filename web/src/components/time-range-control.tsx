import { useUpdateDashboard } from "@/hooks/use-dashboards";
import { RANGE_PRESETS } from "@/lib/time-range";
import type { Dashboard } from "@/lib/types";
import { Button } from "./ui/button";
import { DayRangePicker } from "./day-range-picker";

/** TimeRangeControl is the Dashboard-global Time range: preset buttons plus a
 *  custom day-range picker. A change patches the Dashboard, which refetches
 *  every Panel's series — the "range updates all Panels" behavior. */
export function TimeRangeControl({ dashboard }: { dashboard: Dashboard }) {
  const update = useUpdateDashboard();

  const setPreset = (preset: (typeof RANGE_PRESETS)[number]["value"]) => {
    if (dashboard.range_preset === preset) return;
    update.mutate({ id: dashboard.id, patch: { range_preset: preset } });
  };

  const setCustom = (from: string, to: string) => {
    update.mutate({ id: dashboard.id, patch: { range_preset: "custom", range_from: from, range_to: to } });
  };

  return (
    <div className="flex flex-wrap items-center gap-1">
      <div className="flex items-center rounded-md border p-0.5">
        {RANGE_PRESETS.map((preset) => (
          <Button
            key={preset.value}
            variant={dashboard.range_preset === preset.value ? "secondary" : "ghost"}
            size="sm"
            className="h-7 px-2.5"
            onClick={() => setPreset(preset.value)}
          >
            {preset.label}
          </Button>
        ))}
      </div>

      <DayRangePicker
        from={dashboard.range_from}
        to={dashboard.range_to}
        onSelect={setCustom}
        active={dashboard.range_preset === "custom"}
      />
    </div>
  );
}
