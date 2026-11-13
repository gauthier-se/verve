import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useId } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import { listImportsInfiniteOptions } from "@/api/generated/@tanstack/react-query.gen";
import { ImportsTable } from "@/components/imports/imports-table";
import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/_authenticated/imports")({
  component: Imports,
});

/**
 * The imports of the user of the session, newest first: a table of them
 * whose next pages load on demand, or how to send data while there is none.
 */
function Imports() {
  const { t, i18n } = useTranslation();
  const { user } = Route.useRouteContext();
  const headingId = useId();
  const imports = useInfiniteQuery({
    ...listImportsInfiniteOptions(),
    // The first page, without a cursor.
    initialPageParam: {},
    getNextPageParam: (page) => page.nextCursor,
  });
  const items = imports.data?.pages.flatMap((page) => page.items);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 id={headingId} className="font-heading text-2xl font-semibold">
          {t("imports.title")}
        </h1>
        <p className="text-muted-foreground">{t("imports.description")}</p>
      </div>
      {imports.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(imports.error, i18n)}
        </p>
      )}
      {items &&
        (items.length === 0 ? (
          <EmptyState />
        ) : (
          <ImportsTable
            imports={items}
            timeZone={user.timezone}
            aria-labelledby={headingId}
          />
        ))}
      {imports.hasNextPage && (
        <Button
          variant="outline"
          size="lg"
          className="min-h-11 self-center"
          disabled={imports.isFetchingNextPage}
          onClick={() => {
            void imports.fetchNextPage();
          }}
        >
          {t("imports.more")}
        </Button>
      )}
    </div>
  );
}

/** How to send data, in place of the table while the user has no import. */
function EmptyState() {
  const { t } = useTranslation();
  const headingId = useId();

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col items-start gap-2 rounded-xl border border-dashed p-6"
    >
      <h2 id={headingId} className="font-heading text-lg font-medium">
        {t("imports.empty.title")}
      </h2>
      <p className="text-muted-foreground">{t("imports.empty.description")}</p>
      <Button
        asChild
        variant="outline"
        size="lg"
        className="mt-2 min-h-11 px-4"
      >
        <Link to="/settings/tokens">{t("imports.empty.tokens")}</Link>
      </Button>
    </section>
  );
}
