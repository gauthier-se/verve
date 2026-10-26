import { describe, expect, it, onTestFinished } from "vitest";

import { initI18n } from "@/i18n";
import { pseudoLocalize } from "@/i18n/pseudo";

describe("pseudoLocalize", () => {
  it("given a string, when it is pseudo-localized, then every letter is accented, it is about 40 percent longer and wrapped in brackets", () => {
    expect(pseudoLocalize("Verve")).toBe("[Ṽéŕṽé~~]");
  });

  it("given a longer sentence, when it is pseudo-localized, then the content is about 40 percent longer and contains no plain ASCII letters", () => {
    const source = "Your health data, on your server.";

    const result = pseudoLocalize(source);

    expect(result.startsWith("[")).toBe(true);
    expect(result.endsWith("]")).toBe(true);
    const content = result.slice(1, -1);
    expect(content.length).toBe(Math.round(source.length * 1.4));
    expect(content).not.toMatch(/[A-Za-z]/);
  });
});

describe("pseudo-locale in development", () => {
  it("given a development build opened with ?lng=pseudo, when a text is translated, then it is pseudo-localized", async () => {
    window.history.replaceState(null, "", "/?lng=pseudo");
    onTestFinished(() => {
      window.history.replaceState(null, "", "/");
    });

    const i18n = await initI18n();

    expect(i18n.t("home.tagline")).toBe(
      pseudoLocalize("Your health data, on your server."),
    );
  });
});
