// Drawing one Metric inside one workout (ADR 0041): the points as a chart reads
// them, and the axis they sit on.
//
// The window is the workout's own and arrives with the payload, so nothing here
// invents one. What this module does is turn timestamps into elapsed seconds,
// which is the axis a ride is actually read against: "twelve minutes in", never
// "at 06:12 UTC".
import type { SessionStat, WorkoutSeries } from "./types";

/** WorkoutDatum is one point on the elapsed-time axis. */
export interface WorkoutDatum {
  elapsed: number; // seconds since the workout began
  value: number;
}

/** workoutData projects a curve onto elapsed seconds. A point whose timestamp
 *  does not parse is dropped rather than drawn at zero, which would put it at
 *  the start line. */
export function workoutData(series: WorkoutSeries | undefined): WorkoutDatum[] {
  if (!series) return [];
  const start = Date.parse(series.start_at);
  if (Number.isNaN(start)) return [];

  const out: WorkoutDatum[] = [];
  for (const p of series.points) {
    const at = Date.parse(p.at);
    if (Number.isNaN(at)) continue;
    out.push({ elapsed: Math.round((at - start) / 1000), value: p.value });
  }
  return out;
}

/** formatElapsed renders a point on the axis: "12:30" into a ride, "1:05:00"
 *  into a long one. Hours appear only when there are any, so a 40-minute run is
 *  not labelled in three fields. */
export function formatElapsed(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const two = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${two(m)}:${two(sec)}` : `${m}:${two(sec)}`;
}

/** curveMetrics are the Metrics a workout can draw a curve for: the ones its own
 *  device reported statistics about.
 *
 *  Enumerating the Catalog instead would offer a hundred options, ninety-nine of
 *  which answer "no data", which is the Ledger's empty-row problem in a dropdown
 *  (ADR 0021). The stats are already on the page and they are exactly the list of
 *  what this workout measured. `sum` and `average` rules only: a total distance
 *  has no shape inside the ride, and the server refuses the rest anyway. */
export function curveMetrics(stats: SessionStat[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of stats) {
    if (s.stat !== "average" && s.stat !== "sum") continue;
    if (seen.has(s.metric)) continue;
    seen.add(s.metric);
    out.push(s.metric);
  }
  return out;
}
