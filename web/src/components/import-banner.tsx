import { useQuery } from "@tanstack/react-query";
import { Link, useMatch } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { listImportsOptions } from "@/api/generated/@tanstack/react-query.gen";
import { activePollMs } from "@/components/imports/labels";
import { uploadRatio, useUpload } from "@/components/imports/upload-provider";
import { formatNumber, formatPercent } from "@/i18n/format";

/** How often the banner polls while no import runs. */
const idlePollMs = 60_000;

/**
 * A banner while an import of the user of the session runs, with the number
 * of records it received so far once there is one, the progress of an XML
 * import as a percentage, and a link to the imports page. It polls the
 * running imports every 5 seconds while one runs, and every minute
 * otherwise. Screen readers announce that an import runs, from a live region
 * that stays on the page so that its change is announced, but not the count,
 * which changes at every poll. Outside the upload page, the banner also
 * shows the percentage of a running upload, with a link to the upload page.
 */
export function ImportBanner() {
  const { t, i18n } = useTranslation();
  const running = useQuery({
    ...listImportsOptions({ query: { status: "running", limit: 1 } }),
    refetchInterval: (query) =>
      query.state.data?.items.length ? activePollMs : idlePollMs,
  });
  const { state: upload } = useUpload();
  const onUploadPage =
    useMatch({ from: "/_authenticated/imports_/xml", shouldThrow: false }) !==
    undefined;

  const runningImport = running.data?.items[0];
  const uploadText =
    upload.status === "uploading" && !onUploadPage
      ? t("upload.progress", {
          fileName: upload.fileName,
          percent: formatPercent(uploadRatio(upload), i18n.language),
        })
      : undefined;
  const received = runningImport?.report.total.received ?? 0;

  return (
    <>
      <p role="status" className="sr-only">
        {runningImport && t("importBanner.running")}
      </p>
      {(runningImport ?? uploadText) && (
        <section
          aria-label={t("importBanner.label")}
          className="mb-6 flex flex-col gap-y-1 rounded-xl border bg-muted px-4 py-2 text-sm"
        >
          {uploadText !== undefined && (
            <Row to="/imports/xml" label={t("importBanner.viewUpload")}>
              {uploadText}
            </Row>
          )}
          {runningImport && (
            <Row to="/imports" label={t("importBanner.view")}>
              {t("importBanner.running")}
              {received > 0 &&
                ` ${t("importBanner.received", {
                  count: received,
                  received: formatNumber(received, i18n.language),
                })}`}
              {runningImport.progress !== undefined &&
                ` ${t("importBanner.progress", {
                  percent: formatPercent(runningImport.progress, i18n.language),
                })}`}
            </Row>
          )}
        </section>
      )}
    </>
  );
}

/** A line of the banner, with the link to its page. */
function Row({
  to,
  label,
  children,
}: {
  to: "/imports" | "/imports/xml";
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4">
      <p>{children}</p>
      <Link
        to={to}
        className="inline-flex min-h-11 items-center font-medium underline underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {label}
      </Link>
    </div>
  );
}
