import pluginQuery from "@tanstack/eslint-plugin-query";
import pluginRouter from "@tanstack/eslint-plugin-router";
import eslintConfigPrettier from "eslint-config-prettier/flat";
import reactHooks from "eslint-plugin-react-hooks";
import { defineConfig, globalIgnores } from "eslint/config";
import tseslint from "typescript-eslint";

export default defineConfig(
  globalIgnores([
    "dist",
    "coverage",
    "playwright-report",
    "test-results",
    "src/routeTree.gen.ts",
    "src/api/generated",
    "src/components/ui",
  ]),
  tseslint.configs.strictTypeChecked,
  tseslint.configs.stylisticTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "@typescript-eslint/no-explicit-any": "error",
    },
  },
  reactHooks.configs.flat.recommended,
  pluginRouter.configs["flat/recommended"],
  pluginQuery.configs["flat/recommended"],
  eslintConfigPrettier,
);
