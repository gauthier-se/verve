import * as React from "react";
import { AlertTriangle } from "lucide-react";
import { useCreateExclusion, useExclusionPreview, type ExclusionInput } from "@/hooks/use-exclusions";
import { ApiError } from "@/lib/api";
import { formatExact } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { MetricPicker } from "./metric-picker";

/** ExclusionDialog creates one Exclusion (ADR 0033), from either surface: the
 *  import page, where the Metric is chosen here, and a Metric page, where it is
 *  given and locked.
 *
 *  It is one dialog for two sentences that are the same act. "Never import this
 *  again" and "delete everything this holds" are one object, because a delete that
 *  did not also refuse the next import would quietly undo itself (ADR 0022), and
 *  this dialog's job is to make that single act legible before it happens: it names
 *  the Metric, the span, and how many stored Measurements are about to go.
 *
 *  The confirmation scales with what is at stake. With nothing stored under it,
 *  excluding a Metric destroys nothing and is one click. With rows to purge, the
 *  Metric's name has to be typed: every other destructive action in this app removes
 *  one row, one Panel, one Dashboard, and this one removes years. */
export function ExclusionDialog({
  open,
  onOpenChange,
  metric: lockedMetric,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** lockedMetric fixes the Metric (the Metric page's delete); omitted, it is chosen. */
  metric?: string;
}) {
  const create = useCreateExclusion();

  const [metric, setMetric] = React.useState(lockedMetric ?? "");
  const [startsOn, setStartsOn] = React.useState("");
  const [endsOn, setEndsOn] = React.useState("");
  const [typed, setTyped] = React.useState("");

  // Seed on every open, so the dialog never reappears holding the last Metric's
  // span, and never carries a typed confirmation across two different deletions.
  React.useEffect(() => {
    if (!open) return;
    setMetric(lockedMetric ?? "");
    setStartsOn("");
    setEndsOn("");
    setTyped("");
    create.reset();
  }, [open, lockedMetric]); // eslint-disable-line react-hooks/exhaustive-deps

  const input: ExclusionInput | null = metric ? { metric, starts_on: startsOn, ends_on: endsOn } : null;
  const preview = useExclusionPreview(open ? input : null);

  // A 422 on the preview is the same refusal the create would give (a derived
  // Metric, a state-backed one), so it is shown here rather than after the click.
  const previewError = preview.error instanceof ApiError ? preview.error : undefined;
  const count = preview.data?.measurements ?? 0;
  const destructive = count > 0;

  const error = create.error instanceof ApiError ? create.error : undefined;
  const fields = error?.fields;

  const name = metric ? metricLabel(metric) : "";
  const confirmed = !destructive || typed.trim().toLowerCase() === name.toLowerCase();
  const blocked = !metric || Boolean(previewError) || preview.isLoading || create.isPending || !confirmed;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (blocked || !input) return;
    create.mutate(input, { onSuccess: () => onOpenChange(false) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{lockedMetric ? `Delete ${name} data` : "Exclude a metric"}</DialogTitle>
        </DialogHeader>

        <form className="space-y-3" onSubmit={submit}>
          {lockedMetric ? (
            <p className="text-xs leading-relaxed text-muted-foreground">
              Every stored value of <span className="text-foreground">{name}</span> in the span below
              is deleted, and no future import brings it back: the exclusion this creates is what
              makes the deletion stick.
            </p>
          ) : (
            <div className="space-y-1.5">
              <Label>Metric</Label>
              <MetricPicker value={metric} onChange={setMetric} label="Metric to exclude" className="w-full" />
              {fields?.metric && <p className="text-xs text-destructive">{fields.metric}</p>}
            </div>
          )}

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="exclusion-from">
                From <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="exclusion-from"
                type="date"
                value={startsOn}
                onChange={(e) => setStartsOn(e.target.value)}
              />
              {fields?.starts_on && <p className="text-xs text-destructive">{fields.starts_on}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="exclusion-to">
                To <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="exclusion-to"
                type="date"
                min={startsOn || undefined}
                value={endsOn}
                onChange={(e) => setEndsOn(e.target.value)}
              />
              {fields?.ends_on && <p className="text-xs text-destructive">{fields.ends_on}</p>}
            </div>
          </div>
          <p className="text-2xs leading-relaxed text-muted-foreground/80">
            Leave both empty for the whole history of this metric. Both days are included.
          </p>

          {previewError && <p className="text-xs text-destructive">{previewError.message}</p>}

          {metric && !previewError && (
            <div className="rounded-lg border bg-muted/40 px-3.5 py-3">
              {destructive ? (
                <>
                  <p className="flex items-center gap-2 text-xs font-medium">
                    <AlertTriangle className="size-3.5 text-destructive" />
                    {formatExact(count)} stored {count === 1 ? "measurement" : "measurements"} will be
                    deleted
                  </p>
                  <p className="pt-1 text-2xs leading-relaxed text-muted-foreground">
                    Imported values come back if you remove this exclusion and import the export
                    again. Values you typed yourself do not: nothing else holds them.
                  </p>
                  <div className="space-y-1.5 pt-2.5">
                    <Label htmlFor="exclusion-confirm">
                      Type <span className="text-foreground">{name}</span> to confirm
                    </Label>
                    <Input
                      id="exclusion-confirm"
                      autoComplete="off"
                      value={typed}
                      onChange={(e) => setTyped(e.target.value)}
                    />
                  </div>
                </>
              ) : (
                <p className="text-xs leading-relaxed text-muted-foreground">
                  Nothing stored matches this, so nothing is deleted. From now on, imports will skip
                  it.
                </p>
              )}
            </div>
          )}

          {error && !fields && <p className="text-sm text-destructive">{error.message}</p>}

          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" variant={destructive ? "destructive" : "default"} disabled={blocked}>
              {create.isPending ? "Working…" : destructive ? "Delete and exclude" : "Exclude"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
