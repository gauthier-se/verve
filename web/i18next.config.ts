import { defineConfig, type LinterPlugin } from "i18next-cli";

/** Text that is never translated, accepted by the hardcoded string check. */
const untranslatedText = new Set(["Verve"]);

const acceptUntranslatedText: LinterPlugin = {
  name: "accept-untranslated-text",
  lintOnResult: (_filePath, issues) =>
    issues.filter(
      (issue) =>
        issue.type !== "hardcoded" || !untranslatedText.has(issue.text.trim()),
    ),
};

export default defineConfig({
  locales: ["en", "fr"],
  extract: {
    input: ["src/**/*.{ts,tsx}"],
    ignore: ["src/**/*.test.{ts,tsx}", "src/test/**", "src/api/generated/**"],
    output: "src/locales/{{language}}/{{namespace}}.json",
    primaryLanguage: "en",
    defaultNS: "common",
    // Keys not used in code yet, kept for the components that will need them.
    preservePatterns: ["common:days_*"],
  },
  types: {
    input: ["src/locales/en/*.json"],
    output: "src/i18n/i18next.d.ts",
    resourcesFile: "src/i18n/resources.d.ts",
  },
  plugins: [acceptUntranslatedText],
});
