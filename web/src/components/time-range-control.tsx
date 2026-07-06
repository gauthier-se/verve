import * as React from "react";
import { format, parseISO } from "date-fns";
import type { DateRange } from "react-day-picker";
import { CalendarDays } from "lucide-react";
import { useUpdateDashboard } from "@/hooks/use-dashboards";
import { RANGE_PRESETS } from "@/lib/time-range";
import type { Dashboard } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Calendar } from "./ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";

/** TimeRangeControl is the Dashboard-global Time range: preset buttons plus a
 *  custom day-range picker. A change patches the Dashboard, which refetches
 *  every Panel's series — the "range updates all Panels" behavior. */
export function TimeRangeControl({ dashboard }: { dashboard: Dashboard }) {
  const update = useUpdateDashboard();
  const [open, setOpen] = React.useState(false);

  const setPreset = (preset: (typeof RANGE_PRESETS)[number]["value"]) => {
    if (dashboard.range_preset === preset) return;
    update.mutate({ id: dashboard.id, patch: { range_preset: preset } });
  };

  const selected: DateRange | undefined =
    dashboard.range_preset === "custom" && dashboard.range_from && dashboard.range_to
      ? { from: parseISO(dashboard.range_from), to: parseISO(dashboard.range_to) }
      : undefined;

  const onSelectRange = (range: DateRange | undefined) => {
    if (range?.from && range?.to) {
      update.mutate({
        id: dashboard.id,
        patch: {
          range_preset: "custom",
          range_from: format(range.from, "yyyy-MM-dd"),
          range_to: format(range.to, "yyyy-MM-dd"),
        },
      });
      setOpen(false);
    }
  };

  const customLabel =
    dashboard.range_preset === "custom" && dashboard.range_from && dashboard.range_to
      ? `${dashboard.range_from} → ${dashboard.range_to}`
      : "Custom";

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

      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant={dashboard.range_preset === "custom" ? "secondary" : "outline"}
            size="sm"
            className={cn("h-8 gap-1.5", dashboard.range_preset === "custom" && "font-medium")}
          >
            <CalendarDays className="size-4" />
            {customLabel}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="end">
          <Calendar mode="range" numberOfMonths={2} selected={selected} defaultMonth={selected?.from} onSelect={onSelectRange} />
        </PopoverContent>
      </Popover>
    </div>
  );
}
