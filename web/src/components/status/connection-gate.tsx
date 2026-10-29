import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { classifyApiError } from "@/api/errors";
import { metaQueryOptions, versionCheckInterval } from "@/api/meta";
import { Button } from "@/components/ui/button";

/** How often the gate retries while the server is unreachable or starting. */
const retryInterval = 5_000;

/**
 * Replaces its children with a full-page screen while the server is
 * unreachable or starting, and retries every 5 seconds until the server
 * answers. Other errors let the children through.
 */
export function ConnectionGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const metaQuery = useQuery({
    ...metaQueryOptions(),
    refetchInterval: (query) =>
      connectionFailure(query.state.error)
        ? retryInterval
        : versionCheckInterval,
  });

  switch (connectionFailure(metaQuery.error)) {
    case "unreachable":
      return (
        <ConnectionScreen
          title={t("connection.unreachable.title")}
          description={t("connection.unreachable.description")}
        >
          <Button
            size="lg"
            className="min-h-11 px-4"
            onClick={() => {
              void metaQuery.refetch();
            }}
          >
            {t("connection.unreachable.retry")}
          </Button>
        </ConnectionScreen>
      );
    case "starting":
      return (
        <ConnectionScreen
          title={t("connection.starting.title")}
          description={t("connection.starting.description")}
        />
      );
    case undefined:
      return children;
  }
}

/** The connection failure behind an error of the meta query, if any. */
function connectionFailure(error: unknown) {
  const kind = classifyApiError(error);
  return kind === "other" ? undefined : kind;
}

interface ConnectionScreenProps {
  title: string;
  description: string;
  /** The actions offered below the announcement. */
  children?: ReactNode;
}

/** A full-page screen announcing the state of the connection to the server. */
function ConnectionScreen({
  title,
  description,
  children,
}: ConnectionScreenProps) {
  return (
    <main className="mx-auto flex min-h-svh w-full max-w-md flex-col items-center justify-center gap-6 px-4 text-center">
      <div role="status" aria-live="polite" className="flex flex-col gap-2">
        <h1 className="font-heading text-2xl font-semibold">{title}</h1>
        <p className="text-muted-foreground">{description}</p>
      </div>
      {children}
    </main>
  );
}
