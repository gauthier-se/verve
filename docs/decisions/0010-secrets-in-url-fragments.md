# 0010. Secrets in URL fragments, never in paths or query strings

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

Verve generates links that carry a secret: the first-run setup link, invitation links (valid 72 hours) and password reset links (valid 24 hours). The request log writes the URL path of every request, and the log redaction only looks at attribute keys (see [Observability](../features/platform-observability.md) FR-3 and FR-4), so a secret in a path would be written in clear. Beyond Verve's own log, the path and query string reach the access logs of the reverse proxy in front of Verve (Caddy, Traefik, Nginx Proxy Manager), the browser history and the `Referer` header. The URL fragment (`#...`) is never sent to a server.

## Decision

- Secrets (single-use link tokens, API tokens, session ids, passwords) never appear in the path or query string of a URL that Verve serves or generates.
- A link that carries a secret puts it in the URL fragment, for example `<VERVE_PUBLIC_URL>/invite#<token>`. The SPA reads the secret from the fragment, removes the fragment from the address bar with `history.replaceState` before sending any request, keeps the secret in memory only and sends it to the API in a request body.
- The OIDC callback is the only exception: it receives `code` and `state` in the query string, the default response mode of the Authorization Code flow, and the code is single-use and bound to PKCE.
- The rule covers the URLs that Verve serves or generates. Outbound URLs that Verve calls, such as webhook URLs that carry their secret in the path, are not concerned.

## Consequences

- Neither Verve's request log nor the reverse proxy access logs contain a link token, without relying on redaction.
- API operations that consume a link token take it in the request body, never as a path parameter.
- Opening a link requires JavaScript, since only the SPA reads the fragment. Every page of Verve is part of the SPA already.

## Alternatives considered

- **Redact path wildcards with a sensitive name in the request log** (for example `{token}` in `GET /invite/{token}`): keeps Verve's own log clean, but the token still reaches the reverse proxy access logs, the browser history and the `Referer` header. Rejected as the main rule. It could still be added later as a safety net, in case a secret ends up in a path by mistake.
