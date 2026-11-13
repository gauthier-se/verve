import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import type {
  Import,
  ImportCounters,
  ImportSummary,
  ImportTypeCounters,
} from "@/api/generated";
import {
  getImportOptions,
  listMetricsOptions,
} from "@/api/generated/@tanstack/react-query.gen";
import { CancelImportDialog } from "@/components/imports/cancel-import-dialog";
import {
  counterLabel,
  counterNames,
  formatImportDate,
  isCancellable,
  originLabel,
  statusLabel,
  typeLabel,
} from "@/components/imports/labels";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { formatNumber } from "@/i18n/format";

interface ImportDrawerProps {
  summary: ImportSummary;
  /** The timezone of the user, in which the dates are shown. */
  timeZone: string | null;
}

/**
 * The details action of an import, which opens a drawer with its whole
 * report: the total counters, the counters of each type and of the workouts,
 * and the sample errors. A queued or running import can be cancelled there.
 */
export function ImportDrawer({ summary, timeZone }: ImportDrawerProps) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const date = formatImportDate(summary.createdAt, i18n.language, timeZone);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button
          variant="outline"
          size="lg"
          className="min-h-11"
          aria-label={t("imports.details.label", { date })}
        >
          {t("imports.details.action")}
        </Button>
      </SheetTrigger>
      <SheetContent className="w-full overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{t("imports.details.title", { date })}</SheetTitle>
          <SheetDescription>
            {t("imports.details.description", {
              origin: originLabel(t, summary.origin),
              status: statusLabel(t, summary.status),
            })}
          </SheetDescription>
        </SheetHeader>
        {open && <ImportDetails id={summary.id} />}
        <SheetFooter className="flex-row flex-wrap justify-end">
          {isCancellable(summary.status) && (
            <CancelImportDialog importId={summary.id} />
          )}
          <SheetClose asChild>
            <Button variant="outline" size="lg" className="min-h-11">
              {t("imports.details.close")}
            </Button>
          </SheetClose>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

/** The whole report of the import `id`, once it is loaded. */
function ImportDetails({ id }: { id: number }) {
  const { i18n } = useTranslation();
  const details = useQuery(getImportOptions({ path: { id } }));

  if (details.isError) {
    return (
      <p role="alert" className="px-4 text-sm text-destructive">
        {errorMessage(details.error, i18n)}
      </p>
    );
  }
  if (details.data === undefined) {
    return null;
  }
  return <Report report={details.data.report} />;
}

function Report({ report }: { report: Import["report"] }) {
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
  const types = Object.entries(report.types)
    .map(([key, counters]) => ({
      key,
      name: typeLabel(key, i18n, uncuratedIdentifiers),
      counters,
    }))
    .sort(
      (a, b) =>
        (b.counters.received ?? 0) - (a.counters.received ?? 0) ||
        a.name.localeCompare(b.name, i18n.language),
    );
  if (Object.keys(report.workouts).length > 0) {
    types.push({
      key: "workouts",
      name: t("imports.details.workouts"),
      counters: report.workouts,
    });
  }

  return (
    <div className="flex flex-col gap-6 px-4">
      <Section title={t("imports.details.totals")}>
        <Counters counters={report.total} />
      </Section>
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
