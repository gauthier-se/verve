# 0002. TanStack Router without TanStack Start

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

The frontend is a single page app embedded in the Go binary. The backend is Go, so no JavaScript runtime is available in production. TanStack Start offers an SPA mode that prerenders a static shell, but its main features (server functions, server routes, SSR) require a JavaScript server.

## Decision

Use TanStack Router on its own, with file-based routing through the `@tanstack/router-plugin` Vite plugin, together with TanStack Query, Table and Form. Vite builds static assets that the Go binary embeds and serves, falling back to `index.html` for client-side routes.

## Consequences

- No unused server-side layer, simpler build and fewer moving parts.
- Fully typed routes, loaders and search params, which is useful to keep metric, period and filters in the URL.
- If server-side rendering ever becomes useful, migrating to Start remains possible because Start is built on the Router.

## Alternatives considered

- **TanStack Start in SPA mode**: works, but carries complexity whose benefits require a JavaScript server we will not run.
- **React Router**: viable, but weaker type safety for search params, and the rest of the stack is TanStack.
