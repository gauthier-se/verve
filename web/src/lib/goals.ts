// Goal presentation and input (ADR 0044). The rules that decide anything live on the
// server; what is here is how a Goal is written as a sentence and how a typed value
// becomes the canonical one the API stores.
import { formatFigure } from "./format";
import { toStoredValue } from "./metrics";
import type { Aggregation, Attainment, Goal, GoalDirection, GoalSegment, Metric, Series } from "./types";

/** goalEligible mirrors the server's rule: every Metric but a `latest` one. "75 kg"
 *  is a destination reached once, not a bound held daily, and that question is a
 *  Phase's. Asked of the rule rather than a list, as the server does, so a new
 *  Catalog entry needs no decision here either. */
export function goalEligible(metric: Metric): boolean {
  return metric.aggregation !== "latest";
}

/** isDuration is whether a Metric's Goal is typed as hours and minutes: sleep's
 *  canonical unit is the minute, and nobody sets "at least 420". */
export function isDuration(metric: Metric): boolean {
  return metric.aggregation === "duration_by_state";
}

const DIRECTION_WORDS: Record<GoalDirection, string> = {
  at_least: "At least",
  at_most: "At most",
};

/** goalValue renders a stored Goal value the way the same Metric's figures render,
 *  so the bound and the day it is read against are written alike: "7h 00m", "7 500",
 *  "96" for a stored 0.96. */
export function goalValue(value: number, metric: FigureRule): string {
  return formatFigure(value, metric.aggregation ?? "", metric.unit);
}

/** FigureRule is what writing a value needs: its unit and its rule. A Series carries
 *  both, so a Panel can write a bound without looking the Metric up. */
type FigureRule = { unit: string; aggregation?: Aggregation | "" };

/** goalUnit is the unit to print after the value, or empty when it says nothing: a
 *  duration carries its own, and "count" is the Metric's name, already on screen. */
export function goalUnit(metric: Metric): string {
  if (isDuration(metric) || metric.unit === "count") return "";
  return metric.unit;
}

/** describeGoal writes a Goal as a fact, never a verdict: "At least 7 500 a day".
 *  Sleep is judged per Night (ADR 0027), so its Goal reads "a night". */
export function describeGoal(goal: Pick<Goal, "direction" | "value">, metric: Metric): string {
  const unit = goalUnit(metric);
  const per = isDuration(metric) ? "a night" : "a day";
  return [DIRECTION_WORDS[goal.direction], goalValue(goal.value, metric), unit, per].filter(Boolean).join(" ");
}

/** GoalInput is what the form holds: a plain number, or hours and minutes for a
 *  duration Metric. Strings, because that is what an input gives back. */
export interface GoalInput {
  value: string;
  hours: string;
  minutes: string;
}

/** storedGoalValue turns the form's input into the canonical value the API takes, or
 *  null when nothing valid was typed. A percent is typed 0–100 and stored as a
 *  fraction; a duration is typed as hours and minutes and stored in minutes. */
export function storedGoalValue(input: GoalInput, metric: Metric): number | null {
  if (isDuration(metric)) {
    if (input.hours.trim() === "" && input.minutes.trim() === "") return null;
    const h = Number(input.hours || 0);
    const m = Number(input.minutes || 0);
    if (!Number.isFinite(h) || !Number.isFinite(m) || h < 0 || m < 0) return null;
    return h * 60 + m;
  }
  if (input.value.trim() === "") return null;
  const typed = Number(input.value);
  if (!Number.isFinite(typed)) return null;
  return toStoredValue(metric.unit, typed);
}

/** todayUTC is the day the server calls today: windows and Goals are dated at UTC
 *  midnight (ADR 0012), so the form's default and its upper bound follow the server's
 *  clock rather than the browser's zone. */
export function todayUTC(now: Date = new Date()): string {
  return now.toISOString().slice(0, 10);
}

/** lastDayHeld is the last day a closed Goal was in force. `ended_on` is exclusive,
 *  the first day it no longer held, which is right for arithmetic and wrong for a
 *  person reading "7 Mar → 12 Mar". */
export function lastDayHeld(endedOn: string): string {
  const d = new Date(`${endedOn}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() - 1);
  return d.toISOString().slice(0, 10);
}

/** goalAt is the Goal value in force on a day bucket, or undefined where none was:
 *  what the step line is drawn from. Segments are half-open [from, to) and dates
 *  are YYYY-MM-DD, so string comparison is chronological. */
export function goalAt(attainment: Attainment | undefined, bucket: string): number | undefined {
  return segmentAt(attainment, bucket)?.value;
}

/** segmentAt is the whole Goal segment in force on a day bucket, for a reader that
 *  needs the direction as well as the value: the Ledger's Goal column. */
export function segmentAt(attainment: Attainment | undefined, bucket: string): GoalSegment | undefined {
  return attainment?.segments.find((s) => s.from <= bucket && bucket < s.to);
}

/** drawsGoalLine is whether a Series gets a line: only at day grain, where a bar is
 *  a day and the bound is a day's. Above it the counts stay and the line goes, since
 *  a weekly bar against a daily bound is the comparison ADR 0044 refuses. */
export function drawsGoalLine(s: Series): boolean {
  return s.bucket === "day" && (s.goal?.segments.length ?? 0) > 0;
}

const SYMBOLS: Record<GoalDirection, string> = { at_least: "≥", at_most: "≤" };

/** goalBound writes the bound the counts are against ("≥ 7 500"), or null when the
 *  Goal changed inside the window and no single bound is true of every day. */
export function goalBound(attainment: Attainment, rule: FigureRule): string | null {
  const [first, ...rest] = attainment.segments;
  if (!first) return null;
  if (rest.some((s) => s.direction !== first.direction || s.value !== first.value)) return null;
  return boundText(first, rule);
}

/** boundText writes one bound, "≥ 7 500" or "≤ 2 300 mg": the same words on a Panel's
 *  counts and beside a Day's value, so the two cannot describe one Goal differently. */
export function boundText(goal: { direction: GoalDirection; value: number }, rule: FigureRule): string {
  const unit = rule.aggregation === "duration_by_state" || rule.unit === "count" ? "" : rule.unit;
  return [SYMBOLS[goal.direction], goalValue(goal.value, rule), unit].filter(Boolean).join(" ");
}

/** attainmentText is the counts as the legend prints them: the met days over the
 *  measured ones, always with the denominator, never a percentage (ADR 0044). The
 *  title says what the short form cannot: how many days had no data, and that today
 *  is not counted. Words, not colour: it is a count, not a grade. */
export function attainmentText(attainment: Attainment, rule: FigureRule): { short: string; title: string } {
  const { met, measured, covered } = attainment;
  const bound = goalBound(attainment, rule);
  const unmeasured = covered - measured;
  const title = [
    `${met} of ${measured} measured ${measured === 1 ? "day" : "days"} met the goal${bound ? ` (${bound})` : ""}.`,
    `${covered} ${covered === 1 ? "day" : "days"} under a goal`,
    unmeasured > 0 ? `, ${unmeasured} with no data.` : ".",
    " Today is not counted.",
  ].join("");
  if (measured === 0) {
    return { short: covered === 0 ? "goal set, nothing to count yet" : "no measured day under the goal", title };
  }
  return { short: `${met} of ${measured} days ${bound ?? "at goal"}`, title };
}
