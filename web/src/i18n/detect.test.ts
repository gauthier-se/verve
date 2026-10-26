import { describe, expect, it, vi } from "vitest";

import { initI18n } from "@/i18n";

function setBrowserLanguages(languages: string[]) {
  vi.spyOn(navigator, "languages", "get").mockReturnValue(languages);
  vi.spyOn(navigator, "language", "get").mockReturnValue(languages[0] ?? "");
}

describe("language detection", () => {
  it('given browser languages ["fr-FR", "en"], when the app loads, then French is used', async () => {
    setBrowserLanguages(["fr-FR", "en"]);

    const i18n = await initI18n();

    expect(i18n.language).toBe("fr");
  });

  it('given browser languages ["de-DE"], when the app loads, then English is used', async () => {
    setBrowserLanguages(["de-DE"]);

    const i18n = await initI18n();

    expect(i18n.language).toBe("en");
  });

  it('given localStorage["verve.language"] = "en" and a French browser, when the app loads, then English is used', async () => {
    setBrowserLanguages(["fr-FR", "fr"]);
    localStorage.setItem("verve.language", "en");

    const i18n = await initI18n();

    expect(i18n.language).toBe("en");
  });

  it("given a detected language, when the app loads, then nothing is written to localStorage", async () => {
    setBrowserLanguages(["fr-FR"]);

    await initI18n();

    expect(localStorage.getItem("verve.language")).toBeNull();
  });
});
