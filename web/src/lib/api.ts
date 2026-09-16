// Thin client for the Verve JSON API. Every response is a single envelope
// object ({"dashboards": …} on success, {"error": …} on failure); this module
// unwraps it and turns a non-2xx into a typed ApiError the UI can branch on.

/** ApiError carries the HTTP status and, for a 422, the per-field messages. A
 *  request that never reached the server gets status 0, which is a different
 *  answer from any status the server could have sent: the UI can say "unreachable"
 *  rather than blaming the credentials. */
export class ApiError extends Error {
  status: number;
  fields?: Record<string, string>;
  /** retryAfter is the server's Retry-After in seconds, present on a throttled 429. */
  retryAfter?: number;

  constructor(status: number, message: string, fields?: Record<string, string>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fields = fields;
  }

  /** unauthenticated is the signal the app uses to fall back to the login screen. */
  get unauthenticated() {
    return this.status === 401;
  }

  /** offline means the fetch itself never got an answer: no server, no network. */
  get offline() {
    return this.status === 0;
  }
}

type Method = "GET" | "POST" | "PATCH" | "DELETE";

/** api sends a request to the JSON API and returns the parsed body. A 204
 *  yields undefined. Errors become ApiError, with field messages for a 422. */
export async function api<T = unknown>(
  path: string,
  opts: { method?: Method; body?: unknown } = {},
): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method: opts.method ?? "GET",
      // Same-origin in production (one binary) and behind the Vite dev proxy, so
      // the session cookie rides along without CORS.
      credentials: "same-origin",
      headers: opts.body !== undefined ? { "Content-Type": "application/json" } : undefined,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    });
  } catch {
    // fetch rejects only when the request got no answer at all. Status 0 keeps
    // that apart from every status a server can send, so a caller can tell a
    // dead instance from a refused password.
    throw new ApiError(0, "cannot reach the server");
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const payload = text ? JSON.parse(text) : {};

  if (!res.ok) {
    throw toError(res.status, payload.error, res.headers.get("Retry-After"));
  }
  return payload as T;
}

/** upload POSTs a raw file body (not JSON) to path, unwrapping the envelope and
 *  errors like `api`. The import upload's body is the binary .zip itself; the
 *  server streams it and tracks progress, which the caller polls separately. */
export async function upload<T = unknown>(path: string, file: File): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/zip" },
    body: file,
  });
  const text = await res.text();
  const payload = text ? JSON.parse(text) : {};
  if (!res.ok) throw toError(res.status, payload.error, res.headers.get("Retry-After"));
  return payload as T;
}

function toError(status: number, error: unknown, retryAfter?: string | null): ApiError {
  // A validation failure is a field→message map; everything else is a string.
  let err: ApiError;
  if (error && typeof error === "object") {
    const fields = error as Record<string, string>;
    const first = Object.values(fields)[0] ?? "validation failed";
    err = new ApiError(status, first, fields);
  } else {
    err = new ApiError(status, typeof error === "string" ? error : `request failed (${status})`);
  }

  // Retry-After is whole seconds on a throttled 429 (Verve never sends the
  // HTTP-date form), and is the wait a client should count down.
  const secs = retryAfter ? Number(retryAfter) : NaN;
  if (Number.isFinite(secs) && secs > 0) err.retryAfter = secs;
  return err;
}
