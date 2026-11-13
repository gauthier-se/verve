import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { listImportsOptions } from "@/api/generated/@tanstack/react-query.gen";
import { activePollMs } from "@/components/imports/labels";
import { formatNumber } from "@/i18n/format";

/** How often the banner polls while no import runs. */
const idlePollMs = 60_000;

/**
 * A banner while an import of the user of the session runs, with the number
 * of records it received so far once there is one, and a link to the imports
 * page. It polls the running imports every 5 seconds while one runs, and
 * every minute otherwise. Screen readers announce that an import runs, from
 * a live region that stays on the page so that its change is announced, but
 * not the count, which changes at every poll.
 */
export function ImportBanner() {
  const { t } = useTranslation();
  const running = useQuery({
    ...listImportsOptions({ query: { status: "running", limit: 1 } }),
    refetchInterval: (query) =>
      query.state.data?.items.length ? activePollMs : idlePollMs,
  });

  const runningImport = running.data?.items[0];

  return (
    <>
      <p role="status" className="sr-only">
        {runningImport && t("importBanner.running")}
      </p>
      {runningImport && (
        <Banner received={runningImport.report.total.received} />
      )}
    </>
  );
}

/** The visible banner, with the number of records received once known. */
function Banner({ received }: { received: number }) {
  const { t, i18n } = useTranslation();

  return (
    <section
      aria-label={t("importBanner.label")}
      className="mb-6 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-xl border bg-muted px-4 py-2 text-sm"
    >
      <p>
        {t("importBanner.running")}
        {received > 0 &&
          ` ${t("importBanner.received", {
            count: received,
            received: formatNumber(received, i18n.language),
          })}`}
      </p>
      <Link
        to="/imports"
        className="inline-flex min-h-11 items-center font-medium underline underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {t("importBanner.view")}
      </Link>
    </section>
  );
}
