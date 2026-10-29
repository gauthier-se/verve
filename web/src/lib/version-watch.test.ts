import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  RouterContextProvider,
} from "@tanstack/react-router";
import { renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, it } from "vitest";

import { useVersionWatch } from "@/lib/version-watch";
import { createAppRouter } from "@/router";
import { stubLocationAssign } from "@/test/location";

/**
 * Runs useVersionWatch inside a loaded application router, with
 * window.location.assign replaced by a spy.
 */
async function renderVersionWatch(version: string | undefined) {
  const router = createAppRouter({
    queryClient: new QueryClient(),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  await router.load();
  const assign = stubLocationAssign();

  const hook = renderHook(
    (props: { version: string | undefined }) => useVersionWatch(props.version),
    {
      initialProps: { version },
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(RouterContextProvider, { router, children }),
    },
  );

  return { ...hook, router, assign };
}

describe("useVersionWatch", () => {
  it("given the server keeps the version the app loaded with, when the app navigates, then the navigation stays client-side", async () => {
    const { result, rerender, router, assign } =
      await renderVersionWatch("1.0.0");

    rerender({ version: "1.0.0" });
    await router.navigate({ href: "/workouts/123" });

    expect(result.current).toBe(false);
    expect(assign).not.toHaveBeenCalled();
    expect(router.state.location.pathname).toBe("/workouts/123");
  });

  it("given the server reports another version, then the app is stale and the next navigation performs a full page load of the target URL", async () => {
    const { result, rerender, router, assign } =
      await renderVersionWatch("1.0.0");

    rerender({ version: "1.1.0" });

    expect(result.current).toBe(true);
    expect(assign).not.toHaveBeenCalled();
    await router.navigate({ href: "/workouts/123?tab=splits#map" });
    expect(assign).toHaveBeenCalledExactlyOnceWith(
      "/workouts/123?tab=splits#map",
    );
  });

  it("given the version is unknown until the first answer, when the first version arrives, then it is the version the app loaded with", async () => {
    const { result, rerender, router, assign } =
      await renderVersionWatch(undefined);

    rerender({ version: "1.0.0" });
    await router.navigate({ href: "/sleep" });

    expect(result.current).toBe(false);
    expect(assign).not.toHaveBeenCalled();

    rerender({ version: "1.1.0" });
    await router.navigate({ href: "/workouts" });

    expect(result.current).toBe(true);
    expect(assign).toHaveBeenCalledExactlyOnceWith("/workouts");
  });
});
