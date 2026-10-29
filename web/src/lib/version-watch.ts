import { useRouter } from "@tanstack/react-router";
import { useEffect, useState } from "react";

/**
 * Watches the version of the server, undefined until it first answers. Once
 * it differs from the version the app loaded with, the app is stale: the next
 * client-side navigation becomes a full page load of its target, which loads
 * the frontend of the new version. Returns whether the app is stale.
 */
export function useVersionWatch(version: string | undefined): boolean {
  const router = useRouter();
  const [loadedVersion, setLoadedVersion] = useState(version);
  if (loadedVersion === undefined && version !== undefined) {
    setLoadedVersion(version);
  }
  const stale =
    loadedVersion !== undefined &&
    version !== undefined &&
    version !== loadedVersion;

  useEffect(() => {
    if (!stale) {
      return;
    }
    return router.subscribe("onBeforeNavigate", ({ toLocation }) => {
      window.location.assign(toLocation.href);
    });
  }, [router, stale]);

  return stale;
}
