import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

/** The typographic layer this interface is built on.
 *
 *  One decision carries it: **every number is mono and tabular**. A figure, a date,
 *  a bucket key, a unit divisor, a coefficient, a percentage — all of them. It is
 *  not an aesthetic preference. A dashboard is a grid of figures read by comparison,
 *  and proportional digits make a column of them ragged: `74.8` sits narrower than
 *  `11.2`, so the eye cannot use the left edge, and a value that changes under a
 *  live poll makes the layout twitch. `font-mono tabular-nums` fixes both, and it
 *  marks what is data as against what is prose.
 *
 *  Everything here is expressed in tokens. There is no hex in this file and there
 *  must never be one: Verve ships nine palettes in two modes (ADR 0024, ADR 0026),
 *  and a literal colour is a bug in seventeen of the eighteen combinations. Where
 *  the design called for an off-grey with no token behind it, the token it becomes
 *  is `muted-foreground` at an opacity — which tracks the palette instead of
 *  fighting it, and stays legible in light mode where a fixed dark grey would not. */

const figureVariants = cva("font-mono tabular-nums leading-none", {
  variants: {
    size: {
      /** hero is the Metric page's headline: the largest figure in the app. */
      hero: "text-[2.125rem] font-semibold tracking-figure",
      /** wide is a two-column Panel's headline. */
      wide: "text-[1.75rem] font-semibold tracking-figure",
      /** panel is a single Panel's headline figure. */
      panel: "text-2xl font-semibold",
      /** strip is a report or scatter figure. */
      strip: "text-[1.375rem] font-semibold",
      /** stat is a small stat card's figure. */
      stat: "text-lg font-semibold",
      /** inline is a figure inside a row of prose or a table cell. */
      inline: "text-xs font-medium",
    },
  },
  defaultVariants: { size: "panel" },
});

export interface FigureProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof figureVariants> {}

/** Figure is a number the eye is meant to land on. */
export function Figure({ className, size, ...props }: FigureProps) {
  return <span className={cn(figureVariants({ size }), className)} {...props} />;
}

/** Unit is the small marker beside a Figure: the unit itself at full weight, and
 *  any divisor ("/day", "/night", "avg") recessed behind it — the divisor qualifies
 *  the number, it is not part of it. */
export function Unit({
  children,
  divisor,
  className,
}: {
  children?: React.ReactNode;
  divisor?: React.ReactNode;
  className?: string;
}) {
  if (!children && !divisor) return null;
  return (
    <span className={cn("text-2xs text-muted-foreground", className)}>
      {children}
      {divisor && <span className="opacity-70">{divisor}</span>}
    </span>
  );
}

/** Eyebrow is a section label above a list: the sidebar's "DASHBOARDS", a table's
 *  column head. Small, spaced and quiet — it names a region rather than titling it. */
export function Eyebrow({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn(
        "block text-3xs font-medium uppercase tracking-eyebrow text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

/** Meta is the mono note beside a title: "stacked · 214 nights recorded",
 *  "weekly buckets · copyable". It says how a thing was computed, in the same
 *  typeface as the figures it explains. */
export function Meta({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn("truncate font-mono text-3xs tabular-nums text-muted-foreground/70", className)}
      {...props}
    />
  );
}

/** Chip is a bordered mono label — a unit and rule beside a metric name, an event
 *  kind, a resolved range. It carries no colour of its own: it is a frame around a
 *  fact, and colouring it would imply a status it does not have. */
export function Chip({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded border px-1.5 py-0.5 font-mono text-3xs tabular-nums text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

/** SectionTitle is the 13px title of a Panel, a card or a section. */
export function SectionTitle({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return <span className={cn("truncate text-heading font-medium", className)} {...props} />;
}

/** ScreenTitle is a page's h1. */
export function ScreenTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h1 className={cn("text-screen font-semibold tracking-screen", className)} {...props} />;
}

/** Dot is a small round colour key — a timeline entry's kind, an annotation chip.
 *  The colour is passed in as a resolved token string, never a hex. */
export function Dot({ color, className }: { color: string; className?: string }) {
  return (
    <span
      className={cn("inline-block size-1.5 shrink-0 rounded-full", className)}
      style={{ background: color }}
      aria-hidden
    />
  );
}

/** Key is a small square colour key, the shape a chart legend uses. */
export function Key({ color, className }: { color: string; className?: string }) {
  return (
    <span
      className={cn("inline-block size-2 shrink-0 rounded-[2px]", className)}
      style={{ background: color }}
      aria-hidden
    />
  );
}

/** LegendItem is one entry of a chart key: a swatch, a name, and optionally the
 *  Series' own figure and unit — which is what lets a multi-Metric Panel keep every
 *  magnitude legible without a headline figure (ADR 0019, ADR 0020). */
export function LegendItem({
  color,
  children,
  className,
}: {
  color: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span className={cn("flex items-baseline gap-1.5 text-2xs text-muted-foreground", className)}>
      <Key color={color} className="translate-y-px" />
      {children}
    </span>
  );
}

/** Track is a thin proportional bar: a correlation's strength, an import's
 *  progress. `fill` is the fraction in [0, 1]; the colour is the caller's, because
 *  what the bar means differs (identity on one screen, direction on another). */
export function Track({
  fill,
  color,
  className,
  animated,
}: {
  fill: number;
  color: string;
  className?: string;
  animated?: boolean;
}) {
  const pct = Math.max(0, Math.min(1, fill)) * 100;
  return (
    <span className={cn("block h-1.5 w-full overflow-hidden rounded-full bg-muted", className)}>
      <span
        className={cn("block h-full rounded-full", animated && "transition-[width] duration-300 ease-linear")}
        style={{ width: `${pct}%`, background: color }}
      />
    </span>
  );
}

/** Rule is the 1px divider used inside a card, one step quieter than the card's own
 *  border so a row separator never competes with the edge that holds the card. */
export const RULE = "border-border/60";
