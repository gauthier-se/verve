import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { client } from "@/api/generated/client.gen";
import { problemResponse } from "@/test/problem";
import { server } from "@/test/server";

/** Registers a handler for `method` on /api/v1/echo that records the request it receives. */
function recordRequest(method: "get" | "post" | "delete") {
  const received: Request[] = [];
  server.use(
    http[method]("/api/v1/echo", ({ request }) => {
      received.push(request);
      return HttpResponse.json({});
    }),
  );
  return received;
}

describe("API client", () => {
  it("given the generated client, then it sends requests to the same origin with same-origin credentials", () => {
    expect(client.getConfig()).toMatchObject({
      baseUrl: window.location.origin,
      credentials: "same-origin",
    });
  });

  it("given a GET request, when the client sends it, then it has no CSRF header", async () => {
    const received = recordRequest("get");

    await client.get({ url: "/api/v1/echo" });

    expect(received).toHaveLength(1);
    expect(received[0]?.url).toBe(`${window.location.origin}/api/v1/echo`);
    expect(received[0]?.headers.has("Verve-Csrf")).toBe(false);
  });

  it("given a problem response from Verve, when the client throws, then it throws that problem", async () => {
    const problem = {
      title: "Service Unavailable",
      status: 503,
      code: "starting",
    };
    server.use(http.get("/api/v1/echo", () => problemResponse(problem)));

    await expect(
      client.get({ url: "/api/v1/echo", throwOnError: true }),
    ).rejects.toStrictEqual({ type: "about:blank", ...problem });
  });

  it.each([
    {
      response: "a text page",
      resolver: () =>
        new HttpResponse("Bad Gateway", {
          status: 502,
          statusText: "Bad Gateway",
          headers: { "Content-Type": "text/plain" },
        }),
      status: 502,
      title: "Bad Gateway",
    },
    {
      response: "an empty body",
      resolver: () =>
        new HttpResponse(null, { status: 504, statusText: "Gateway Timeout" }),
      status: 504,
      title: "Gateway Timeout",
    },
    {
      response: "JSON that is not a problem",
      resolver: () =>
        HttpResponse.json(
          { message: "upstream down" },
          { status: 500, statusText: "Internal Server Error" },
        ),
      status: 500,
      title: "Internal Server Error",
    },
  ])(
    "given an error response with $response, which does not come from Verve, when the client throws, then it throws a problem with the response status",
    async ({ resolver, status, title }) => {
      server.use(http.get("/api/v1/echo", resolver));

      await expect(
        client.get({ url: "/api/v1/echo", throwOnError: true }),
      ).rejects.toStrictEqual({
        type: "about:blank",
        title,
        status,
        code: "unexpected_response",
      });
    },
  );

  it.each(["post", "delete"] as const)(
    "given a %s request, when the client sends it, then it carries the header Verve-Csrf: 1",
    async (method) => {
      const received = recordRequest(method);

      await client[method]({ url: "/api/v1/echo" });

      expect(received).toHaveLength(1);
      expect(received[0]?.headers.get("Verve-Csrf")).toBe("1");
    },
  );
});
