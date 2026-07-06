import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// The SPA is embedded into the Go binary (ADR 0005): the production build is
// written into internal/web/dist, the directory that package's go:embed reads.
// In dev, Vite serves the app and proxies the API to a locally running
// `verve serve` (default :8080), so the browser talks to one origin and the
// session cookie flows normally.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "src") },
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: path.resolve(__dirname, "../internal/web/dist"),
    // Don't wipe the directory: it holds a committed .gitkeep placeholder that
    // keeps go:embed compiling before the first UI build. The `make ui` target
    // clears stale hashed assets/ instead.
    emptyOutDir: false,
  },
});
