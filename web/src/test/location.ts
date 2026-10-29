import { onTestFinished, vi } from "vitest";

/**
 * Replaces window.location for the current test with a copy of its URL whose
 * `assign` is a spy, since jsdom does not let tests redefine Location methods.
 */
export function stubLocationAssign() {
  const assign = vi.fn<(url: string) => void>();
  vi.stubGlobal(
    "location",
    Object.assign(new URL(window.location.href), { assign }),
  );
  onTestFinished(() => {
    vi.unstubAllGlobals();
  });
  return assign;
}
