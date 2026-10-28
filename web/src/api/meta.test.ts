import { QueryClient } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, expectTypeOf, it } from "vitest";

import type { Meta } from "@/api/generated";
import { getMetaOptions } from "@/api/generated/@tanstack/react-query.gen";
import { server } from "@/test/server";

describe("getMetaOptions", () => {
  it("given the API answers GET /api/v1/meta, when the query client fetches getMetaOptions(), then it returns the typed build metadata", async () => {
    const meta: Meta = {
      name: "Verve",
      version: "1.2.3",
      commit: "abc1234",
      buildDate: "2026-10-28T10:00:00Z",
    };
    server.use(http.get("/api/v1/meta", () => HttpResponse.json(meta)));

    const data = await new QueryClient().fetchQuery(getMetaOptions());

    expectTypeOf(data).toEqualTypeOf<Meta>();
    expect(data).toEqual(meta);
  });
});
