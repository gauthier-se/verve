import { Link, useParams } from "@tanstack/react-router";
import { PinOff } from "lucide-react";
import { useMetricMap } from "@/hooks/use-catalog";
import { usePins, useRemovePin } from "@/hooks/use-pins";
import { metricLabel } from "@/lib/metrics";
import { Eyebrow } from "./ui/figure";
import { cn } from "@/lib/utils";
import { MetricIcon } from "./metric-icon";

/** PinnedNav is the sidebar's "Pinned" section: the Metrics the Account keeps one
 *  click away, beside its Dashboards (ADR 0025). It renders nothing until the
 *  Account pins something, so it introduces itself the day it first appears
 *  rather than sitting empty from day one.
 *
 *  A Pin whose Metric has left the Catalog is dropped here, at render, against
 *  the Catalog the client already holds. It is never deleted server-side, so the
 *  Pin comes back if the Metric does. A Metric with no *data* is shown normally:
 *  knowing otherwise would cost a query per Pin on every render of the shell, to
 *  prevent a click onto a page that already says it has nothing to show. */
export function PinnedNav() {
  const pins = usePins();
  const catalog = useMetricMap();
  const removePin = useRemovePin();
  const params = useParams({ strict: false }) as { metric?: string };
  const activeMetric = params.metric;

  const known = (pins.data ?? []).filter((p) => catalog.map.has(p.metric));
  if (known.length === 0) return null;

  return (
    <div className="flex min-h-0 shrink flex-col border-t px-2 py-2">
      <Eyebrow className="px-2 pb-1.5">Pinned</Eyebrow>
      {/* The list owns its own scroll and is capped, so a long list of Pins
          squeezes rather than pushes the Dashboards above it off the screen. */}
      <div className="max-h-[40vh] space-y-0.5 overflow-y-auto">
        {known.map((p) => (
          <div key={p.metric} className="group/pin flex items-center">
            <Link
              to="/data/$metric"
              params={{ metric: p.metric }}
              className={cn(
                "flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-[0.8125rem] transition-colors hover:bg-accent",
                activeMetric === p.metric
                  ? "bg-accent font-medium text-accent-foreground"
                  : "text-muted-foreground",
              )}
            >
              <MetricIcon slug={p.metric} />
              <span className="truncate">{metricLabel(p.metric)}</span>
            </Link>
            {/* Outside the Link, never inside it: an anchor must not wrap a button. */}
            <button
              type="button"
              onClick={() => removePin.mutate(p.metric)}
              aria-label={`Unpin ${metricLabel(p.metric)}`}
              className="mr-1 rounded-md p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover/pin:opacity-100"
            >
              <PinOff className="size-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
