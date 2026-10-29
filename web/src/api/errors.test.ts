import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { classifyApiError } from "@/api/errors";
import { getMeta } from "@/api/generated";
import { problemResponse } from "@/test/problem";
import { server } from "@/test/server";

/** Returns what the generated client throws when GET /api/v1/meta gets `response`. */
async function errorOfGetMeta(response: () => Response): Promise<unknown> {
  server.use(http.get("/api/v1/meta", response));
  return getMeta({ throwOnError: true }).then(
    () => {
      throw new Error("GET /api/v1/meta succeeded");
    },
    (error: unknown) => error,
  );
}

describe("classifyApiError", () => {
  it.each([
    {
      response: "a network error",
      resolver: () => HttpResponse.error(),
      kind: "unreachable",
    },
    {
      response: "a 502 text page from a reverse proxy",
      resolver: () =>
        new HttpResponse("Bad Gateway", {
          status: 502,
          headers: { "Content-Type": "text/plain" },
        }),
      kind: "unreachable",
    },
    {
      response: "a 502 HTML page from a reverse proxy",
      resolver: () =>
        HttpResponse.html("<h1>502 Bad Gateway</h1>", { status: 502 }),
      kind: "unreachable",
    },
    {
      response: "an empty 504 from a reverse proxy",
      resolver: () => new HttpResponse(null, { status: 504 }),
      kind: "unreachable",
    },
    {
      response: "a 503 page from a reverse proxy whose backend is down",
      resolver: () =>
        new HttpResponse("no available server", {
          status: 503,
          headers: { "Content-Type": "text/plain" },
        }),
      kind: "unreachable",
    },
    {
      response: 'a 503 problem with code "starting"',
      resolver: () =>
        problemResponse({
          title: "Service Unavailable",
          status: 503,
          code: "starting",
        }),
      kind: "starting",
    },
    {
      response: 'a 503 problem with code "unavailable"',
      resolver: () =>
        problemResponse({
          title: "Service Unavailable",
          status: 503,
          code: "unavailable",
        }),
      kind: "other",
    },
    {
      response: "a 500 problem",
      resolver: () =>
        problemResponse({
          title: "Internal Server Error",
          status: 500,
          code: "internal",
        }),
      kind: "other",
    },
    {
      response: "a 404 problem",
      resolver: () =>
        problemResponse({ title: "Not Found", status: 404, code: "not_found" }),
      kind: "other",
    },
    {
      response: "a 500 text page from a reverse proxy",
      resolver: () =>
        new HttpResponse("Internal Server Error", {
          status: 500,
          headers: { "Content-Type": "text/plain" },
        }),
      kind: "other",
    },
  ])(
    "given the client gets $response, when classifyApiError classifies the error, then it is $kind",
    async ({ resolver, kind }) => {
      const error = await errorOfGetMeta(resolver);

      expect(classifyApiError(error)).toBe(kind);
    },
  );

  it.each([new Error("boom"), "boom", undefined, null, {}])(
    "given %o, which the client does not throw for a response, then it is other",
    (error) => {
      expect(classifyApiError(error)).toBe("other");
    },
  );
});
