import { queryOptions } from "@tanstack/react-query";

import { getMetaOptions } from "@/api/generated/@tanstack/react-query.gen";

/** How often the app refetches the server metadata to notice an upgrade. */
export const versionCheckInterval = 5 * 60_000;

/**
 * The query of the server metadata shared by the app shell. It is refetched
 * on window focus and every 5 minutes, so that the app notices a server
 * upgrade, and never retried: the connection gate drives the retries. It runs
 * even when the browser reports no network, so that a failure reaches the
 * gate instead of pausing the query.
 */
export function metaQueryOptions() {
  return queryOptions({
    ...getMetaOptions(),
    networkMode: "always",
    retry: false,
    refetchOnWindowFocus: true,
    refetchInterval: versionCheckInterval,
  });
}
