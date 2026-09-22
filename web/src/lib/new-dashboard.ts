// The draft behind the "New dashboard" dialog: an empty grid or a Dashboard
// template (ADR 0047), and a name that follows the pick until the Account types
// its own.

import type { DashboardTemplate } from "./types";

/** Draft is what the dialog holds: the picked template's slug (null for an empty
 *  grid), the name, and whether that name was typed rather than filled by a pick. */
export interface Draft {
  template: string | null;
  name: string;
  typed: boolean;
}

export const emptyDraft: Draft = { template: null, name: "", typed: false };

/** pick selects a template, or an empty grid for null. A name the Account typed is
 *  kept; a name a pick filled follows the new pick. */
export function pick(draft: Draft, template: DashboardTemplate | null): Draft {
  const name = draft.typed ? draft.name : (template?.name ?? "");
  return { template: template?.slug ?? null, name, typed: draft.typed };
}

/** typeName records a name typed by hand. Clearing it hands the name back to the
 *  pick, so an emptied field does not pin an empty name. */
export function typeName(draft: Draft, name: string): Draft {
  return { ...draft, name, typed: name !== "" };
}

/** createBody is the POST /v1/dashboards body for a draft. */
export function createBody(draft: Draft): { name: string; template?: string } {
  const name = draft.name.trim();
  return draft.template ? { template: draft.template, name } : { name };
}

/** coverageText states how many of a template's Metrics this Account holds data
 *  for, always with its denominator: a fact beside the name, never a verdict. */
export function coverageText(t: DashboardTemplate): string {
  const total = t.metrics.length;
  const noun = total === 1 ? "metric" : "metrics";
  if (t.with_data === 0) return `No data yet for its ${total} ${noun}`;
  return `${t.with_data} of ${total} ${noun} ${total === 1 ? "has" : "have"} data`;
}
