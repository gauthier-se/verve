# Run Verve behind a reverse proxy

Verve serves plain HTTP on port 8080 and has no built-in TLS. To reach it over HTTPS, put it behind a reverse proxy. This guide gives complete examples for [Caddy](#caddy), [Traefik](#traefik) and [Nginx Proxy Manager](#nginx-proxy-manager).

> [!WARNING]
> Do not put `/api/v1/ingest/*` behind forward-auth (Authelia, Authentik, oauth2-proxy). Health Auto Export and the other machine clients authenticate with API tokens and cannot pass a login page, so their pushes would fail. See [Forward-auth](#forward-auth).

## What a proxy must do

| Need | Why | Where to set it |
|---|---|---|
| TLS termination | Verve only speaks HTTP | Proxy |
| Forwarded headers | Verve reads `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Forwarded-Host`. The `Forwarded` header is not supported | Proxy, on by default in the three proxies below |
| Trusted proxies | Verve ignores forwarded headers unless the immediate peer is listed, so that clients cannot forge them | `VERVE_TRUSTED_PROXIES` |
| Public URL | Secure cookies, OIDC, CSRF origin checks, absolute links and HSTS depend on it | `VERVE_PUBLIC_URL` |
| Large request bodies | XML imports go up to `VERVE_MAX_UPLOAD_SIZE` (10 GB by default) and pushes up to `VERVE_MAX_INGEST_BODY` (1 GB by default) | Proxy |
| Long timeouts | Large uploads and imports take minutes. Allow at least 300 seconds | Proxy |

The [configuration reference](../features/platform-configuration-and-deployment.md#functional-requirements) (FR-3) describes these variables, and the exact rules for forwarded headers.

### Which address to trust

`VERVE_TRUSTED_PROXIES` takes comma-separated addresses or CIDRs, and must match the address Verve sees the proxy connect from:

| Setup | Value |
|---|---|
| Proxy and Verve in the same Docker network | The CIDR of this network |
| Proxy on the Docker host, Verve in a container | The gateway of the Docker network, usually the `.1` address of its CIDR |
| Proxy and Verve on the same machine, without Docker | `127.0.0.1` |
| Proxy on another machine | The address of this machine |

Find the CIDR of a Docker network with:

```sh
docker network inspect <network> --format '{{(index .IPAM.Config 0).Subnet}}'
```

Docker picks this CIDR when it creates the network, so the examples below set it explicitly to `172.30.0.0/24`, and the value of `VERVE_TRUSTED_PROXIES` stays valid. Pick another range if this one is already in use on your host.

Every address in the trusted range can claim to be any client. The examples therefore have no `ports:` section on the `verve` service: Verve is only reachable through the proxy. If you publish its port as well, connections can reach Verve from the gateway of the network, which belongs to the trusted range, and forge their client IP.

## Caddy

Tested with Caddy 2.11. Caddy obtains a certificate automatically, as soon as `verve.example.com` points to the host and ports 80 and 443 are reachable.

`compose.yaml`:

```yaml
services:
  caddy:
    image: caddy:2
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    networks:
      - proxy

  verve:
    image: ghcr.io/gauthier-se/verve:latest
    restart: unless-stopped
    volumes:
      - verve-data:/data
    environment:
      VERVE_PUBLIC_URL: https://verve.example.com
      VERVE_TRUSTED_PROXIES: 172.30.0.0/24
    networks:
      - proxy

volumes:
  verve-data:
  caddy-data:
  caddy-config:

networks:
  proxy:
    ipam:
      config:
        - subnet: 172.30.0.0/24
```

`Caddyfile`:

```caddy
{
	servers {
		timeouts {
			read_body_idle 1h
			write_idle 1h
		}
	}
}

verve.example.com {
	reverse_proxy verve:8080
}
```

- **TLS termination:** automatic for the site address.
- **Forwarded headers:** set by `reverse_proxy` without configuration. Values sent by clients are dropped.
- **Request bodies:** Caddy has no default limit and does not buffer them.
- **Timeouts:** Caddy limits neither the length of a request nor the wait for the response. It does close a connection that makes no progress for 1 minute, which the two `timeouts` options raise to one hour.

## Traefik

Tested with Traefik 3.7. Traefik reads the configuration of each service from its Docker labels, and obtains a certificate from Let's Encrypt as soon as `verve.example.com` points to the host and port 443 is reachable.

`compose.yaml`:

```yaml
services:
  traefik:
    image: traefik:v3
    restart: unless-stopped
    command:
      - --providers.docker=true
      - --providers.docker.exposedbydefault=false
      - --entrypoints.web.address=:80
      - --entrypoints.web.http.redirections.entrypoint.to=websecure
      - --entrypoints.web.http.redirections.entrypoint.scheme=https
      - --entrypoints.websecure.address=:443
      # Whole time allowed to read a request, body included. The default of 60 seconds cuts uploads.
      - --entrypoints.websecure.transport.respondingtimeouts.readtimeout=1h
      - --certificatesresolvers.letsencrypt.acme.email=you@example.com
      - --certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json
      - --certificatesresolvers.letsencrypt.acme.tlschallenge=true
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - traefik-letsencrypt:/letsencrypt
    networks:
      - proxy

  verve:
    image: ghcr.io/gauthier-se/verve:latest
    restart: unless-stopped
    volumes:
      - verve-data:/data
    environment:
      VERVE_PUBLIC_URL: https://verve.example.com
      VERVE_TRUSTED_PROXIES: 172.30.0.0/24
    labels:
      - traefik.enable=true
      - traefik.http.routers.verve.rule=Host(`verve.example.com`)
      - traefik.http.routers.verve.entrypoints=websecure
      - traefik.http.routers.verve.tls.certresolver=letsencrypt
      - traefik.http.services.verve.loadbalancer.server.port=8080
    networks:
      - proxy

volumes:
  verve-data:
  traefik-letsencrypt:

networks:
  proxy:
    ipam:
      config:
        - subnet: 172.30.0.0/24
```

- **TLS termination:** on the `websecure` entry point, with the `letsencrypt` resolver.
- **Forwarded headers:** set without configuration. Values sent by clients are dropped.
- **Request bodies:** Traefik has no default limit and does not buffer them, unless you add the `buffering` middleware.
- **Timeouts:** the `readTimeout` of an entry point defaults to 60 seconds and covers the whole request, so any upload that lasts longer fails. This is the one setting Traefik needs, and it applies to every service of the entry point. Traefik does not limit the wait for the response.

## Nginx Proxy Manager

Tested with Nginx Proxy Manager 2.16.

`compose.yaml`:

```yaml
services:
  nginx-proxy-manager:
    image: jc21/nginx-proxy-manager:2
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "127.0.0.1:81:81"
    volumes:
      - npm-data:/data
      - npm-letsencrypt:/etc/letsencrypt
    networks:
      - proxy

  verve:
    image: ghcr.io/gauthier-se/verve:latest
    restart: unless-stopped
    volumes:
      - verve-data:/data
    environment:
      VERVE_PUBLIC_URL: https://verve.example.com
      VERVE_TRUSTED_PROXIES: 172.30.0.0/24
    networks:
      - proxy

volumes:
  verve-data:
  npm-data:
  npm-letsencrypt:

networks:
  proxy:
    ipam:
      config:
        - subnet: 172.30.0.0/24
```

Open the admin interface on http://localhost:81 (from another machine, through an SSH tunnel), create the administrator account, then add a proxy host under **Hosts > Proxy Hosts**:

1. **Details:** set the domain name to `verve.example.com`, the scheme to `http`, the forward hostname to `verve` and the forward port to `8080`.
2. **SSL:** request a new certificate, and enable **Force SSL** and **HTTP/2 Support**. Leave **HSTS** off: Verve sends this header itself.
3. **Advanced:** add this custom Nginx configuration.

```nginx
client_max_body_size 0;
proxy_request_buffering off;
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;
client_body_timeout 3600s;
```

- **TLS termination:** on the proxy host, with the requested certificate.
- **Forwarded headers:** set without configuration. A client-sent `X-Forwarded-For` stays on the left of the list, where Verve ignores it. A client-sent `X-Forwarded-Proto` is passed on as is, which only changes the scheme of that client's own requests.
- **Request bodies:** `client_max_body_size 0` removes the limit of 2000 MB, which otherwise fails larger uploads with a 413. `proxy_request_buffering off` streams them to Verve instead of writing a copy to the disk of the proxy first.
- **Timeouts:** the default is 90 seconds, which otherwise fails slow requests with a 504. The three timeouts above raise it to one hour.

## Forward-auth

Exclude `/api/v1/ingest/*` from forward-auth. Scripts and dashboards that call the [public API](../features/public-api.md) with a token cannot pass it either, so exclude the paths they use as well. These examples use Authelia and only show the exclusion: the address, the login redirect and the identity headers of your provider come from its own documentation. Verve only uses these headers once [proxy header authentication](../features/auth-proxy-header.md) ships, in milestone M5.

Caddy, with one `handle` block for each case, as a request only runs the first that matches:

```caddy
verve.example.com {
	handle /api/v1/ingest/* {
		reverse_proxy verve:8080
	}

	handle {
		forward_auth authelia:9091 {
			uri /api/authz/forward-auth
			copy_headers Remote-User Remote-Groups Remote-Email Remote-Name
		}
		reverse_proxy verve:8080
	}
}
```

Traefik, with a second router without the middleware. Its longer rule gives it priority:

```yaml
labels:
  - traefik.enable=true
  - traefik.http.middlewares.authelia.forwardauth.address=http://authelia:9091/api/authz/forward-auth
  - traefik.http.middlewares.authelia.forwardauth.trustForwardHeader=true
  - traefik.http.middlewares.authelia.forwardauth.authResponseHeaders=Remote-User,Remote-Groups,Remote-Email,Remote-Name
  - traefik.http.routers.verve.rule=Host(`verve.example.com`)
  - traefik.http.routers.verve.entrypoints=websecure
  - traefik.http.routers.verve.tls.certresolver=letsencrypt
  - traefik.http.routers.verve.middlewares=authelia
  - traefik.http.routers.verve.service=verve
  - traefik.http.routers.verve-ingest.rule=Host(`verve.example.com`) && PathPrefix(`/api/v1/ingest/`)
  - traefik.http.routers.verve-ingest.entrypoints=websecure
  - traefik.http.routers.verve-ingest.tls.certresolver=letsencrypt
  - traefik.http.routers.verve-ingest.service=verve
  - traefik.http.services.verve.loadbalancer.server.port=8080
```

Nginx Proxy Manager, added to the custom configuration of the proxy host. `auth_request` applies to the whole host, and the ingestion location turns it off:

```nginx
auth_request /internal/authelia;

location = /internal/authelia {
  internal;
  proxy_pass http://authelia:9091/api/authz/auth-request;
  proxy_pass_request_body off;
  proxy_set_header Content-Length "";
  proxy_set_header X-Original-Method $request_method;
  proxy_set_header X-Original-URL $scheme://$http_host$request_uri;
  proxy_set_header X-Forwarded-For $remote_addr;
}

location /api/v1/ingest/ {
  auth_request off;
  include conf.d/include/proxy.conf;
}
```

## Check the setup

Open Verve through the proxy, then look at its logs:

```sh
docker compose logs verve
```

The `client_ip` of each request must be your real address, not the one of the proxy. Then match what you see:

| Symptom | Cause | Fix |
|---|---|---|
| The log has `ignoring X-Forwarded-* headers from an untrusted peer`, with the address of the proxy as `peer` | The proxy is missing from the trusted list | Add this address to `VERVE_TRUSTED_PROXIES` |
| `client_ip` is the address of the proxy, without that warning | The proxy sends no `X-Forwarded-For` | Check the configuration of the proxy |
| The startup log has `VERVE_PUBLIC_URL is not set` | The public URL is missing | Set `VERVE_PUBLIC_URL` |
| Uploads fail with a 413 | The proxy limits the body size | Remove the limit: see the section of your proxy |
| Uploads fail with a 502 after 60 seconds on Traefik | The `readTimeout` of the entry point is too low | Raise it as in the [example](#traefik) |
| Requests fail with a 504 after 90 seconds on Nginx Proxy Manager | The default timeouts are too low | Raise them as in the [example](#nginx-proxy-manager) |
| Health Auto Export gets a login page or a 401 from the proxy | `/api/v1/ingest/*` is behind forward-auth | Exclude it as in [Forward-auth](#forward-auth) |
