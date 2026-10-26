import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { initI18n } from "@/i18n";
import { renderRoute } from "@/test/render";

describe("index route", () => {
  it("given the index route, when it renders, then it shows Verve as the level 1 heading", async () => {
    renderRoute("/");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Verve" }),
    ).toBeInTheDocument();
  });

  it.each([
    { lng: "en", tagline: "Your health data, on your server." },
    { lng: "fr", tagline: "Vos données de santé, sur votre serveur." },
  ])(
    "given the index route in $lng, when it renders, then it shows the translated tagline below the heading",
    async ({ lng, tagline }) => {
      const i18n = await initI18n();
      await i18n.changeLanguage(lng);

      renderRoute("/");

      const heading = await screen.findByRole("heading", { level: 1 });
      const paragraph = screen.getByText(tagline);
      expect(heading.compareDocumentPosition(paragraph)).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      );
    },
  );
});
