import { StickyNote } from "lucide-react";
import { useAllAnnotations } from "@/hooks/use-annotations";
import { useUpdateDashboard } from "@/hooks/use-dashboards";
import type { Dashboard } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";

/** AnnotationsControl toggles whether this Dashboard draws the Account's notes
 *  (ADR 0030). The notes themselves are Account data and are written once; only
 *  their showing is a Dashboard property, which is why this patches the Dashboard
 *  and sits next to the comparison and range controls, on the time axis they all
 *  belong to.
 *
 *  It hides itself while the Account has no Annotations at all: a switch for
 *  something that does not exist teaches nothing, and the empty state that does
 *  teach it lives on the Data page. */
export function AnnotationsControl({ dashboard }: { dashboard: Dashboard }) {
  const all = useAllAnnotations();
  const update = useUpdateDashboard();

  if (!all.data?.length) return null;

  const on = dashboard.annotations;
  return (
    <Button
      variant="outline"
      size="sm"
      className={cn("h-8 gap-1.5", !on && "text-muted-foreground")}
      aria-pressed={on}
      title={on ? "Hide notes on this dashboard" : "Show notes on this dashboard"}
      onClick={() => update.mutate({ id: dashboard.id, patch: { annotations: !on } })}
    >
      <StickyNote className="size-4" aria-hidden />
      Notes
    </Button>
  );
}
