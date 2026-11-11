import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useId } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import { listTokensOptions } from "@/api/generated/@tanstack/react-query.gen";
import { CreateTokenDialog } from "@/components/tokens/create-token-dialog";
import { TokensTable } from "@/components/tokens/tokens-table";

export const Route = createFileRoute("/_authenticated/settings/tokens")({
  component: Tokens,
});

/**
 * The API tokens of the user of the session: a table of them, or what tokens
 * are for while there is none, and the dialog that creates one.
 */
function Tokens() {
  const { t, i18n } = useTranslation();
  const { user } = Route.useRouteContext();
  const headingId = useId();
  const tokens = useQuery(listTokensOptions());

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-col gap-2">
          <h1 id={headingId} className="font-heading text-2xl font-semibold">
            {t("tokens.title")}
          </h1>
          <p className="text-muted-foreground">{t("tokens.description")}</p>
        </div>
        <CreateTokenDialog />
      </div>
      {tokens.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(tokens.error, i18n)}
        </p>
      )}
      {tokens.data &&
        (tokens.data.items.length === 0 ? (
          <EmptyState />
        ) : (
          <TokensTable
            tokens={tokens.data.items}
            timeZone={user.timezone}
            aria-labelledby={headingId}
          />
        ))}
    </div>
  );
}

function EmptyState() {
  const { t } = useTranslation();
  const headingId = useId();

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-2 rounded-xl border border-dashed p-6"
    >
      <h2 id={headingId} className="font-heading text-lg font-medium">
        {t("tokens.empty.title")}
      </h2>
      <p className="text-muted-foreground">{t("tokens.empty.description")}</p>
    </section>
  );
}
