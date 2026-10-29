import { type QueryClient, useQuery } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  Link,
  Outlet,
} from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { metaQueryOptions } from "@/api/meta";
import { AppLayout } from "@/components/layout/app-layout";
import { ConnectionGate } from "@/components/status/connection-gate";
import { Button } from "@/components/ui/button";
import { useVersionWatch } from "@/lib/version-watch";

export interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Root,
  notFoundComponent: NotFound,
});

function Root() {
  const { data: meta } = useQuery(metaQueryOptions());
  useVersionWatch(meta?.version);

  return (
    <ConnectionGate>
      <AppLayout>
        <Outlet />
      </AppLayout>
    </ConnectionGate>
  );
}

function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start gap-4">
      <h1 className="font-heading text-2xl font-semibold">
        {t("notFound.title")}
      </h1>
      <p className="text-muted-foreground">{t("notFound.description")}</p>
      <Button asChild variant="outline" size="lg" className="min-h-11 px-4">
        <Link to="/">{t("notFound.home")}</Link>
      </Button>
    </div>
  );
}
