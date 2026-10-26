import { act, render, screen } from "@testing-library/react";
import { useTranslation } from "react-i18next";
import { describe, expect, it } from "vitest";

import { initI18n } from "@/i18n";

function Tagline() {
  const { t } = useTranslation();
  return <p>{t("home.tagline")}</p>;
}

describe("runtime language switch", () => {
  it('given the app in English, when i18n.changeLanguage("fr") is called, then visible texts update without reload and <html lang> becomes fr', async () => {
    const i18n = await initI18n();
    render(<Tagline />);
    expect(
      screen.getByText("Your health data, on your server."),
    ).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("en");

    await act(() => i18n.changeLanguage("fr"));

    expect(
      screen.getByText("Vos données de santé, sur votre serveur."),
    ).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("fr");
  });
});
