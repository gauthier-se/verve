// Thin client for the Verve JSON API. Every response is a single envelope
// object ({"dashboards": …} on success, {"error": …} on failure); this module
// unwraps it and turns a non-2xx into a typed ApiError the UI can branch on.

/** ApiError carries the HTTP status and, for a 422, the per-field messages. */
export class ApiError extends Error {
  status: number;
  fields?: Record<string, string>;

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
}

type Method = "GET" | "POST" | "PATCH" | "DELETE";

/** api sends a request to the JSON API and returns the parsed body. A 204
 *  yields undefined. Errors become ApiError, with field messages for a 422. */
export async function api<T = unknown>(
  path: string,
  opts: { method?: Method; body?: unknown } = {},
): Promise<T> {
  const res = await fetch(path, {
    method: opts.method ?? "GET",
    // Same-origin in production (one binary) and behind the Vite dev proxy, so
    // the session cookie rides along without CORS.
    credentials: "same-origin",
    headers: opts.body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const payload = text ? JSON.parse(text) : {};

  if (!res.ok) {
    throw toError(res.status, payload.error);
  }
  return payload as T;
}

function toError(status: number, error: unknown): ApiError {
  // A validation failure is a field→message map; everything else is a string.
  if (error && typeof error === "object") {
    const fields = error as Record<string, string>;
    const first = Object.values(fields)[0] ?? "validation failed";
    return new ApiError(status, first, fields);
  }
  return new ApiError(status, typeof error === "string" ? error : `request failed (${status})`);
}
