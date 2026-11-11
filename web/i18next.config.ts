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
    // errors.<code> translates the code of an API problem.
    // metrics:<key> translates a metric type by its key in the catalog of the server.
    preservePatterns: ["common:days_*", "common:errors.*", "metrics:*"],
  },
  types: {
    input: ["src/locales/en/*.json"],
    output: "src/i18n/i18next.d.ts",
    resourcesFile: "src/i18n/resources.d.ts",
  },
  plugins: [acceptUntranslatedText],
});
