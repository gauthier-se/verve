import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { initI18n } from "@/i18n";
import { renderRoute } from "@/test/render";

describe("not-found page", () => {
  it.each([
    { lng: "en", title: "Page not found", home: "Back to home" },
    { lng: "fr", title: "Page introuvable", home: "Retour à l'accueil" },
  ])(
    "given an unknown client route in $lng, then the layout shows a translated not-found page with a link to the home page",
    async ({ lng, title, home }) => {
      const i18n = await initI18n();
      await i18n.changeLanguage(lng);

      renderRoute("/does-not-exist");

      const main = await screen.findByRole("main");
      expect(
        await within(main).findByRole("heading", { level: 1, name: title }),
      ).toBeInTheDocument();
      expect(within(main).getByRole("link", { name: home })).toHaveAttribute(
        "href",
        "/",
      );
    },
  );

  it("given the not-found page, when the user follows the home link, then the home page is shown", async () => {
    const user = userEvent.setup();
    const { router } = renderRoute("/does-not-exist");

    await user.click(await screen.findByRole("link", { name: "Back to home" }));

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });
});
