import { screen } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { admin, fakeAuth, password, server } from "@/test/server";

/** The login page, with `redirect` as the target after login. */
function loginPath(redirect: string) {
  return `/login?${new URLSearchParams({ redirect }).toString()}`;
}

const english = {
  login: "Username or email",
  password: "Password",
  submit: "Log in",
};

/** Fills in the login form with the labels of `labels` and submits it. */
async function logIn(
  user: UserEvent,
  credentials: { login: string; password: string },
  labels = english,
) {
  await user.type(
    await screen.findByLabelText(labels.login),
    credentials.login,
  );
  await user.type(screen.getByLabelText(labels.password), credentials.password);
  await user.click(screen.getByRole("button", { name: labels.submit }));
}

/** Answers POST /api/v1/auth/login with 429 rate_limited and its wait. */
function rateLimited(retryAfterSeconds: number) {
  return http.post("/api/v1/auth/login", () => {
    const response = problemResponse({
      title: "Too Many Requests",
      status: 429,
      code: "rate_limited",
    });
    response.headers.set("Retry-After", String(retryAfterSeconds));
    return response;
  });
}

describe("login page", () => {
  it("given the login page, then both fields have labels and the password is masked", async () => {
    renderRoute("/login");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Username or email" }),
    ).toHaveAttribute("autocomplete", "username");
    const passwordField = screen.getByLabelText("Password");
    expect(passwordField).toHaveAttribute("type", "password");
    expect(passwordField).toHaveAttribute("autocomplete", "current-password");
  });

  it.each([
    { target: "/admin", heading: "Administration" },
    { target: "/?period=week", heading: "Verve" },
  ])(
    "given the redirect target $target, when the account logs in, then the app goes to $target",
    async ({ target, heading }) => {
      const user = userEvent.setup();
      server.use(...fakeAuth({ user: admin }));
      const { router } = renderRoute(loginPath(target));

      await logIn(user, { login: "admin", password });

      expect(
        await screen.findByRole("heading", { level: 1, name: heading }),
      ).toBeInTheDocument();
      expect(router.state.location.href).toBe(target);
    },
  );

  it("given no redirect target, when the account logs in, then the app goes to the home page", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: admin }));
    const { router } = renderRoute("/login");

    await logIn(user, { login: "admin", password });

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(router.state.location.href).toBe("/");
  });

  it.each([
    "https://evil.example/",
    "//evil.example/",
    "/\\evil.example/",
    "javascript:alert(1)",
  ])(
    "given the redirect target %s, which is not a path of the app, when the account logs in, then the app goes to the home page",
    async (target) => {
      const user = userEvent.setup();
      server.use(...fakeAuth({ user: admin }));
      const { router } = renderRoute(loginPath(target));

      await logIn(user, { login: "admin", password });

      expect(
        await screen.findByRole("heading", { level: 1, name: "Verve" }),
      ).toBeInTheDocument();
      expect(router.state.location.href).toBe("/");
    },
  );

  it("given data cached for the account of a session that ended, when an account logs in, then none of it is kept", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: admin }));
    const { queryClient } = renderRoute("/login");
    const leftOver = ["data of the previous account"];
    queryClient.setQueryData(leftOver, "health data");

    await logIn(user, { login: "admin", password });

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(queryClient.getQueryData(leftOver)).toBeUndefined();
  });

  it("given the login page, when the user fills it in with the keyboard and presses Enter, then the account logs in", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: admin }));
    renderRoute("/login");
    const loginField = await screen.findByLabelText("Username or email");

    // The product name in the header comes first.
    await user.tab();
    await user.tab();
    expect(loginField).toHaveFocus();
    await user.keyboard("admin");
    await user.tab();
    expect(screen.getByLabelText("Password")).toHaveFocus();
    await user.keyboard(`${password}{Enter}`);

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
  });

  it("given empty fields, when the form is submitted, then each field says that it is required and nothing is sent", async () => {
    const user = userEvent.setup();
    const requests: Request[] = [];
    server.use(
      http.post("/api/v1/auth/login", ({ request }) => {
        requests.push(request);
        return HttpResponse.json(admin);
      }),
    );
    renderRoute("/login");

    await user.click(await screen.findByRole("button", { name: "Log in" }));

    const loginField = screen.getByLabelText("Username or email");
    const passwordField = screen.getByLabelText("Password");
    expect(loginField).toHaveAttribute("aria-invalid", "true");
    expect(loginField).toHaveAccessibleDescription(
      "Enter your username or email.",
    );
    expect(passwordField).toHaveAttribute("aria-invalid", "true");
    expect(passwordField).toHaveAccessibleDescription("Enter your password.");
    expect(requests).toEqual([]);
  });

  it("given a wrong password, when the account logs in, then the page says that the credentials are incorrect", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ user: admin }));
    const { router } = renderRoute("/login");

    await logIn(user, { login: "admin", password: "not the password" });

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The username, email or password is incorrect.",
    );
    expect(router.state.location.pathname).toBe("/login");
  });

  it.each([
    {
      lng: "en",
      labels: english,
      message: "Too many attempts. Try again in 1m 30s.",
    },
    {
      lng: "fr",
      labels: {
        login: "Nom d'utilisateur ou e-mail",
        password: "Mot de passe",
        submit: "Se connecter",
      },
      message: "Trop de tentatives. Réessayez dans 1 min 30 s.",
    },
  ])(
    "given too many failed logins in $lng, when the account logs in, then the page says how long to wait",
    async ({ lng, labels, message }) => {
      const i18n = await initI18n();
      await i18n.changeLanguage(lng);
      const user = userEvent.setup();
      server.use(rateLimited(90));
      renderRoute("/login");

      await logIn(user, { login: "admin", password }, labels);

      expect(await screen.findByRole("alert")).toHaveTextContent(message);
    },
  );

  it.each([
    {
      name: "a failed CSRF check",
      response: () =>
        problemResponse({
          title: "Forbidden",
          status: 403,
          code: "csrf_failed",
        }),
      message:
        "The request was blocked as a security measure. Reload the page and try again.",
    },
    {
      name: "an error code without a message of its own",
      response: () =>
        problemResponse({
          title: "Internal Server Error",
          status: 500,
          code: "internal",
        }),
      message: "Something went wrong. Try again.",
    },
    {
      name: "a network failure",
      response: () => HttpResponse.error(),
      message: "Something went wrong. Try again.",
    },
  ])(
    "given $name, when the account logs in, then the page shows the message of its code",
    async ({ response, message }) => {
      const user = userEvent.setup();
      server.use(http.post("/api/v1/auth/login", response));
      renderRoute("/login");

      await logIn(user, { login: "admin", password });

      expect(await screen.findByRole("alert")).toHaveTextContent(message);
    },
  );
});
