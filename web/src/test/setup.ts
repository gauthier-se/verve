import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";

import { server } from "@/test/server";

// jsdom does not implement scrolling, and TanStack Router resets the scroll
// position on every navigation.
window.scrollTo = () => undefined;

beforeAll(() => {
  // Fail on any request without a handler. MSW 3 renamed MSW 2's
  // `onUnhandledRequest` option to `onUnhandledFrame`.
  server.listen({ onUnhandledFrame: "error" });
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});
