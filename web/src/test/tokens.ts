import { http, HttpResponse } from "msw";

import type {
  ApiToken,
  CreatedApiToken,
  CreateTokenInputBody,
} from "@/api/generated";
import { problemResponse } from "@/test/problem";
import { server } from "@/test/server";

/** The path of the API tokens of the user of the session. */
export const tokensPath = "/api/v1/account/tokens";

/** The synthetic token that `fakeTokens` answers to a creation. */
export const createdSecret = "vrv_3fK9Qm7Lp2Xw8Rt4Vy6Bn1Cz5Dh0Gj3Ks9Ue2Fa7Ho4";

/** An ingest token used by Health Auto Export, without expiry. */
export const iPhoneToken: ApiToken = {
  id: 2,
  name: "iPhone",
  scopes: ["ingest"],
  prefix: "vrv_7Tq2",
  createdAt: "2026-10-01T09:00:00Z",
  lastUsedAt: "2026-11-10T08:30:00Z",
  lastUsedIp: "192.0.2.10",
};

/** A read token that was never used and expires. */
export const dashboardToken: ApiToken = {
  id: 1,
  name: "Home Assistant",
  scopes: ["read"],
  prefix: "vrv_8Hq2",
  createdAt: "2026-09-15T10:00:00Z",
  expiresAt: "2027-09-15T10:00:00Z",
};

/**
 * Fakes the token endpoints of the user of the session, starting with
 * `initial`, newest first, as the server lists them. A creation answers the
 * new token with `createdSecret` and lists it first, and a revocation marks
 * the token revoked, at the current time. Returns the bodies of the creations
 * and the ids of the revocations it receives.
 */
export function fakeTokens(initial: ApiToken[] = []) {
  let tokens = [...initial];
  let nextId = Math.max(0, ...tokens.map((token) => token.id)) + 1;
  const created: CreateTokenInputBody[] = [];
  const revoked: number[] = [];

  server.use(
    http.get(tokensPath, () => HttpResponse.json({ items: tokens })),
    http.post<never, CreateTokenInputBody>(tokensPath, async ({ request }) => {
      const body = await request.json();
      created.push(body);
      const token: ApiToken = {
        id: nextId++,
        name: body.name,
        scopes: body.scopes,
        prefix: createdSecret.slice(0, 8),
        createdAt: new Date().toISOString(),
      };
      tokens = [token, ...tokens];
      const answer: CreatedApiToken = { ...token, token: createdSecret };
      return HttpResponse.json(answer, { status: 201 });
    }),
    http.delete<{ id: string }>(`${tokensPath}/:id`, ({ params }) => {
      const id = Number(params.id);
      if (!tokens.some((token) => token.id === id)) {
        return problemResponse({
          title: "Not Found",
          status: 404,
          code: "not_found",
        });
      }
      revoked.push(id);
      tokens = tokens.map((token) =>
        token.id === id && token.revokedAt === undefined
          ? { ...token, revokedAt: new Date().toISOString() }
          : token,
      );
      return new HttpResponse(null, { status: 204 });
    }),
  );

  return { created, revoked };
}
