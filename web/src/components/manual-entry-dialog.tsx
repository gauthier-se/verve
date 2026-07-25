import * as React from "react";
import { Search, Trash2 } from "lucide-react";
import { useMetrics } from "@/hooks/use-catalog";
import {
  useCreateManualMeasurement,
  useDeleteManualMeasurement,
  useManualMeasurements,
} from "@/hooks/use-measurements";
import { ApiError } from "@/lib/api";
import { metricLabel } from "@/lib/metrics";
import type { ManualMeasurement, Metric } from "@/lib/types";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { MetricIcon } from "./metric-icon";

/** Percent Metrics are stored as fractions — `body_fat_percentage` is 0.27, not 27, and
 *  `oxygen_saturation` is 0.969. Nobody will type 0.27, so the field is presented in
 *  0–100 and converted here. Keyed off the Catalog unit, in exactly one place: a second
 *  copy of this rule is how a 26-point error that still looks plausible gets shipped. */
const isPercent = (metric: Metric) => metric.unit === "%";
const toStored = (metric: Metric, typed: number) => (isPercent(metric) ? typed / 100 : typed);
const toDisplay = (unit: string, stored: number) => (unit === "%" ? stored * 100 : stored);

/** localDateTimeValue formats a Date for a datetime-local input, which wants local wall
 *  time with no zone — the value the user actually means when they type "yesterday 8am". */
function localDateTimeValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** ManualEntryDialog is Verve's first write path that is not a file upload: it records a
 *  Manual entry and lists the ones already recorded so they can be removed (ADR 0022).
 *  Correcting a value is delete-then-re-enter — there is no edit, deliberately. */
export function ManualEntryDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const metrics = useMetrics();
  const create = useCreateManualMeasurement();
  const [selected, setSelected] = React.useState<Metric | null>(null);
  const [value, setValue] = React.useState("");
  const [measuredAt, setMeasuredAt] = React.useState(() => localDateTimeValue(new Date()));

  // Reset when the dialog closes so it never reopens holding a half-typed entry.
  React.useEffect(() => {
    if (!open) {
      setSelected(null);
      setValue("");
      setMeasuredAt(localDateTimeValue(new Date()));
      create.reset();
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selected) return;
    const typed = Number(value);
    if (!Number.isFinite(typed)) return;
    create.mutate(
      {
        metric: selected.slug,
        value: toStored(selected, typed),
        measured_at: new Date(measuredAt).toISOString(),
      },
      {
        onSuccess: () => {
          setValue("");
          setSelected(null);
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Enter a measurement</DialogTitle>
        </DialogHeader>

        {selected === null ? (
          <MetricPicker metrics={metrics.data ?? []} onPick={setSelected} />
        ) : (
          <form className="space-y-3" onSubmit={submit}>
            <div className="flex items-center justify-between rounded-md border px-3 py-2">
              <span className="flex items-center gap-2 text-sm font-medium">
                <MetricIcon slug={selected.slug} />
                {metricLabel(selected.slug)}
              </span>
              <Button type="button" variant="ghost" size="sm" onClick={() => setSelected(null)}>
                Change
              </Button>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="manual-value">
                  Value <span className="text-muted-foreground">({selected.unit})</span>
                </Label>
                <Input
                  id="manual-value"
                  autoFocus
                  type="number"
                  step="any"
                  inputMode="decimal"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="manual-at">Measured at</Label>
                <Input
                  id="manual-at"
                  type="datetime-local"
                  value={measuredAt}
                  onChange={(e) => setMeasuredAt(e.target.value)}
                  required
                />
              </div>
            </div>

            {create.error && (
              <p className="text-sm text-destructive">
                {create.error instanceof ApiError ? create.error.message : "Could not save the entry."}
              </p>
            )}

            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Close
              </Button>
              <Button type="submit" disabled={create.isPending || value === ""}>
                {create.isPending ? "Saving…" : "Save"}
              </Button>
            </div>
          </form>
        )}

        <RecentEntries />
      </DialogContent>
    </Dialog>
  );
}

/** MetricPicker is the searchable Catalog list. Derived Metrics are excluded: they are
 *  computed from a Formula, never entered (ADR 0014), and the API refuses them anyway —
 *  offering one would be an invitation to a 422. */
function MetricPicker({ metrics, onPick }: { metrics: Metric[]; onPick: (m: Metric) => void }) {
  const [query, setQuery] = React.useState("");

  const matches = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    return metrics
      .filter((m) => m.nature !== "derived" && m.aggregation !== "duration_by_state")
      .filter((m) => !q || m.slug.includes(q) || metricLabel(m.slug).toLowerCase().includes(q))
      .sort((a, b) => a.slug.localeCompare(b.slug));
  }, [metrics, query]);

  return (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
        <Input
          autoFocus
          placeholder="Search metrics…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="pl-8"
        />
      </div>
      <div className="max-h-64 space-y-0.5 overflow-y-auto">
        {matches.map((m) => (
          <button
            key={m.slug}
            type="button"
            onClick={() => onPick(m)}
            className="flex w-full items-center justify-between rounded-md px-2.5 py-1.5 text-left text-sm transition-colors hover:bg-accent"
          >
            <span className="flex items-center gap-2">
              <MetricIcon slug={m.slug} />
              {metricLabel(m.slug)}
            </span>
            <span className="text-xs text-muted-foreground">{m.unit}</span>
          </button>
        ))}
        {matches.length === 0 && (
          <p className="px-2 py-4 text-center text-sm text-muted-foreground">No metrics match.</p>
        )}
      </div>
    </div>
  );
}

/** RecentEntries lists what the Account has typed, with a delete affordance. It exists
 *  because a manual entry is the only Measurement Verve will remove, and a typo that
 *  cannot be addressed cannot be undone. */
function RecentEntries() {
  const entries = useManualMeasurements();
  const rows = entries.data ?? [];
  if (rows.length === 0) {
    return (
      <p className="border-t pt-3 text-xs text-muted-foreground">
        Values you enter here outrank your devices for the day they fall on, and leave every
        other day untouched.
      </p>
    );
  }
  return (
    <div className="space-y-1 border-t pt-3">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Your entries
      </p>
      <div className="max-h-40 space-y-0.5 overflow-y-auto">
        {rows.map((row) => (
          <EntryRow key={row.id} row={row} />
        ))}
      </div>
    </div>
  );
}

function EntryRow({ row }: { row: ManualMeasurement }) {
  const remove = useDeleteManualMeasurement();
  const shown = toDisplay(row.unit, row.value);
  return (
    <div className="flex items-center justify-between rounded-md px-2 py-1 text-sm hover:bg-accent/50">
      <span className="flex items-center gap-2 truncate">
        <MetricIcon slug={row.metric} />
        <span className="truncate">{metricLabel(row.metric)}</span>
      </span>
      <span className="flex items-center gap-3">
        <span className="tabular-nums">
          {shown} <span className="text-xs text-muted-foreground">{row.unit}</span>
        </span>
        <span className="text-xs text-muted-foreground">{row.measured_at.slice(0, 10)}</span>
        <Button
          variant="ghost"
          size="icon"
          className="size-6"
          disabled={remove.isPending}
          aria-label={`Delete ${metricLabel(row.metric)} entry`}
          onClick={() => remove.mutate(row.id)}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </span>
    </div>
  );
}
