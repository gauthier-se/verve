import { screen } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest";

import type {
  AcceptInvitationInputBody,
  CurrentUser,
  InspectInvitationInputBody,
} from "@/api/generated";
import { getCurrentUserQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { fakeAuth, server } from "@/test/server";

/** The token of the invitation link. */
const invitationToken = "Zk2pQ8rT1vX4yB7nM0cL3hJ6gF9dS5aW2eR8tY1uI4o";

const bobPassword = "a long passphrase for bob";

/** The user that accepting the invitation creates. */
const bob: CurrentUser = {
  id: 2,
  username: "bob",
  email: "bob@example.com",
  role: "member",
  timezone: null,
};

/** The problem of a used, expired or unknown invitation. */
function invitationGone() {
  return problemResponse({
    title: "Gone",
    status: 410,
    code: "invitation_invalid",
  });
}

/**
 * Fakes the invitation endpoints for one invitation of bob, whose token is
 * `invitationToken`, and returns the bodies they receive. Accepting it opens
 * the session of bob and uses it up.
 */
function fakeInvitation() {
  const inspected: InspectInvitationInputBody[] = [];
  const accepted: AcceptInvitationInputBody[] = [];
  let used = false;
  server.use(
    ...fakeAuth(),
    http.post<never, InspectInvitationInputBody>(
      "/api/v1/invitations/inspect",
      async ({ request }) => {
        const body = await request.json();
        inspected.push(body);
        if (used || body.token !== invitationToken) {
          return invitationGone();
        }
        return HttpResponse.json({
          username: bob.username,
          expiresAt: "2026-11-12T09:00:00Z",
        });
      },
    ),
    http.post<never, AcceptInvitationInputBody>(
      "/api/v1/invitations/accept",
      async ({ request }) => {
        const body = await request.json();
        accepted.push(body);
        if (used || body.token !== invitationToken) {
          return invitationGone();
        }
        used = true;
        return HttpResponse.json(bob, { status: 201 });
      },
    ),
    http.get("/api/v1/auth/me", () =>
      used
        ? HttpResponse.json(bob)
        : problemResponse({
            title: "Unauthorized",
            status: 401,
            code: "unauthorized",
          }),
    ),
  );
  return { inspected, accepted };
}

/** Answers POST /api/v1/invitations/accept with the problem of `code`. */
function rejectAccept({ status, code }: { status: number; code: string }) {
  server.use(
    http.post("/api/v1/invitations/accept", () =>
      problemResponse({ title: "Rejected", status, code }),
    ),
  );
}

interface PasswordEntry {
  password?: string;
  /** Defaults to the password. */
  confirmation?: string;
}

/** Chooses the password of the account and submits it. */
async function choosePassword(
  user: UserEvent,
  { password = bobPassword, confirmation = password }: PasswordEntry = {},
) {
  await user.type(await screen.findByLabelText("Password"), password);
  await user.type(screen.getByLabelText("Confirm password"), confirmation);
  await user.click(screen.getByRole("button", { name: "Create account" }));
}

const invalidMessage =
  "This invitation link is no longer valid: it was already used, it expired, or it is incomplete. Ask an admin for a new link.";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("invitation page", () => {
  it("given /invite#<token>, then the fragment leaves the history before any request and the token is sent in the body of the inspect and accept requests", async () => {
    const user = userEvent.setup();
    const { inspected, accepted } = fakeInvitation();
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

    page.rendered = renderRoute(`/invite#${invitationToken}`);
    await choosePassword(user);

    expect(
      await screen.findByRole("dialog", { name: "Confirm your timezone" }),
    ).toBeInTheDocument();
    expect(inspected).toEqual([{ token: invitationToken }]);
    expect(accepted).toEqual([
      { token: invitationToken, password: bobPassword },
    ]);
    expect(addresses).not.toHaveLength(0);
    expect(addresses[0]).toBe("/invite");
    expect(
      addresses.filter((address) => address.includes(invitationToken)),
    ).toEqual([]);
  });

  it("given a valid invitation, then the page names the invited username, and choosing a password opens the app with the session of the new account, which confirms its timezone first", async () => {
    const user = userEvent.setup();
    fakeInvitation();
    const { queryClient, router } = renderRoute(`/invite#${invitationToken}`);

    expect(
      await screen.findByRole("heading", { level: 1, name: "Join Verve" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText(
        "You are invited to Verve as bob. Choose your password to create your account.",
      ),
    ).toBeInTheDocument();
    await choosePassword(user);

    expect(
      await screen.findByRole("dialog", { name: "Confirm your timezone" }),
    ).toBeInTheDocument();
    expect(router.state.location.href).toBe("/");
    expect(queryClient.getQueryData(getCurrentUserQueryKey())).toEqual(bob);
  });

  it.each([
    {
      link: "an unknown token",
      path: "/invite#not-an-invitation-token",
      inspects: [{ token: "not-an-invitation-token" }],
    },
    { link: "no token", path: "/invite", inspects: [] },
  ])(
    "given a link with $link, then the page says that the link is not valid, without a password form",
    async ({ path, inspects }) => {
      const { inspected } = fakeInvitation();
      renderRoute(path);

      expect(await screen.findByRole("alert")).toHaveTextContent(
        invalidMessage,
      );
      expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
      expect(
        screen.getByRole("link", { name: "Go to the login page" }),
      ).toBeInTheDocument();
      expect(inspected).toEqual(inspects);
    },
  );

  it("given an invitation used or expired meanwhile, when the password is chosen, then the page says that the link is no longer valid", async () => {
    const user = userEvent.setup();
    fakeInvitation();
    rejectAccept({ status: 410, code: "invitation_invalid" });
    renderRoute(`/invite#${invitationToken}`);

    await choosePassword(user);

    expect(await screen.findByRole("alert")).toHaveTextContent(invalidMessage);
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
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
      fakeInvitation();
      rejectAccept({ status: 422, code });
      renderRoute(`/invite#${invitationToken}`);

      await choosePassword(user);

      expect(await screen.findByRole("alert")).toHaveTextContent(message);
      expect(screen.getByLabelText("Password")).toBeInTheDocument();
    },
  );

  it.each([
    {
      code: "username_taken",
      message: "Another account already has this username.",
    },
    {
      code: "email_taken",
      message: "Another account already has this email address.",
    },
  ])(
    "given a login taken since the invitation, when the server answers $code, then the page says so and asks for a new invitation",
    async ({ code, message }) => {
      const user = userEvent.setup();
      fakeInvitation();
      rejectAccept({ status: 409, code });
      renderRoute(`/invite#${invitationToken}`);

      await choosePassword(user);

      expect(await screen.findByRole("alert")).toHaveTextContent(
        `${message} Ask an admin for a new invitation.`,
      );
    },
  );

  it("given a confirmation that differs from the password, then the confirmation says so and nothing is sent", async () => {
    const user = userEvent.setup();
    const { accepted } = fakeInvitation();
    renderRoute(`/invite#${invitationToken}`);

    await choosePassword(user, { confirmation: "another passphrase entirely" });

    const confirmation = screen.getByLabelText("Confirm password");
    expect(confirmation).toHaveAttribute("aria-invalid", "true");
    expect(confirmation).toHaveAccessibleDescription(
      "The passwords do not match.",
    );
    expect(accepted).toEqual([]);
  });

  it("given a reload of the page, then nothing sensitive persists: the storage stays untouched and the reloaded page has no token", async () => {
    const user = userEvent.setup();
    fakeInvitation();
    const writes = vi.spyOn(Storage.prototype, "setItem");
    const { router, unmount } = renderRoute(`/invite#${invitationToken}`);
    await user.type(await screen.findByLabelText("Password"), bobPassword);

    // A reload keeps the URL and the state of the history entry, nothing else.
    const { href, state } = router.history.location;
    unmount();
    renderRoute(href);

    expect(await screen.findByRole("alert")).toHaveTextContent(invalidMessage);
    expect(href).not.toContain(invitationToken);
    expect(JSON.stringify(state)).not.toContain(invitationToken);
    expect(writes).not.toHaveBeenCalled();
    expect(localStorage).toHaveLength(0);
    expect(sessionStorage).toHaveLength(0);
  });

  it("given the app in French, then the page and its errors are translated", async () => {
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    fakeInvitation();
    renderRoute("/invite#not-an-invitation-token");

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Rejoindre Verve",
      }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Ce lien d'invitation n'est plus valide : il a déjà servi, il a expiré ou il est incomplet. Demandez un nouveau lien à un administrateur.",
    );
  });
});
