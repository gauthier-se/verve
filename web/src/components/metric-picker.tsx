import * as React from "react";
import { ChevronsUpDown } from "lucide-react";
import { useMetrics } from "@/hooks/use-catalog";
import { metricLabel } from "@/lib/metrics";
import { textMatcher } from "@/lib/search";
import type { Metric } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Eyebrow } from "./ui/figure";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { MetricIcon } from "./metric-icon";
import { SearchField } from "./search-field";

/** MetricPicker chooses one Metric out of the whole Catalog.
 *
 *  It replaces the plain dropdown the same job used to get, for the reason the Data
 *  page grew a search box: a full Apple export puts well over a hundred entries in
 *  that list, and scrolling a menu for `dietary_vitamin_b6` is not choosing, it is
 *  hunting. Typing narrows it; Enter takes the first match, so the common case is
 *  three letters and a key.
 *
 *  Derived Metrics come first, as in the panel builder — a handful of computed
 *  Metrics disappear at the bottom of an alphabetical list of everything Apple
 *  records (ADR 0014). */
export function MetricPicker({
  value,
  onChange,
  label,
  className,
}: {
  value: string;
  onChange: (slug: string) => void;
  /** label names what the choice drives, for the trigger's accessible name. */
  label: string;
  className?: string;
}) {
  const catalog = useMetrics();
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");

  const groups = React.useMemo(() => {
    const matches = textMatcher(query);
    const all = [...(catalog.data ?? [])]
      .filter((m) => matches(m.slug, m.unit))
      .sort((a, b) => a.slug.localeCompare(b.slug));
    return [
      { label: "Derived", metrics: all.filter((m) => m.nature === "derived") },
      { label: "Imported", metrics: all.filter((m) => m.nature !== "derived") },
    ].filter((g) => g.metrics.length > 0);
  }, [catalog.data, query]);

  const first = groups[0]?.metrics[0];
  const current = (catalog.data ?? []).find((m) => m.slug === value);

  const choose = (m: Metric) => {
    onChange(m.slug);
    setOpen(false);
  };

  // The query is dropped when the popover closes rather than when it opens, so the
  // list is whole the next time it is opened and nothing is filtered by a search
  // somebody typed a week ago.
  const toggle = (next: boolean) => {
    setOpen(next);
    if (!next) setQuery("");
  };

  return (
    <Popover open={open} onOpenChange={toggle}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-label={label}
          className={cn("h-8 justify-between gap-2 px-2.5 text-xs font-normal", className)}
        >
          <span className="flex min-w-0 items-center gap-2">
            <MetricIcon slug={value} className="size-3.5" />
            <span className="truncate">{metricLabel(value)}</span>
            {current && <span className="shrink-0 font-mono text-3xs text-muted-foreground">{current.unit}</span>}
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>

      <PopoverContent align="end" className="w-72 p-2">
        <div
          onKeyDown={(e) => {
            if (e.key === "Enter" && first) {
              e.preventDefault();
              choose(first);
            }
          }}
        >
          <SearchField
            autoFocus
            value={query}
            onChange={setQuery}
            label="Search the catalog"
            placeholder="Search metrics…"
          />
          <div className="mt-2 max-h-72 space-y-2 overflow-y-auto">
            {groups.map((group) => (
              <div key={group.label} className="space-y-0.5">
                <Eyebrow className="px-2 pb-0.5 pt-1">{group.label}</Eyebrow>
                {group.metrics.map((m) => (
                  <button
                    key={m.slug}
                    type="button"
                    onClick={() => choose(m)}
                    className={cn(
                      "flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent",
                      m.slug === value && "bg-accent font-medium",
                    )}
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <MetricIcon slug={m.slug} className="size-3.5" />
                      <span className="truncate">{metricLabel(m.slug)}</span>
                    </span>
                    <span className="shrink-0 font-mono text-3xs text-muted-foreground">{m.unit}</span>
                  </button>
                ))}
              </div>
            ))}
            {groups.length === 0 && (
              <p className="px-2 py-6 text-center text-xs text-muted-foreground">No metric matches.</p>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
