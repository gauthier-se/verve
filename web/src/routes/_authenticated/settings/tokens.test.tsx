import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ApiToken } from "@/api/generated";
import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { fakeAuth, member, server } from "@/test/server";
import {
  createdSecret,
  dashboardToken,
  fakeTokens,
  iPhoneToken,
  tokensPath,
} from "@/test/tokens";

/** A token created long before `now`, never used, which each case dates. */
const unusedToken: ApiToken = {
  id: 2,
  name: "iPhone",
  scopes: ["ingest"],
  prefix: "vrv_7Tq2",
  createdAt: "2025-01-01T12:00:00Z",
};

/** The current time of every test: the fixtures date from shortly before. */
const now = new Date("2026-11-11T12:00:00Z");

/** Opens the tokens page for `member`, once it shows its heading. */
async function openTokensPage() {
  server.use(...fakeAuth({ user: member, signedIn: true }));
  const rendered = renderRoute("/settings/tokens");
  await screen.findByRole("heading", { level: 1, name: "API tokens" });
  return rendered;
}

/** The row of the token named `name` in the table of tokens. */
async function findTokenRow(name: string) {
  const table = await screen.findByRole("table", { name: "API tokens" });
  const row = within(table)
    .getAllByRole("row")
    .find((candidate) => within(candidate).queryByText(name) !== null);
  if (row === undefined) {
    throw new Error(`no row for the token ${name}`);
  }
  return row;
}

/** The texts of the cells of `row`, in the order of the columns. */
function cellTexts(row: HTMLElement) {
  return within(row)
    .getAllByRole("cell")
    .map((cell) => cell.textContent);
}

/** Opens the creation dialog with a click. */
async function openCreationDialog(user: UserEvent) {
  await user.click(screen.getByRole("button", { name: "Create token" }));
  return screen.findByRole("dialog", { name: "Create a token" });
}

/** Moves the focus with Tab until it reaches `target`. */
async function tabTo(user: UserEvent, target: HTMLElement) {
  for (let i = 0; i < 30 && document.activeElement !== target; i++) {
    await user.tab();
  }
  expect(target).toHaveFocus();
}

