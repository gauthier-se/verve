// The colours every chart draws with, as token references and nothing else.
//
// Recharts takes colours as strings, so they cannot be Tailwind classes; they are
// written as `hsl(var(--token))` instead, which resolves against whichever Palette
// and mode are on the root element. That indirection is the whole contract: nine
// palettes times two modes (ADR 0024, ADR 0026), and a literal colour anywhere in
// the drawing code is a bug in seventeen of the eighteen.
//
// This module exists so that the list is in one place rather than at the top of
// whichever component drew something first. Everything that puts ink on a chart
// imports from here.

/** token references a CSS custom property as a colour, optionally at an alpha. */
function token(name: string, alpha?: number): string {
  return alpha === undefined ? `hsl(var(--${name}))` : `hsl(var(--${name}) / ${alpha})`;
}

/** SERIES_COLORS is the categorical ramp, assigned by position in the Panel and
 *  never by Metric identity (ADR 0020). Every Palette is verified for four-way
 *  separation across exactly these four (ADR 0026). */
export const SERIES_COLORS = [token("chart-1"), token("chart-2"), token("chart-3"), token("chart-4")];

/** PRIMARY is the interface's own accent, used on a chart only for marks that are
 *  about the reading rather than about a Series — a fitted line, a progress fill. */
export const PRIMARY = token("primary");

/** GRID and AXIS are the chart's furniture: the border tone for rules, the muted
 *  text tone for ticks. */
export const GRID = token("border");
export const AXIS = token("muted-foreground");

/** RECESSED is the tone for everything that is context rather than content: the
 *  Baseline overlay (ADR 0015), an Annotation marker (ADR 0030), the awake segment
 *  of a Night (ADR 0027), a gap in the history, a relationship too weak to rank.
 *
 *  It is `muted-foreground` faded rather than a fixed grey, which is what keeps it
 *  working in both modes: a literal mid-grey is a legible step down from white on a
 *  dark ground and an almost-black smear on a light one. Faded from the palette's
 *  own muted tone, it sits the same distance from the text colour either way. */
export const RECESSED = token("muted-foreground", 0.55);

/** POSITIVE and NEGATIVE encode the *sign* of a signed Metric — warm above zero,
 *  cool below (ADR 0014) — and, on the cross-metric matrix, the direction of a
 *  relationship. Neither is a valence: Verve does not know which way is good. */
export const POSITIVE = token("chart-positive");
export const NEGATIVE = token("chart-negative");

/** DIRECTION_UP and DIRECTION_DOWN are the two hues the co-variation matrix shades
 *  with: together in the accent, opposite in the cool chart colour. Direction, never
 *  valence — which is why they are the identity colours and not the sign pair, whose
 *  warm/cool reading a reader would take as good/bad. */
export const DIRECTION_UP = "var(--chart-1)";
export const DIRECTION_DOWN = "var(--chart-2)";

/** shade renders one of the direction hues at an alpha, for a matrix cell. */
export function shade(hue: string, alpha: number): string {
  return `hsl(${hue} / ${alpha})`;
}
