import type { i18n, TFunction } from "i18next";

import type { ImportCounters, ImportSummary } from "@/api/generated";
import type Resources from "@/i18n/resources";
import { formatDate } from "@/i18n/format";

/** The counters of an import report, in the order in which they are shown. */
export const counterNames = [
  "received",
  "inserted",
  "updated",
  "duplicates",
  "blocked",
  "invalid",
  "unsupported",
  "suspicious",
  "warnings",
  "deleted",
  "notFound",
] as const satisfies readonly (keyof ImportCounters)[];

/** The name of where the data of an import comes from. */
export function originLabel(t: TFunction, origin: ImportSummary["origin"]) {
  switch (origin) {
    case "xml":
      return t("imports.origin.xml");
    case "hae":
      return t("imports.origin.hae");
    case "native":
      return t("imports.origin.native");
    case "cli":
      return t("imports.origin.cli");
  }
}

/** The name of the status of an import. */
export function statusLabel(t: TFunction, status: ImportSummary["status"]) {
  switch (status) {
    case "queued":
      return t("imports.status.queued");
    case "running":
      return t("imports.status.running");
    case "completed":
      return t("imports.status.completed");
    case "failed":
      return t("imports.status.failed");
    case "cancelled":
      return t("imports.status.cancelled");
  }
}

/** The name of a counter of an import report. */
export function counterLabel(
  t: TFunction,
  counter: (typeof counterNames)[number],
) {
  switch (counter) {
    case "received":
      return t("imports.counters.received");
    case "inserted":
      return t("imports.counters.inserted");
    case "updated":
      return t("imports.counters.updated");
    case "duplicates":
      return t("imports.counters.duplicates");
    case "blocked":
      return t("imports.counters.blocked");
    case "invalid":
      return t("imports.counters.invalid");
    case "unsupported":
      return t("imports.counters.unsupported");
    case "suspicious":
      return t("imports.counters.suspicious");
    case "warnings":
      return t("imports.counters.warnings");
    case "deleted":
      return t("imports.counters.deleted");
    case "notFound":
      return t("imports.counters.notFound");
  }
}

/** How often the views of an import poll it while it is queued or running. */
export const activePollMs = 5_000;

/**
 * Whether an import is queued or running: it can still be cancelled, and
 * its status and counters still change.
 */
export function isActive(status: ImportSummary["status"]) {
  return status === "queued" || status === "running";
}

/**
 * The creation time of an import, in the timezone of the user: to the
 * minute in the table, and to the second in the names of its drawer, so that
 * two imports of the same minute differ.
 */
export function formatImportDate(
  iso: string,
  lng: string,
  timeZone: string | null,
  timeStyle: "short" | "medium" = "short",
) {
  return formatDate(new Date(iso), lng, {
    dateStyle: "medium",
    timeStyle,
    timeZone: timeZone ?? undefined,
  });
}

type MetricKey = keyof Resources["metrics"];

/**
 * The name of the type key of an import report: the translated name of a
 * metric type, the identifier of an uncurated type, or the key itself, which
 * is the identifier as received for an unsupported type.
 */
export function typeLabel(
  key: string,
  i18n: i18n,
  uncuratedIdentifiers: ReadonlyMap<string, string>,
) {
  const identifier = uncuratedIdentifiers.get(key);
  if (identifier !== undefined) {
    return identifier;
  }
  return isTranslatedMetric(key, i18n)
    ? i18n.t(`${key}.name`, { ns: "metrics" })
    : key;
}

function isTranslatedMetric(key: string, i18n: i18n): key is MetricKey {
  return i18n.exists(`${key}.name`, { ns: "metrics" });
}