/** Everything that the query and mutation caches of the app hold. */
function cacheContents(queryClient: QueryClient) {
  return JSON.stringify([
    queryClient
      .getQueryCache()
      .getAll()
      .map((query) => query.state),
    queryClient
      .getMutationCache()
      .getAll()
      .map((mutation) => mutation.state),
  ]);
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(now);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("tokens page", () => {
  it("given the user menu, when the user chooses API tokens, then the tokens page opens", async () => {
    const user = userEvent.setup();
    fakeTokens();
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "alice" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "API tokens" }),
    );

    expect(
      await screen.findByRole("heading", { level: 1, name: "API tokens" }),
    ).toBeInTheDocument();
  });

  describe("table", () => {
    it("given tokens, then a table shows the name, scopes, prefix, creation, last use and expiry of each, newest first", async () => {
      fakeTokens([iPhoneToken, dashboardToken]);
      await openTokensPage();

      const table = await screen.findByRole("table", { name: "API tokens" });
      expect(
        within(table)
          .getAllByRole("columnheader")
          .map((header) => header.textContent),
      ).toEqual([
        "Name",
        "Scopes",
        "Prefix",
        "Created",
        "Last used",
        "Expires",
        "Actions",
      ]);
      const [, first, second] = within(table).getAllByRole("row");
      expect(first && cellTexts(first)).toEqual([
        "iPhone",
        "ingest",
        "vrv_7Tq2",
        "Oct 1, 2026",
        "Nov 10, 2026",
        "Never",
        "Revoke",
      ]);
      expect(second && cellTexts(second)).toEqual([
        "Home Assistant",
        "read",
        "vrv_8Hq2",
        "Sep 15, 2026",
        "Never",
        "Sep 15, 2027",
        "Revoke",
      ]);
    });

    it("given a revoked token, then its row says so and offers no revoke action", async () => {
      fakeTokens([{ ...iPhoneToken, revokedAt: "2026-11-01T10:00:00Z" }]);
      await openTokensPage();

      const row = await findTokenRow("iPhone");
      expect(within(row).getByText("Revoked")).toBeInTheDocument();
      expect(within(row).queryByRole("button")).not.toBeInTheDocument();
    });

    it("given an expired token, then its expiry says that it expired", async () => {
      fakeTokens([{ ...dashboardToken, expiresAt: "2026-11-01T10:00:00Z" }]);
      await openTokensPage();

      const row = await findTokenRow("Home Assistant");
      expect(cellTexts(row)[5]).toBe("Nov 1, 2026Expired");
    });

    it("given the tokens cannot be loaded, then the page says so", async () => {
      server.use(
        http.get(tokensPath, () =>
          problemResponse({
            title: "Internal Server Error",
            status: 500,
            code: "internal",
          }),
        ),
      );
      await openTokensPage();

      expect(await screen.findByRole("alert")).toHaveTextContent(
        "Something went wrong. Try again.",
      );
    });
  });

  describe("revocation", () => {
    it("given a token, when the user revokes it and confirms, then it is revoked and its row says so", async () => {
      const user = userEvent.setup();
      const { revoked } = fakeTokens([iPhoneToken, dashboardToken]);
      await openTokensPage();

      await user.click(
        within(await findTokenRow("iPhone")).getByRole("button", {
          name: "Revoke iPhone",
        }),
      );
      const dialog = await screen.findByRole("alertdialog", {
        name: "Revoke iPhone?",
      });
      expect(dialog).toHaveAccessibleDescription(
        "The apps that use this token stop working at their next request. You cannot undo this.",
      );
      await user.click(
        within(dialog).getByRole("button", { name: "Revoke token" }),
      );

      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
      });
      expect(revoked).toEqual([iPhoneToken.id]);
      expect(
        await within(await findTokenRow("iPhone")).findByText("Revoked"),
      ).toBeInTheDocument();
      expect(
        within(await findTokenRow("Home Assistant")).getByRole("button", {
          name: "Revoke Home Assistant",
        }),
      ).toBeInTheDocument();
    });

    it("given the confirmation, when the user cancels, then nothing is revoked", async () => {
      const user = userEvent.setup();
      const { revoked } = fakeTokens([iPhoneToken]);
      await openTokensPage();

      await user.click(
        within(await findTokenRow("iPhone")).getByRole("button", {
          name: "Revoke iPhone",
        }),
      );
      const dialog = await screen.findByRole("alertdialog");
      await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

      await waitFor(() => {
        expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
      });
      expect(revoked).toEqual([]);
      expect(
        within(await findTokenRow("iPhone")).getByRole("button", {
          name: "Revoke iPhone",
        }),
      ).toBeInTheDocument();
    });

    it("given a revocation that fails, then the confirmation says so and stays open", async () => {
      const user = userEvent.setup();
      fakeTokens([iPhoneToken]);
      server.use(
        http.delete(`${tokensPath}/:id`, () =>
          problemResponse({
            title: "Internal Server Error",
            status: 500,
            code: "internal",
          }),
        ),
      );
      await openTokensPage();

      await user.click(
        within(await findTokenRow("iPhone")).getByRole("button", {
          name: "Revoke iPhone",
        }),
      );
      const dialog = await screen.findByRole("alertdialog");
      await user.click(
        within(dialog).getByRole("button", { name: "Revoke token" }),
      );

      expect(await within(dialog).findByRole("alert")).toHaveTextContent(
        "Something went wrong. Try again.",
      );
      expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    });
  });

  describe("creation", () => {
    it("given the creation dialog, then it offers the presets Health Auto Export, Read only and Custom, Health Auto Export first and chosen", async () => {
      const user = userEvent.setup();
      fakeTokens();
      await openTokensPage();

      const dialog = await openCreationDialog(user);

      const presets = within(dialog).getByRole("radiogroup", {
        name: "Access",
      });
      expect(within(presets).getAllByRole("radio")).toHaveLength(3);
      expect(
        within(presets).getByRole("radio", { name: /^Health Auto Export/ }),
      ).toBeChecked();
      expect(
        within(presets).getByRole("radio", { name: /^Read only/ }),
      ).not.toBeChecked();
      expect(
        within(presets).getByRole("radio", { name: /^Custom/ }),
      ).not.toBeChecked();
    });

    it.each([
      { preset: /^Health Auto Export/, scopes: ["ingest"] },
      { preset: /^Read only/, scopes: ["read"] },
    ])(
      "given the preset $preset, when the user creates a token, then it has the scopes $scopes",
      async ({ preset, scopes }) => {
        const user = userEvent.setup();
        const { created } = fakeTokens();
        await openTokensPage();
        const dialog = await openCreationDialog(user);

        await user.type(
          within(dialog).getByRole("textbox", { name: "Name" }),
          "My token",
        );
        await user.click(within(dialog).getByRole("radio", { name: preset }));
        await user.click(
          within(dialog).getByRole("button", { name: "Create" }),
        );

        await screen.findByRole("dialog", { name: "Token created" });
        expect(created).toEqual([{ name: "My token", scopes }]);
      },
    );

    it("given the preset Custom, when the user chooses both scopes and creates the token, then it has both", async () => {
      const user = userEvent.setup();
      const { created } = fakeTokens();
      await openTokensPage();
      const dialog = await openCreationDialog(user);

      await user.type(
        within(dialog).getByRole("textbox", { name: "Name" }),
        "Script",
      );
      await user.click(within(dialog).getByRole("radio", { name: /^Custom/ }));
      await user.click(
        within(dialog).getByRole("checkbox", { name: /^ingest/ }),
      );
      await user.click(within(dialog).getByRole("checkbox", { name: /^read/ }));
      await user.click(within(dialog).getByRole("button", { name: "Create" }));

      await screen.findByRole("dialog", { name: "Token created" });
      expect(created).toEqual([{ name: "Script", scopes: ["ingest", "read"] }]);
    });

    it("given the preset Custom without a scope, when the user creates the token, then the dialog asks for one and nothing is sent", async () => {
      const user = userEvent.setup();
      const { created } = fakeTokens();
      await openTokensPage();
      const dialog = await openCreationDialog(user);

      await user.type(
        within(dialog).getByRole("textbox", { name: "Name" }),
        "Script",
      );
      await user.click(within(dialog).getByRole("radio", { name: /^Custom/ }));
      await user.click(within(dialog).getByRole("button", { name: "Create" }));

      expect(
        within(dialog).getByRole("group", { name: "Scopes" }),
      ).toHaveAccessibleDescription("Choose at least one scope.");
      expect(created).toEqual([]);
    });

    it("given an empty name, when the user creates the token, then the field says that it is required and nothing is sent", async () => {
      const user = userEvent.setup();
      const { created } = fakeTokens();
      await openTokensPage();
      const dialog = await openCreationDialog(user);

      await user.click(within(dialog).getByRole("button", { name: "Create" }));

      const name = within(dialog).getByRole("textbox", { name: "Name" });
      expect(name).toHaveAttribute("aria-invalid", "true");
      expect(name).toHaveAccessibleDescription("Enter a name.");
      expect(created).toEqual([]);
    });

    it("given a name longer than 64 characters, when the user creates the token, then the field says so and nothing is sent", async () => {
      const user = userEvent.setup();
      const { created } = fakeTokens();
      await openTokensPage();
      const dialog = await openCreationDialog(user);

      await user.type(
        within(dialog).getByRole("textbox", { name: "Name" }),
        "a".repeat(65),
      );
      await user.click(within(dialog).getByRole("button", { name: "Create" }));

      expect(
        within(dialog).getByRole("textbox", { name: "Name" }),
      ).toHaveAccessibleDescription("Use at most 64 characters for the name.");
      expect(created).toEqual([]);
    });

    it("given a creation that fails, then the dialog says so and keeps the form", async () => {
      const user = userEvent.setup();
      fakeTokens();
      server.use(
        http.post(tokensPath, () =>
          problemResponse({
            title: "Internal Server Error",
            status: 500,
            code: "internal",
          }),
        ),
      );
      await openTokensPage();
      const dialog = await openCreationDialog(user);

      await user.type(
        within(dialog).getByRole("textbox", { name: "Name" }),
        "iPhone",
      );
      await user.click(within(dialog).getByRole("button", { name: "Create" }));

      expect(await within(dialog).findByRole("alert")).toHaveTextContent(
        "Something went wrong. Try again.",
      );
      expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue(
        "iPhone",
      );
    });
  });

  describe("created token", () => {
    /** Creates a token named iPhone with `preset`, and returns the dialog. */
    async function createToken(user: UserEvent, preset: RegExp) {
      const rendered = await openTokensPage();
      const dialog = await openCreationDialog(user);
      await user.type(
        within(dialog).getByRole("textbox", { name: "Name" }),
        "iPhone",
      );
      await user.click(within(dialog).getByRole("radio", { name: preset }));
      await user.click(within(dialog).getByRole("button", { name: "Create" }));
      const created = await screen.findByRole("dialog", {
        name: "Token created",
      });
      return { ...rendered, dialog: created };
    }

    it("given a created token, then the dialog shows it once with a copy button that copies it", async () => {
      const user = userEvent.setup();
      fakeTokens();
      const { dialog } = await createToken(user, /^Read only/);

      expect(dialog).toHaveAccessibleDescription(
        "Copy the token now: Verve shows it only once.",
      );
      expect(
        within(dialog).getByRole("textbox", { name: "Token" }),
      ).toHaveValue(createdSecret);
      await user.click(
        within(dialog).getByRole("button", { name: "Copy token" }),
      );

      expect(await navigator.clipboard.readText()).toBe(createdSecret);
      expect(
        await within(dialog).findByText("Copied to the clipboard."),
      ).toBeInTheDocument();
    });

    it("given a created token, then the table lists it without the full token", async () => {
      const user = userEvent.setup();
      fakeTokens();
      await createToken(user, /^Read only/);

      const row = await findTokenRow("iPhone");
      expect(cellTexts(row).slice(0, 3)).toEqual([
        "iPhone",
        "read",
        createdSecret.slice(0, 8),
      ]);
      expect(row).not.toHaveTextContent(createdSecret);
    });

    it("given a created token, when the dialog closes, then the token leaves the caches and the dialog shows the form again", async () => {
      const user = userEvent.setup();
      fakeTokens();
      const { dialog, queryClient } = await createToken(user, /^Read only/);
      expect(cacheContents(queryClient)).not.toContain(createdSecret);

      await user.click(within(dialog).getByRole("button", { name: "Done" }));

      await waitFor(() => {
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      });
      await waitFor(() => {
        expect(cacheContents(queryClient)).not.toContain(createdSecret);
      });
      const reopened = await openCreationDialog(user);
      expect(
        within(reopened).getByRole("textbox", { name: "Name" }),
      ).toHaveValue("");
      expect(screen.queryByDisplayValue(createdSecret)).not.toBeInTheDocument();
    });

    it("given a Health Auto Export token, then the dialog shows the URL and the header to configure, each with a copy button", async () => {
      const user = userEvent.setup();
      fakeTokens();
      const { dialog } = await createToken(user, /^Health Auto Export/);

      const setup = within(dialog).getByRole("region", {
        name: "Configure Health Auto Export",
      });
      const url = `${window.location.origin}/api/v1/ingest/health-auto-export`;
      expect(within(setup).getByRole("textbox", { name: "URL" })).toHaveValue(
        url,
      );
      expect(
        within(setup).getByRole("textbox", { name: "Header key" }),
      ).toHaveValue("Authorization");
      expect(
        within(setup).getByRole("textbox", { name: "Header value" }),
      ).toHaveValue(`Bearer ${createdSecret}`);

      await user.click(within(setup).getByRole("button", { name: "Copy URL" }));
      expect(await navigator.clipboard.readText()).toBe(url);
      await user.click(
        within(setup).getByRole("button", { name: "Copy header value" }),
      );
      expect(await navigator.clipboard.readText()).toBe(
        `Bearer ${createdSecret}`,
      );
    });

    it("given a token of another preset, then the dialog shows no Health Auto Export setup", async () => {
      const user = userEvent.setup();
      fakeTokens();
      const { dialog } = await createToken(user, /^Read only/);

      expect(
        within(dialog).queryByRole("region", {
          name: "Configure Health Auto Export",
        }),
      ).not.toBeInTheDocument();
    });
  });

  describe("unused token", () => {
    it.each<{ name: string; token: Partial<ApiToken>; suggests: boolean }>([
      {
        name: "last used 90 days ago",
        token: { lastUsedAt: "2026-08-13T12:00:00Z" },
        suggests: true,
      },
      {
        name: "last used 89 days ago",
        token: { lastUsedAt: "2026-08-14T12:00:00Z" },
        suggests: false,
      },
      {
        name: "never used and created 90 days ago",
        token: { createdAt: "2026-08-13T12:00:00Z" },
        suggests: true,
      },
      {
        name: "never used and created 10 days ago",
        token: { createdAt: "2026-11-01T12:00:00Z" },
        suggests: false,
      },
      {
        name: "revoked and last used a year ago",
        token: {
          lastUsedAt: "2025-11-01T12:00:00Z",
          revokedAt: "2026-01-01T12:00:00Z",
        },
        suggests: false,
      },
      {
        name: "expired and last used a year ago",
        token: {
          lastUsedAt: "2025-11-01T12:00:00Z",
          expiresAt: "2026-01-01T12:00:00Z",
        },
        suggests: false,
      },
    ])(
      "given a token $name, then its row suggests revoking it: $suggests",
      async ({ token, suggests }) => {
        fakeTokens([{ ...unusedToken, ...token }]);
        await openTokensPage();

        const row = await findTokenRow("iPhone");
        const suggestion = within(row).queryByText(
          "Not used for 90 days or more. Revoke it if no app needs it anymore.",
        );
        if (suggests) {
          expect(suggestion).toBeInTheDocument();
        } else {
          expect(suggestion).not.toBeInTheDocument();
        }
      },
    );
  });

  describe("empty state", () => {
    it("given no token, then the page explains what tokens are for instead of a table", async () => {
      fakeTokens();
      await openTokensPage();

      expect(
        await screen.findByRole("heading", { level: 2, name: "No token yet" }),
      ).toBeInTheDocument();
      expect(
        screen.getByText(
          "Create a token to connect an app to Verve: Health Auto Export to sync the health data of your iPhone, or a script that reads your data. A token only does what its scopes allow, and you can revoke it at any time.",
        ),
      ).toBeInTheDocument();
      expect(screen.queryByRole("table")).not.toBeInTheDocument();
    });
  });

  describe("keyboard", () => {
    it("given the keyboard only, when the user creates a token, copies it, closes the dialog and revokes the token, then each step works and the focus returns to the page", async () => {
      const user = userEvent.setup();
      const { created, revoked } = fakeTokens();
      await openTokensPage();
      const create = screen.getByRole("button", { name: "Create token" });

      await tabTo(user, create);
      await user.keyboard("{Enter}");
      const dialog = await screen.findByRole("dialog", {
        name: "Create a token",
      });
      await waitFor(() => {
        expect(
          within(dialog).getByRole("textbox", { name: "Name" }),
        ).toHaveFocus();
      });
      await user.keyboard("iPhone{Enter}");

      const shown = await screen.findByRole("dialog", {
        name: "Token created",
      });
      expect(created).toEqual([{ name: "iPhone", scopes: ["ingest"] }]);
      await tabTo(
        user,
        within(shown).getByRole("button", { name: "Copy token" }),
      );
      await user.keyboard("{Enter}");
      expect(await navigator.clipboard.readText()).toBe(createdSecret);
      await user.keyboard("{Escape}");
      await waitFor(() => {
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      });
      expect(create).toHaveFocus();

      const revoke = await within(await findTokenRow("iPhone")).findByRole(
        "button",
        { name: "Revoke iPhone" },
      );
      await tabTo(user, revoke);
      await user.keyboard("{Enter}");
      const confirmation = await screen.findByRole("alertdialog");
      await tabTo(
        user,
        within(confirmation).getByRole("button", { name: "Revoke token" }),
      );
      await user.keyboard("{Enter}");

      await waitFor(() => {
        expect(revoked).toHaveLength(1);
      });
      expect(
        await within(await findTokenRow("iPhone")).findByText("Revoked"),
      ).toBeInTheDocument();
    });

    it("given the creation dialog, when the user moves between the presets with the arrow keys, then the chosen preset follows", async () => {
      const user = userEvent.setup();
      fakeTokens();
      await openTokensPage();
      const dialog = await openCreationDialog(user);
      const hae = within(dialog).getByRole("radio", {
        name: /^Health Auto Export/,
      });

      await tabTo(user, hae);
      await user.keyboard("{ArrowDown}");

      expect(
        within(dialog).getByRole("radio", { name: /^Read only/ }),
      ).toBeChecked();
    });
  });

  describe("translation", () => {
    it("given the app in French, then the page, its table and its creation dialog are translated", async () => {
      const user = userEvent.setup();
      const i18n = await initI18n();
      await i18n.changeLanguage("fr");
      fakeTokens([iPhoneToken]);
      server.use(...fakeAuth({ user: member, signedIn: true }));
      renderRoute("/settings/tokens");

      await screen.findByRole("heading", { level: 1, name: "Jetons d'API" });
      const table = await screen.findByRole("table", { name: "Jetons d'API" });
      expect(
        within(table)
          .getAllByRole("columnheader")
          .map((header) => header.textContent),
      ).toEqual([
        "Nom",
        "Portées",
        "Préfixe",
        "Création",
        "Dernière utilisation",
        "Expiration",
        "Actions",
      ]);
      const [, row] = within(table).getAllByRole("row");
      expect(row && cellTexts(row)).toEqual([
        "iPhone",
        "ingest",
        "vrv_7Tq2",
        "1 oct. 2026",
        "10 nov. 2026",
        "Jamais",
        "Révoquer",
      ]);

      await user.click(screen.getByRole("button", { name: "Créer un jeton" }));
      const dialog = await screen.findByRole("dialog", {
        name: "Créer un jeton",
      });
      expect(
        within(dialog).getByRole("textbox", { name: "Nom" }),
      ).toBeInTheDocument();
      const presets = within(dialog).getByRole("radiogroup", { name: "Accès" });
      expect(
        within(presets).getByRole("radio", { name: /^Health Auto Export/ }),
      ).toBeInTheDocument();
      expect(
        within(presets).getByRole("radio", { name: /^Lecture seule/ }),
      ).toBeInTheDocument();
      expect(
        within(presets).getByRole("radio", { name: /^Personnalisé/ }),
      ).toBeInTheDocument();
      expect(
        within(dialog).getByRole("button", { name: "Créer" }),
      ).toBeInTheDocument();
    });

    it("given the app in French and no token, then the empty state is translated", async () => {
      const i18n = await initI18n();
      await i18n.changeLanguage("fr");
      fakeTokens();
      server.use(...fakeAuth({ user: member, signedIn: true }));
      renderRoute("/settings/tokens");

      expect(
        await screen.findByRole("heading", { level: 2, name: "Aucun jeton" }),
      ).toBeInTheDocument();
    });
  });
});
