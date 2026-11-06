import { screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest";

import type {
  SetupInputBody,
  UpdatePreferencesInputBody,
} from "@/api/generated";
import { getCurrentUserQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { admin, fakeAuth, server } from "@/test/server";

/** The setup token that the fake server printed. */
const setupToken = "bq3jX0m_Vb8Qe5rWkY2nT4uH7cL9aZ1sD6fG0hJ3kP8";

const adminPassword = "a long passphrase for the admin";

/**
 * Returns the bodies of the setup requests, which the handlers registered
 * before still answer.
 */
function recordSetups() {
  const bodies: SetupInputBody[] = [];
  server.use(
    http.post<never, SetupInputBody>("/api/v1/setup", async ({ request }) => {
      bodies.push(await request.clone().json());
    }),
  );
  return bodies;
}

/** Answers POST /api/v1/setup with the problem of `code`. */
function rejectSetup({ status, code }: { status: number; code: string }) {
  return http.post("/api/v1/setup", () =>
    problemResponse({ title: "Setup rejected", status, code }),
  );
}

/** Makes the browser report `timeZone` as its timezone. */
function stubBrowserTimezone(timeZone: string) {
  const options = new Intl.DateTimeFormat().resolvedOptions();
  vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockReturnValue({
    ...options,
    timeZone,
  });
}

/**
 * Answers PATCH /api/v1/account/preferences as the server does for a valid
 * name, and returns the bodies it receives.
 */
function acceptPreferences() {
  const bodies: UpdatePreferencesInputBody[] = [];
  server.use(
    http.patch<never, UpdatePreferencesInputBody>(
      "/api/v1/account/preferences",
      async ({ request }) => {
        const body = await request.json();
        bodies.push(body);
        return HttpResponse.json({ timezone: body.timezone ?? null });
      },
    ),
  );
  return bodies;
}

interface AdminEntry {
  username?: string;
  password?: string;
  /** Defaults to the password. */
  confirmation?: string;
}

/** Fills in the admin account step and submits it. */
async function createAdmin(
  user: UserEvent,
  {
    username = "admin",
    password = adminPassword,
    confirmation = password,
  }: AdminEntry = {},
) {
  await user.type(await screen.findByLabelText("Username"), username);
  await user.type(screen.getByLabelText("Password"), password);
  await user.type(screen.getByLabelText("Confirm password"), confirmation);
  await user.click(screen.getByRole("button", { name: "Create account" }));
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("setup page", () => {
  it("given /setup#<token>, then the fragment leaves the history before any request and the token is sent in the body of the setup request", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    const setups = recordSetups();
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

    page.rendered = renderRoute(`/setup#${setupToken}`);
    await createAdmin(user);

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "Confirm your timezone",
      }),
    ).toBeInTheDocument();
    expect(setups).toEqual([
      { token: setupToken, username: "admin", password: adminPassword },
    ]);
    expect(addresses).not.toHaveLength(0);
    expect(addresses.filter((address) => address !== "/setup")).toEqual([]);
  });

  it("given the link, when the user goes through the wizard, then its steps are the admin account, the timezone of the browser and the import guide, and the app opens with the session of the admin", async () => {
    const user = userEvent.setup();
    stubBrowserTimezone("Asia/Tokyo");
    server.use(...fakeAuth({ setupToken }));
    const preferences = acceptPreferences();
    const { queryClient, router } = renderRoute(`/setup#${setupToken}`);

    expect(await screen.findByText("Step 1 of 3")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Admin account" }),
    ).toBeInTheDocument();
    await createAdmin(user, { username: "root" });

    expect(await screen.findByText("Step 2 of 3")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Confirm your timezone" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Timezone" })).toHaveValue(
      "Asia/Tokyo",
    );
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByText("Step 3 of 3")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Import your data" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "Open Verve" }));

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(router.state.location.href).toBe("/");
    expect(preferences).toEqual([{ timezone: "Asia/Tokyo" }]);
    expect(queryClient.getQueryData(getCurrentUserQueryKey())).toEqual({
      id: 1,
      username: "root",
      role: "admin",
      timezone: "Asia/Tokyo",
    });
    // The timezone is confirmed: the app does not ask for it again.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("given the wizard, when the user completes a step with the keyboard, then focus moves to the heading of the next step", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    acceptPreferences();
    renderRoute(`/setup#${setupToken}`);
    await user.type(await screen.findByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), adminPassword);
    await user.type(
      screen.getByLabelText("Confirm password"),
      `${adminPassword}{Enter}`,
    );

    const timezone = await screen.findByRole("heading", {
      level: 2,
      name: "Confirm your timezone",
    });
    await waitFor(() => {
      expect(timezone).toHaveFocus();
    });
    await user.tab();
    expect(screen.getByRole("textbox", { name: "Timezone" })).toHaveFocus();
    await user.keyboard("{Enter}");

    const guide = await screen.findByRole("heading", {
      level: 2,
      name: "Import your data",
    });
    await waitFor(() => {
      expect(guide).toHaveFocus();
    });
  });

  it("given /setup without a fragment, then the page asks for the setup token", async () => {
    server.use(...fakeAuth());
    renderRoute("/setup");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Set up Verve" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Setup token" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Setup link or token" }),
    ).toHaveValue("");
  });

  it.each([
    { entry: "the token", input: setupToken },
    {
      entry: "the setup link",
      input: `  https://verve.example.com/setup#${setupToken}  `,
    },
  ])(
    "given /setup without a fragment, when the user enters $entry, then the wizard has four steps and sends the token",
    async ({ input }) => {
      const user = userEvent.setup();
      server.use(...fakeAuth({ setupToken }));
      const setups = recordSetups();
      renderRoute("/setup");

      expect(await screen.findByText("Step 1 of 4")).toBeInTheDocument();
      await user.type(screen.getByLabelText("Setup link or token"), input);
      await user.click(screen.getByRole("button", { name: "Continue" }));
      expect(await screen.findByText("Step 2 of 4")).toBeInTheDocument();
      await createAdmin(user);

      expect(await screen.findByText("Step 3 of 4")).toBeInTheDocument();
      expect(setups).toEqual([
        { token: setupToken, username: "admin", password: adminPassword },
      ]);
    },
  );

  it("given the token step, when the user continues without a token, then the field says that it is required", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    renderRoute("/setup");

    await user.click(await screen.findByRole("button", { name: "Continue" }));

    const field = screen.getByLabelText("Setup link or token");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription(
      "Enter the setup link or the token.",
    );
    expect(screen.getByText("Step 1 of 4")).toBeInTheDocument();
  });

  it("given a link with a token that is no longer valid, when the admin is created, then the wizard asks for the token and says why, and the token entered then creates the admin", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    const setups = recordSetups();
    renderRoute("/setup#the-token-of-a-previous-start");

    await createAdmin(user);

    expect(await screen.findByText("Step 1 of 4")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This setup token is not valid. Each start of Verve writes a new setup link to its log: use the latest one.",
    );
    await user.type(screen.getByLabelText("Setup link or token"), setupToken);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await createAdmin(user);

    expect(await screen.findByText("Step 3 of 4")).toBeInTheDocument();
    expect(setups.map((setup) => setup.token)).toEqual([
      "the-token-of-a-previous-start",
      setupToken,
    ]);
  });

  it.each([
    {
      code: "username_invalid",
      message:
        "Use 1 to 64 characters for the username, without @, spaces or invisible characters.",
    },
    {
      code: "password_too_short",
      message: "Use at least 12 characters for the password.",
    },
    {
      code: "password_too_long",
      message: "Use at most 128 characters for the password.",
    },
    {
      code: "password_too_common",
      message: "This password is too common. Choose another one.",
    },
  ])(
    "given an account that breaks a rule, when the server answers $code, then the account step says what to change",
    async ({ code, message }) => {
      const user = userEvent.setup();
      server.use(
        rejectSetup({ status: 422, code }),
        ...fakeAuth({ setupToken }),
      );
      renderRoute(`/setup#${setupToken}`);

      await createAdmin(user);

      expect(await screen.findByRole("alert")).toHaveTextContent(message);
      expect(screen.getByText("Step 1 of 3")).toBeInTheDocument();
    },
  );

  it("given an admin created meanwhile, for example with the CLI, when the admin is created, then the page says that setup is complete and links to the login page", async () => {
    const user = userEvent.setup();
    server.use(
      rejectSetup({ status: 409, code: "setup_completed" }),
      ...fakeAuth({ setupToken }),
    );
    const { router } = renderRoute(`/setup#${setupToken}`);

    await createAdmin(user);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Verve is already set up. Log in with an existing account.",
    );
    await user.click(
      screen.getByRole("link", { name: "Go to the login page" }),
    );
    expect(
      await screen.findByRole("heading", { level: 1, name: "Log in" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("given a confirmation that differs from the password, when the admin is created, then the confirmation says so and nothing is sent", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    const setups = recordSetups();
    renderRoute(`/setup#${setupToken}`);

    await createAdmin(user, { confirmation: "another passphrase entirely" });

    const confirmation = screen.getByLabelText("Confirm password");
    expect(confirmation).toHaveAttribute("aria-invalid", "true");
    expect(confirmation).toHaveAccessibleDescription(
      "The passwords do not match.",
    );
    expect(setups).toEqual([]);
  });

  it("given a reload during the wizard, then nothing sensitive persists: the storage stays untouched and the reloaded page asks for the token again", async () => {
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    const writes = vi.spyOn(Storage.prototype, "setItem");
    const { router, unmount } = renderRoute(`/setup#${setupToken}`);
    await user.type(await screen.findByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), adminPassword);

    // A reload keeps the URL and the state of the history entry, nothing else.
    const { href, state } = router.history.location;
    unmount();
    renderRoute(href);

    expect(await screen.findByText("Step 1 of 4")).toBeInTheDocument();
    expect(screen.getByLabelText("Setup link or token")).toHaveValue("");
    expect(href).not.toContain(setupToken);
    expect(JSON.stringify(state)).not.toContain(setupToken);
    expect(writes).not.toHaveBeenCalled();
    expect(localStorage).toHaveLength(0);
    expect(sessionStorage).toHaveLength(0);
  });

  it("given the app in French, then the wizard and its errors are translated", async () => {
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    const user = userEvent.setup();
    server.use(...fakeAuth({ setupToken }));
    renderRoute("/setup#the-token-of-a-previous-start");

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Configurer Verve",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("Étape 1 sur 3")).toBeInTheDocument();
    await user.type(screen.getByLabelText("Nom d'utilisateur"), "admin");
    await user.type(screen.getByLabelText("Mot de passe"), adminPassword);
    await user.type(
      screen.getByLabelText("Confirmer le mot de passe"),
      adminPassword,
    );
    await user.click(screen.getByRole("button", { name: "Créer le compte" }));

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "Jeton de configuration",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Ce jeton de configuration n'est pas valide. Chaque démarrage de Verve écrit un nouveau lien de configuration dans son journal : utilisez le plus récent.",
    );
    expect(
      screen.getByRole("textbox", { name: "Lien ou jeton de configuration" }),
    ).toBeInTheDocument();
  });

  it.each(["/setup", "/setup#a-setup-token"])(
    "given setup is not required, when opening %s, then the app goes to the login page",
    async (path) => {
      server.use(...fakeAuth({ user: admin }));
      const { router } = renderRoute(path);

      expect(
        await screen.findByRole("heading", { level: 1, name: "Log in" }),
      ).toBeInTheDocument();
      expect(router.state.location.href).toBe("/login");
    },
  );
});
