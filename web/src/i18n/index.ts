import i18next, { type i18n } from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";

import enCommon from "@/locales/en/common.json";
import frCommon from "@/locales/fr/common.json";

export const supportedLanguages = ["en", "fr"] as const;

/**
 * Temporary language override until the server-side preference (M2): set it
 * by hand to force a language whatever the browser language.
 */
export const languageStorageKey = "verve.language";

/** Query parameter value enabling the pseudo-locale in development builds. */
const pseudoLanguage = "pseudo";

/**
 * Creates and initializes the i18next instance used by react-i18next.
 * Every call creates a new instance, which becomes the one `useTranslation`
 * and the formatters in `@/i18n/format` use.
 */
export async function initI18n(): Promise<i18n> {
  const instance = i18next.createInstance();
  instance.on("languageChanged", () => {
    // The detected language can be a regional variant such as "en-US": the
    // resolved one is the language whose texts are displayed.
    document.documentElement.lang = instance.resolvedLanguage ?? "en";
  });

  // Vite replaces import.meta.env.DEV with false in production builds, which
  // drops the pseudo-locale and its post-processor from the bundle.
  const pseudo =
    import.meta.env.DEV &&
    new URLSearchParams(window.location.search).get("lng") === pseudoLanguage;
  const pseudoPostProcessor = pseudo
    ? (await import("@/i18n/pseudo")).pseudoPostProcessor
    : undefined;
  if (pseudoPostProcessor) {
    instance.use(pseudoPostProcessor);
  }

  await instance
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      resources: {
        en: { common: enCommon },
        fr: { common: frCommon },
      },
      supportedLngs: supportedLanguages,
      fallbackLng: "en",
      load: "languageOnly",
      defaultNS: "common",
      ns: ["common"],
      interpolation: { escapeValue: false },
      detection: {
        order: ["localStorage", "navigator"],
        lookupLocalStorage: languageStorageKey,
        caches: [],
        // "fr-FR" becomes "fr", so i18n.language is always a supported code.
        convertDetectedLanguage: (lng) => lng.split("-")[0] ?? lng,
      },
      ...(pseudoPostProcessor && {
        lng: "en",
        postProcess: [pseudoPostProcessor.name],
      }),
    });

  return instance;
}
