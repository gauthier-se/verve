import { screen } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest";

import type { ResetPasswordInputBody } from "@/api/generated";
import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { fakeAuth, server } from "@/test/server";

/** The token of the reset link. */
const resetToken = "Qm9iLXJlc2V0LWxpbmstdG9rZW4tZm9yLXRoZS10ZXN0cw";

const newPassword = "a new passphrase for bob";

/** The problem of a used, expired or unknown reset link. */
function resetLinkGone() {
  return problemResponse({
    title: "Gone",
    status: 410,
    code: "reset_link_invalid",
  });
}

/**
 * Fakes the reset endpoint for one reset link, whose token is `resetToken`,
 * and returns the bodies it receives. A reset uses the link up.
 */
function fakeResetLink() {
  const resets: ResetPasswordInputBody[] = [];
  let used = false;
  server.use(
    ...fakeAuth(),
    http.post<never, ResetPasswordInputBody>(
      "/api/v1/password-reset",
      async ({ request }) => {
        const body = await request.json();
        resets.push(body);
        if (used || body.token !== resetToken) {
          return resetLinkGone();
        }
        used = true;
        return new HttpResponse(null, { status: 204 });
      },
    ),
  );
  return { resets };
}

/** Answers POST /api/v1/password-reset with the problem of `code`. */
function rejectReset({ status, code }: { status: number; code: string }) {
  server.use(
    http.post("/api/v1/password-reset", () =>
      problemResponse({ title: "Rejected", status, code }),
    ),
  );
}

interface PasswordEntry {
  password?: string;
  /** Defaults to the password. */
  confirmation?: string;
}

/** Chooses the new password and submits it. */
async function choosePassword(
  user: UserEvent,
  { password = newPassword, confirmation = password }: PasswordEntry = {},
) {
  await user.type(await screen.findByLabelText("New password"), password);
  await user.type(screen.getByLabelText("Confirm password"), confirmation);
  await user.click(screen.getByRole("button", { name: "Change password" }));
}

const invalidMessage =
  "This password reset link is no longer valid: it was already used, it expired, a newer one replaced it, or it is incomplete. Ask an admin for a new link.";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("password reset page", () => {
  it("given /reset-password#<token>, then the fragment leaves the history before any request and the token is sent in the body of the reset request", async () => {
    const user = userEvent.setup();
    const { resets } = fakeResetLink();
    // The address in the history when each request starts.
    const addresses: string[] = [];
    const page: { rendered?: ReturnType<typeof renderRoute> } = {};
    const onRequest = () => {
      addresses.push(
        page.rendered?.router.history.location.href ?? "not rendered",
      );
    };
    server.events.on("request:start", onRequest);
    onTestFinished(() => {
      server.events.removeListener("request:start", onRequest);
    });

    page.rendered = renderRoute(`/reset-password#${resetToken}`);
    expect(page.rendered.router.history.location.href).toBe("/reset-password");
    await choosePassword(user);

    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(resets).toEqual([{ token: resetToken, password: newPassword }]);
    expect(addresses).not.toHaveLength(0);
    expect(
      addresses.filter((address) => address.includes(resetToken)),
    ).toEqual([]);
  });

  it("given a valid link, when the new password is chosen, then the page goes to the login page, which says that the password changed", async () => {
    const user = userEvent.setup();
    fakeResetLink();
    const { router } = renderRoute(`/reset-password#${resetToken}`);

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Reset your password",
      }),
    ).toBeInTheDocument();
    await choosePassword(user);

    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    expect(screen.getByRole("status")).toHaveTextContent(
      "Your password changed. Log in with it.",
    );
  });

  it("given a link without a token, then the page says that the link is not valid, without a password form", async () => {
    const { resets } = fakeResetLink();
    renderRoute("/reset-password");

    expect(await screen.findByRole("alert")).toHaveTextContent(invalidMessage);
    expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Go to the login page" }),
    ).toBeInTheDocument();
    expect(resets).toEqual([]);
  });

  it("given a link used twice, expired or unknown, when the password is chosen, then the server answers reset_link_invalid and the page says that the link is no longer valid", async () => {
    const user = userEvent.setup();
    fakeResetLink();
    renderRoute("/reset-password#not-a-reset-token");

    await choosePassword(user);

    expect(await screen.findByRole("alert")).toHaveTextContent(invalidMessage);
    expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Go to the login page" }),
    ).toBeInTheDocument();
  });

  it.each([
    {
      code: "password_too_short",
      message: "Use at least 12 characters for the password.",
    },
    {
      code: "password_too_common",
      message: "This password is too common. Choose another one.",
    },
  ])(
    "given a password rejected with $code, then the page says what to change and keeps the form",
    async ({ code, message }) => {
      const user = userEvent.setup();
      fakeResetLink();
      rejectReset({ status: 422, code });
      renderRoute(`/reset-password#${resetToken}`);

      await choosePassword(user);

      expect(await screen.findByRole("alert")).toHaveTextContent(message);
      expect(screen.getByLabelText("New password")).toBeInTheDocument();
    },
  );

  it("given a confirmation that differs from the password, then the confirmation says so and nothing is sent", async () => {
    const user = userEvent.setup();
    const { resets } = fakeResetLink();
    renderRoute(`/reset-password#${resetToken}`);

    await choosePassword(user, { confirmation: "another passphrase entirely" });

    const confirmation = screen.getByLabelText("Confirm password");
    expect(confirmation).toHaveAttribute("aria-invalid", "true");
    expect(confirmation).toHaveAccessibleDescription(
      "The passwords do not match.",
    );
    expect(resets).toEqual([]);
  });

  it("given a reload of the page, then nothing sensitive persists: the storage stays untouched and the reloaded page has no token", async () => {
    const user = userEvent.setup();
    fakeResetLink();
    const writes = vi.spyOn(Storage.prototype, "setItem");
    const { router, unmount } = renderRoute(`/reset-password#${resetToken}`);
    await user.type(await screen.findByLabelText("New password"), newPassword);

    // A reload keeps the URL and the state of the history entry, nothing else.
    const { href, state } = router.history.location;
    unmount();
    renderRoute(href);

    expect(await screen.findByRole("alert")).toHaveTextContent(invalidMessage);
    expect(href).not.toContain(resetToken);
    expect(JSON.stringify(state)).not.toContain(resetToken);
    expect(writes).not.toHaveBeenCalled();
    expect(localStorage).toHaveLength(0);
    expect(sessionStorage).toHaveLength(0);
  });

  it("given the app in French, then the page and its errors are translated", async () => {
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    fakeResetLink();
    renderRoute("/reset-password");

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Réinitialiser votre mot de passe",
      }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Ce lien de réinitialisation du mot de passe n'est plus valide : il a déjà servi, il a expiré, un lien plus récent l'a remplacé ou il est incomplet. Demandez un nouveau lien à un administrateur.",
    );
  });
});
