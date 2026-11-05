import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

import type { CurrentUser, LoginInputBody, Meta } from "@/api/generated";
import { problemResponse } from "@/test/problem";

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

/** A member account, with an email address. */
export const member: CurrentUser = {
  id: 2,
  username: "alice",
  email: "alice@example.com",
  role: "member",
};

/** An admin account, without an email address. */
export const admin: CurrentUser = { id: 1, username: "admin", role: "admin" };

/** The password of the account of `fakeAuth`. */
export const fakePassword = "correct horse battery staple";

/** Answers GET `path` with 401, as the server does once the session ended. */
export function unauthorized(path: string) {
  return http.get(path, () =>
    problemResponse({
      title: "Unauthorized",
      status: 401,
      code: "unauthorized",
    }),
  );
}

interface FakeAuthOptions {
  /** The only account of the instance. Without one, setup is required. */
  user?: CurrentUser;
  /** Whether the browser starts with a session of the account. */
  signedIn?: boolean;
}

/**
 * Handlers that fake the authentication endpoints for one browser, to
 * register with `server.use(...fakeAuth())`. GET /api/v1/auth/me answers the
 * account while the browser has a session and 401 otherwise. A login with the
 * username or email of the account and `fakePassword` opens the session, any
 * other login answers 401 invalid_credentials, and a logout ends it.
 */
export function fakeAuth({ user, signedIn = false }: FakeAuthOptions = {}) {
  let sessionUser = signedIn ? user : undefined;

  return [
    http.get("/api/v1/setup", () =>
      HttpResponse.json({ required: user === undefined }),
    ),
    http.get("/api/v1/auth/me", () =>
      sessionUser
        ? HttpResponse.json(sessionUser)
        : problemResponse({
            title: "Unauthorized",
            status: 401,
            code: "unauthorized",
          }),
    ),
    http.post<never, LoginInputBody>(
      "/api/v1/auth/login",
      async ({ request }) => {
        const body = await request.json();
        const known =
          user !== undefined &&
          (body.login === user.username || body.login === user.email);
        if (!known || body.password !== fakePassword) {
          return problemResponse({
            title: "Unauthorized",
            status: 401,
            code: "invalid_credentials",
          });
        }
        sessionUser = user;
        return HttpResponse.json(user);
      },
    ),
    http.post("/api/v1/auth/logout", () => {
      sessionUser = undefined;
      return new HttpResponse(null, { status: 204 });
    }),
  ];
}
