import { defineConfig } from "@hey-api/openapi-ts";

// Generates the API client and its TanStack Query options from the spec that
// `verve openapi` prints. Run `task generate` rather than this config alone.
export default defineConfig({
  input: "./openapi.json",
  output: {
    path: "src/api/generated",
    // The fetch client runtime, copied into client/ and core/ without a file
    // of its own here, does not compile with exactOptionalPropertyTypes. The
    // files generated from the spec are still type checked, and so is every
    // call to the client.
    header: (ctx) =>
      ctx.file ? ctx.defaultValue : [...ctx.defaultValue, "// @ts-nocheck"],
  },
  plugins: [
    {
      name: "@hey-api/client-fetch",
      runtimeConfigPath: "./src/api/client",
    },
    "@tanstack/react-query",
  ],
});
