import * as React from "react";
import { GitCompareArrows } from "lucide-react";
import { useUpdateDashboard } from "@/hooks/use-dashboards";
import type { BaselineRule, Dashboard } from "@/lib/types";
import { DayRangePicker } from "./day-range-picker";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

/** The Baseline rules the control offers, in display order (ADR 0015). */
const RULE_OPTIONS: { value: BaselineRule; label: string }[] = [
  { value: "none", label: "No comparison" },
  { value: "previous", label: "Previous period" },
  { value: "same_period_last_year", label: "Same period last year" },
  { value: "custom", label: "Custom…" },
];

/** ComparisonControl is the Dashboard-global period-comparison control (ADR 0015):
 *  picking a rule patches the Dashboard, refetching every Panel. Disabled at `all`. */
export function ComparisonControl({ dashboard }: { dashboard: Dashboard }) {
  const update = useUpdateDashboard();
  const [open, setOpen] = React.useState(false);
  // "custom" can be picked before its bounds exist; hold it locally and persist
  // only once a full range is chosen (the API rejects a bound-less custom rule).
  const [draftCustom, setDraftCustom] = React.useState(false);

  const disabled = dashboard.range_preset === "all";
  const rule: BaselineRule = draftCustom ? "custom" : dashboard.baseline_rule;

  const onRuleChange = (next: BaselineRule) => {
    if (next === "custom") {
      // Reopen the picker; keep existing bounds if already on custom.
      setDraftCustom(true);
      setOpen(true);
      return;
    }
    setDraftCustom(false);
    if (next === dashboard.baseline_rule) return;
    update.mutate({ id: dashboard.id, patch: { baseline_rule: next } });
  };

  const onSelectRange = (from: string, to: string) => {
    update.mutate({ id: dashboard.id, patch: { baseline_rule: "custom", baseline_from: from, baseline_to: to } });
    setDraftCustom(false);
  };

  return (
    <div className="flex flex-wrap items-center gap-1" title={disabled ? "Comparison is unavailable for the All range" : undefined}>
      <GitCompareArrows className="size-4 text-muted-foreground" aria-hidden />
      <Select value={rule} onValueChange={(v) => onRuleChange(v as BaselineRule)} disabled={disabled}>
        <SelectTrigger className="h-8 w-[190px]" aria-label="Comparison">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {RULE_OPTIONS.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {rule === "custom" && !disabled && (
        <DayRangePicker
          from={dashboard.baseline_from}
          to={dashboard.baseline_to}
          onSelect={onSelectRange}
          placeholder="Pick dates"
          open={open}
          onOpenChange={setOpen}
        />
      )}
    </div>
  );
}
