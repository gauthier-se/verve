import * as React from "react";
import { GitCompareArrows } from "lucide-react";
import { useUpdateDashboard } from "@/hooks/use-dashboards";
import type { BaselineRule, Dashboard } from "@/lib/types";
import { DayRangePicker } from "./day-range-picker";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

/** The Baseline rules the control offers, in display order (ADR 0015). `none` is
 *  comparison off; the two relative rules recompute server-side; `custom` reveals
 *  the date-range picker. */
const RULE_OPTIONS: { value: BaselineRule; label: string }[] = [
  { value: "none", label: "No comparison" },
  { value: "previous", label: "Previous period" },
  { value: "same_period_last_year", label: "Same period last year" },
  { value: "custom", label: "Custom…" },
];

/** ComparisonControl is the Dashboard-global period-comparison control, the
 *  companion to the Time range (ADR 0015). Picking a Baseline rule patches the
 *  Dashboard, which refetches every Panel with the baseline overlaid. It is
 *  disabled when the range is `all` — nothing precedes "all". */
export function ComparisonControl({ dashboard }: { dashboard: Dashboard }) {
  const update = useUpdateDashboard();
  const [open, setOpen] = React.useState(false);
  // "custom" can be chosen before its bounds exist, but the API rejects a
  // bound-less custom rule — so we hold that selection locally and only persist it
  // once a full range is picked. Until then the stored rule is unchanged.
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
