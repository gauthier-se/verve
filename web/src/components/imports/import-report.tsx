import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useId, useMemo } from "react";
import { useTranslation } from "react-i18next";

import type {
  Import,
  ImportCounters,
  ImportTypeCounters,
} from "@/api/generated";
import { listMetricsOptions } from "@/api/generated/@tanstack/react-query.gen";
import {
  counterLabel,
  counterNames,
  typeLabel,
} from "@/components/imports/labels";
import { formatDate, formatNumber } from "@/i18n/format";

interface ImportReportProps {
  report: Import["report"];
  /** The timezone of the user, in which the dates are shown. */
  timeZone: string | null;
}

/**
 * The report of an import: the total counters, the period of its records and
 * the routes of its workouts once it has them, the counters of each type,
 * the types with the most records first and the workouts last, and the
 * sample errors.
 */
export function ImportReport({ report, timeZone }: ImportReportProps) {
  const { t, i18n } = useTranslation();
  const metrics = useQuery(listMetricsOptions());
  const uncuratedIdentifiers = useMemo(
    () =>
      new Map(
        (metrics.data?.items ?? [])
          .filter((metric) => !metric.curated)
          .map((metric) => [metric.key, metric.identifier]),
      ),
    [metrics.data],
  );

  // The types with the most records first, then the workouts.
  const types = [
    ...Object.entries(report.types)
      .map(([key, counters]) => ({
        key,
        name: typeLabel(key, i18n, uncuratedIdentifiers),
        counters,
      }))
      .sort(
        (a, b) =>
          (b.counters.received ?? 0) - (a.counters.received ?? 0) ||
          a.name.localeCompare(b.name, i18n.language),
      ),
    ...(Object.keys(report.workouts).length > 0
      ? [
          {
            key: "workouts",
            name: t("imports.details.workouts"),
            counters: report.workouts,
          },
        ]
      : []),
  ];

  return (
    <div className="flex flex-col gap-6">
      <Section title={t("imports.details.totals")}>
        <Counters counters={report.total} />
      </Section>
      <Coverage report={report} timeZone={timeZone} />
      <Section title={t("imports.details.byType")}>
        {types.length === 0 ? (
          <p className="text-muted-foreground">
            {t("imports.details.noRecord")}
          </p>
        ) : (
          <ul className="flex flex-col gap-4">
            {types.map((type) => (
              <li key={type.key} className="flex flex-col gap-1">
                <h4 className="font-medium break-all">{type.name}</h4>
                <Counters counters={type.counters} />
              </li>
            ))}
          </ul>
        )}
      </Section>
      <Section title={t("imports.details.errors")}>
        {report.errors.length === 0 ? (
          <p className="text-muted-foreground">
            {t("imports.details.noError")}
          </p>
        ) : (
          <ul className="flex flex-col gap-2 font-mono text-xs break-words">
            {report.errors.map((message, i) => (
              <li key={i}>{message}</li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}

/**
 * The period that the records of an import cover and the number of routes
 * stored with its workouts, each once the report has it.
 */
function Coverage({ report, timeZone }: ImportReportProps) {
  const { t, i18n } = useTranslation();
  const day = (iso: string) =>
    formatDate(new Date(iso), i18n.language, {
      dateStyle: "medium",
      timeZone: timeZone ?? undefined,
    });
  const routes = report.routes ?? 0;

  return (
    <>
      {report.period && (
        <Section title={t("imports.details.period")}>
          <p>
            {t("imports.details.periodRange", {
              start: day(report.period.start),
              end: day(report.period.end),
            })}
          </p>
        </Section>
      )}
      {routes > 0 && (
        <Section title={t("imports.details.routes")}>
          <p>
            {t("imports.details.routesStored", {
              count: routes,
              routes: formatNumber(routes, i18n.language),
            })}
          </p>
        </Section>
      )}
    </>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const headingId = useId();

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2">
      <h3 id={headingId} className="font-heading font-medium">
        {title}
      </h3>
      {children}
    </section>
  );
}

/**
 * The counters given, in the order of the report: every counter of the
 * total, and only the counters that a type has.
 */
function Counters({
  counters,
}: {
  counters: ImportCounters | ImportTypeCounters;
}) {
  const { t, i18n } = useTranslation();

  return (
    <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1">
      {counterNames.map((name) => {
        const value = counters[name];
        return (
          value !== undefined && (
            <div key={name} className="contents">
              <dt className="text-muted-foreground">{counterLabel(t, name)}</dt>
              <dd className="text-right tabular-nums">
                {formatNumber(value, i18n.language)}
              </dd>
            </div>
          )
        );
      })}
    </dl>
  );
}
