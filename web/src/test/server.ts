import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

import type { Meta } from "@/api/generated";

/** The server metadata answered by default. */
export const meta: Meta = {
  name: "Verve",
  version: "1.0.0",
  commit: "abc1234",
  buildDate: "2026-10-28T10:00:00Z",
};

/**
 * The app shell queries GET /api/v1/meta on every page, so the server answers
 * it by default. Tests register the other handlers they need, or replace this
 * one, with `server.use()`.
 */
export const server = setupServer(
  http.get("/api/v1/meta", () => HttpResponse.json(meta)),
);
