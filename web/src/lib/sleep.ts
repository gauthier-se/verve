// The Stage vocabulary: how a Night's minutes are labelled, ordered and coloured
// (CONTEXT.md: Stage, ADR 0027). Kept apart from metrics.ts because it is the only
// Metric-specific vocabulary in the client, and apart from panel-chart.tsx because
// the chart imports it.
import type { Point, Series } from "./types";

/** SLEEP_STAGES is every Stage the server can send, bottom to top in the stack.
 *  `awake` sits on top because an interruption reads as a break in the night rather
 *  than a foundation of it; `in_bed` sits just under it and in practice never shares
 *  a Night with the staged three (the server drops it when Stages exist, ADR 0027). */
export const SLEEP_STAGES = [
  "asleep_deep",
  "asleep_core",
  "asleep_rem",
  "asleep",
  "in_bed",
  "awake",
] as const;

export type SleepStage = (typeof SLEEP_STAGES)[number];

export const STAGE_LABEL: Record<string, string> = {
  asleep_deep: "Deep",
  asleep_core: "Core",
  asleep_rem: "REM",
  asleep: "Asleep",
  in_bed: "In bed",
  awake: "Awake",
};

/** STAGE_COLOR_INDEX assigns each Stage a fixed slot in the Palette's categorical
 *  ramp, so a Stage keeps its colour whatever a given Night happens to contain — a
 *  night with no REM must not repaint deep sleep.
 *
 *  Four slots for six Stages, which costs nothing in practice: `in_bed` never shares
 *  a Night with a Stage, and `asleep` (unspecified) is what a Source sends *instead*
 *  of core/deep/REM, so it takes core's slot. Four concurrent segments is also
 *  exactly what every Palette's ramp is verified for (ADR 0026). */
export const STAGE_COLOR_INDEX: Record<string, number> = {
  asleep_deep: 0,
  asleep_core: 1,
  asleep_rem: 2,
  asleep: 1,
  in_bed: 0,
  awake: 3,
};

/** stageLabel humanizes a Stage slug, falling back to the slug for one Verve does
 *  not know — a Connector may send a Stage this build has never heard of, and an
 *  unlabelled segment is better than a missing one. */
export function stageLabel(stage: string): string {
  return STAGE_LABEL[stage] ?? stage.replace(/_/g, " ");
}

/** isSleepSeries reports whether a Series carries a Stage breakdown to stack. */
export function isSleepSeries(s: Series | undefined): boolean {
  return s?.aggregation === "duration_by_state";
}

/** stagesPresent lists the Stages a set of Points actually contains, in stack order,
 *  followed by any unknown Stage in the order first seen. A Stage absent from the
 *  window is absent from the chart and from the table: an empty column explains
 *  nothing. */
export function stagesPresent(points: Point[]): string[] {
  const seen = new Set<string>();
  for (const p of points) {
    for (const stage of Object.keys(p.states ?? {})) seen.add(stage);
  }
  const known = SLEEP_STAGES.filter((s) => seen.has(s));
  const unknown = [...seen].filter((s) => !SLEEP_STAGES.includes(s as SleepStage));
  return [...known, ...unknown];
}
