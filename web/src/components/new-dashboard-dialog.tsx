import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCreateDashboard, useDashboardTemplates } from "@/hooks/use-dashboards";
import { ApiError } from "@/lib/api";
import { coverageText, createBody, emptyDraft, pick, typeName, type Draft } from "@/lib/new-dashboard";
import type { DashboardTemplate } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "./ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "./ui/field";
import { Input } from "./ui/input";

/** NewDashboardDialog creates a dashboard, from an empty grid or from a Dashboard
 *  template (ADR 0047), and navigates to it. */
export function NewDashboardDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [draft, setDraft] = React.useState<Draft>(emptyDraft);
  const templates = useDashboardTemplates();
  const create = useCreateDashboard();
  const navigate = useNavigate();

  const error = create.error instanceof ApiError ? create.error : undefined;
  const fields = error?.fields;

  const close = (next: boolean) => {
    if (!next) {
      setDraft(emptyDraft);
      create.reset();
    }
    onOpenChange(next);
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!draft.name.trim()) return;
    create.mutate(createBody(draft), {
      onSuccess: ({ dashboard }) => {
        close(false);
        navigate({ to: "/d/$dashboardId", params: { dashboardId: String(dashboard.id) } });
      },
    });
  };

  const options: (DashboardTemplate | null)[] = [null, ...(templates.data ?? [])];

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>New dashboard</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit}>
          <FieldGroup>
            <Field>
              <FieldLabel id="dashboard-start">Start from</FieldLabel>
              <StartPicker
                labelledBy="dashboard-start"
                options={options}
                selected={draft.template}
                onSelect={(t) => setDraft((d) => pick(d, t))}
              />
              <FieldError>{fields?.template}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="dashboard-name">Name</FieldLabel>
              <Input
                id="dashboard-name"
                placeholder="Training"
                value={draft.name}
                onChange={(e) => setDraft((d) => typeName(d, e.target.value))}
              />
              <FieldError>{fields?.name ?? (error && !fields ? error.message : undefined)}</FieldError>
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-6">
            <Button type="submit" disabled={!draft.name.trim() || create.isPending}>
              {create.isPending ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** StartPicker is a radio group over "Empty" and each template: arrows move the
 *  selection, as in any radio group, and the form's Enter submits. The coverage is
 *  muted text with its denominator, never a colour and never a reason to dim. */
function StartPicker({
  labelledBy,
  options,
  selected,
  onSelect,
}: {
  labelledBy: string;
  options: (DashboardTemplate | null)[];
  selected: string | null;
  onSelect: (t: DashboardTemplate | null) => void;
}) {
  const refs = React.useRef<(HTMLButtonElement | null)[]>([]);
  const index = Math.max(
    0,
    options.findIndex((o) => (o?.slug ?? null) === selected),
  );

  const move = (e: React.KeyboardEvent<HTMLButtonElement>, from: number) => {
    // Enter submits from the list as from the name field: a radio is chosen by
    // moving onto it, so there is nothing left for Enter to select.
    if (e.key === "Enter") {
      e.preventDefault();
      e.currentTarget.form?.requestSubmit();
      return;
    }
    const step = e.key === "ArrowDown" || e.key === "ArrowRight" ? 1 : e.key === "ArrowUp" || e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const to = (from + step + options.length) % options.length;
    onSelect(options[to]);
    refs.current[to]?.focus();
  };

  return (
    <div role="radiogroup" aria-labelledby={labelledBy} className="flex flex-col gap-1.5">
      {options.map((o, i) => {
        const checked = i === index;
        return (
          <button
            key={o?.slug ?? "empty"}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role="radio"
            aria-checked={checked}
            tabIndex={checked ? 0 : -1}
            onClick={() => onSelect(o)}
            onKeyDown={(e) => move(e, i)}
            className={cn(
              "flex flex-col items-start gap-0.5 rounded-md border px-3 py-2 text-left text-sm transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              checked ? "border-primary bg-accent" : "border-border hover:bg-accent/50",
            )}
          >
            <span className="font-medium">{o ? o.name : "Empty"}</span>
            <span className="text-muted-foreground">{o ? o.description : "A blank grid to add panels to."}</span>
            {o && <span className="text-xs text-muted-foreground">{coverageText(o)}</span>}
          </button>
        );
      })}
    </div>
  );
}
