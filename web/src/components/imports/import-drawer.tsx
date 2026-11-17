import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import type { ImportSummary } from "@/api/generated";
import { getImportOptions } from "@/api/generated/@tanstack/react-query.gen";
import { CancelImportDialog } from "@/components/imports/cancel-import-dialog";
import { ImportReport } from "@/components/imports/import-report";
import {
  activePollMs,
  formatImportDate,
  isActive,
  originLabel,
  statusLabel,
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
  const date = formatImportDate(
    summary.createdAt,
    i18n.language,
    timeZone,
    "medium",
  );

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
        {open && <ImportDetails id={summary.id} timeZone={timeZone} />}
        <SheetFooter className="flex-row flex-wrap justify-end">
          {isActive(summary.status) && (
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

/**
 * The whole report of the import `id`, once it is loaded, polled while the
 * import is queued or running.
 */
function ImportDetails({
  id,
  timeZone,
}: {
  id: number;
  timeZone: string | null;
}) {
  const { i18n } = useTranslation();
  const details = useQuery({
    ...getImportOptions({ path: { id } }),
    refetchInterval: (query) =>
      query.state.data && isActive(query.state.data.status)
        ? activePollMs
        : false,
  });

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
  return (
    <div className="px-4">
      <ImportReport report={details.data.report} timeZone={timeZone} />
    </div>
  );
}
