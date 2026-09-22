// The Now screen's pure layer: the words an age is written in, and the split of the
// Sources the server already ordered (ADR 0045).
//
// Every number here arrives from the server. An age is counted there against the UTC
// today, the day Goals and windows are dated by, and it is never recomputed from this
// browser's clock: a second count of days in a second language is the drift ADR 0032
// already refused for bucket boundaries.
import type { SourceFreshness } from "./types";

/** ageText writes how long ago a day was: "today", "yesterday", "12 days ago",
 *  "5 weeks ago". Weeks start at three, because "2 weeks ago" for 14 to 20 days hides
 *  a week the reader would want to know about; days are exact up to 20. No colour and
 *  no verdict: how old is too old is the owner's to judge. */
export function ageText(days: number): string {
  if (days <= 0) return "today";
  if (days === 1) return "yesterday";
  if (days < 21) return `${days} days ago`;
  if (days < 60) return `${Math.floor(days / 7)} weeks ago`;
  const months = Math.floor(days / 30);
  if (months < 24) return `${months} months ago`;
  return `${Math.floor(days / 365)} years ago`;
}

/** lagText writes how far a Source stops behind the Account's last datum, or
 *  undefined for the Source that recorded last, which is behind nothing. */
export function lagText(days: number): string | undefined {
  if (days <= 0) return undefined;
  if (days === 1) return "1 day behind";
  if (days < 21) return `${days} days behind`;
  return `${Math.floor(days / 7)} weeks behind`;
}

/** splitSources separates the active Sources from the retired ones, keeping the
 *  server's order in each. A retired Source is listed and not raised: it is a device
 *  the Account stopped using, not one that went quiet. */
export function splitSources(sources: SourceFreshness[]): {
  active: SourceFreshness[];
  retired: SourceFreshness[];
} {
  return {
    active: sources.filter((s) => !s.retired),
    retired: sources.filter((s) => s.retired),
  };
}

/** staleAfterDays is the age past which Now asks for a new export. It is a
 *  reminder and not a verdict: file-based ingestion means the data is always a
 *  little old, and two weeks is where "a little" usually stops (ADR 0049). */
export const staleAfterDays = 14;

/** needsExport says whether the Account's last datum is old enough to ask for a
 *  new export. An Account with no data is not stale, it is empty, and Now shows the
 *  import invitation instead. */
export function needsExport(lastDay: string | undefined, ageDays: number): boolean {
  return !!lastDay && ageDays >= staleAfterDays;
}
