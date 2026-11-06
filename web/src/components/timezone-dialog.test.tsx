import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CurrentUser } from "@/api/generated";
import { getCurrentUserQueryKey } from "@/api/generated/@tanstack/react-query.gen";
import { initI18n } from "@/i18n";
import { problemResponse } from "@/test/problem";
import { renderRoute } from "@/test/render";
import { fakeAuth, member, server } from "@/test/server";
import {
  acceptPreferences,
  preferencesPath,
  stubBrowserTimezone,
} from "@/test/timezone";

/** A member who has not confirmed a timezone yet. */
const newcomer: CurrentUser = { ...member, timezone: null };

/** Opens the shell for `newcomer`, with `timeZone` as the browser zone. */
async function openShell(timeZone = "Asia/Tokyo") {
  stubBrowserTimezone(timeZone);
  server.use(...fakeAuth({ user: newcomer, signedIn: true }));
  const rendered = renderRoute("/");
  const dialog = await screen.findByRole("dialog", {
    name: "Confirm your timezone",
  });
  return { ...rendered, dialog };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TimezoneDialog", () => {
  it("given a user without a timezone, when the shell opens, then a dialog proposes the timezone of the browser in an editable field", async () => {
    const { dialog } = await openShell("Asia/Tokyo");

    const field = within(dialog).getByRole("textbox", { name: "Timezone" });
    expect(field).toHaveValue("Asia/Tokyo");
    expect(field).toBeEnabled();
  });

  it("given the dialog, when the user confirms the proposed timezone, then it is stored, the dialog closes and the user of the session has it", async () => {
    const user = userEvent.setup();
    const bodies = acceptPreferences();
    const { dialog, queryClient } = await openShell("Asia/Tokyo");

    await user.click(within(dialog).getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    expect(bodies).toEqual([{ timezone: "Asia/Tokyo" }]);
    expect(queryClient.getQueryData(getCurrentUserQueryKey())).toEqual({
      ...newcomer,
      timezone: "Asia/Tokyo",
    });
  });

  it("given the dialog, when the user replaces the timezone with the keyboard and presses Enter, then the edited one is stored", async () => {
    const user = userEvent.setup();
    const bodies = acceptPreferences();
    const { dialog } = await openShell("Asia/Tokyo");
    const field = within(dialog).getByRole("textbox", { name: "Timezone" });

    await waitFor(() => {
      expect(field).toHaveFocus();
    });
    await user.clear(field);
    await user.keyboard("Europe/Paris{Enter}");

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    expect(bodies).toEqual([{ timezone: "Europe/Paris" }]);
  });

  it("given the dialog, then focus starts in the field and Tab moves through its buttons without leaving it", async () => {
    const user = userEvent.setup();
    const { dialog } = await openShell();
    const field = within(dialog).getByRole("textbox", { name: "Timezone" });

    await waitFor(() => {
      expect(field).toHaveFocus();
    });
    await user.tab();
    expect(
      within(dialog).getByRole("button", { name: "Not now" }),
    ).toHaveFocus();
    await user.tab();
    expect(
      within(dialog).getByRole("button", { name: "Confirm" }),
    ).toHaveFocus();
    await user.tab();
    expect(field).toHaveFocus();
  });

  it.each([
    {
      name: "presses Escape",
      dismiss: (user: UserEvent) => user.keyboard("{Escape}"),
    },
    {
      name: "chooses Not now",
      dismiss: (user: UserEvent) =>
        user.click(screen.getByRole("button", { name: "Not now" })),
    },
  ])(
    "given the dialog, when the user $name, then it closes without storing a timezone and the page is usable",
    async ({ dismiss }) => {
      const user = userEvent.setup();
      const bodies = acceptPreferences();
      await openShell();

      await dismiss(user);

      await waitFor(() => {
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      });
      expect(
        screen.getByRole("heading", { level: 1, name: "Verve" }),
      ).toBeInTheDocument();
      expect(bodies).toEqual([]);
    },
  );

  it("given a name that is not a timezone, when the user confirms it, then the dialog says so and stays open", async () => {
    const user = userEvent.setup();
    server.use(
      http.patch(preferencesPath, () =>
        problemResponse({
          title: "Unprocessable Entity",
          status: 422,
          code: "timezone_invalid",
        }),
      ),
    );
    const { dialog } = await openShell("Mars/Olympus_Mons");

    await user.click(within(dialog).getByRole("button", { name: "Confirm" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Verve does not know this timezone. Enter a name such as Europe/Paris.",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("given an empty field, when the user confirms, then the field says that it is required and nothing is sent", async () => {
    const user = userEvent.setup();
    const bodies = acceptPreferences();
    const { dialog } = await openShell();
    const field = within(dialog).getByRole("textbox", { name: "Timezone" });

    await user.clear(field);
    await user.click(within(dialog).getByRole("button", { name: "Confirm" }));

    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription("Enter a timezone.");
    expect(bodies).toEqual([]);
  });

  it("given a user with a timezone, when the shell opens, then no dialog asks for it", async () => {
    server.use(...fakeAuth({ user: member, signedIn: true }));
    renderRoute("/");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("given the app in French, then the dialog is translated", async () => {
    const i18n = await initI18n();
    await i18n.changeLanguage("fr");
    stubBrowserTimezone("Europe/Paris");
    server.use(...fakeAuth({ user: newcomer, signedIn: true }));
    renderRoute("/");

    const dialog = await screen.findByRole("dialog", {
      name: "Confirmez votre fuseau horaire",
    });
    expect(dialog).toHaveAccessibleDescription(
      "Vos journées commencent à minuit dans ce fuseau horaire. Il vient de votre navigateur : corrigez-le s'il est faux.",
    );
    expect(
      within(dialog).getByRole("textbox", { name: "Fuseau horaire" }),
    ).toHaveValue("Europe/Paris");
    expect(
      within(dialog).getByRole("button", { name: "Confirmer" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Plus tard" }),
    ).toBeInTheDocument();
  });
});
