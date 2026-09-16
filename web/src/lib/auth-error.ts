import { ApiError } from "@/lib/api";

/** AuthFailure is what went wrong on a sign-in attempt, as the screen needs to
 *  say it. The server answers a refused login with a deliberately generic 401
 *  (it never reveals whether the email exists, ADR 0017), so the useful
 *  distinctions are between the *kinds* of failure, not between accounts:
 *  wrong credentials, throttled, unreachable, or broken server. */
export type AuthFailure =
  | { kind: "credentials"; message: string }
  | { kind: "throttled"; message: string; retryAfter: number }
  | { kind: "offline"; message: string }
  | { kind: "server"; message: string }
  | { kind: "closed"; message: string }
  | { kind: "fields"; message: string; fields: Record<string, string> };

/** defaultRetryAfter is the wait assumed when a 429 arrives without the header:
 *  the limiter's own refill interval, so the countdown is honest rather than
 *  arbitrary. */
const defaultRetryAfter = 12;

/** classifyAuthError turns whatever a login or bootstrap mutation threw into the
 *  one thing the screen has to render. It returns null for no error. */
export function classifyAuthError(err: unknown): AuthFailure | null {
  if (!err) return null;
  if (!(err instanceof ApiError)) {
    return { kind: "server", message: "Something went wrong" };
  }
  if (err.offline) {
    return { kind: "offline", message: "Can't reach this Verve instance. Check that the server is running." };
  }
  if (err.status === 429) {
    return {
      kind: "throttled",
      message: "Too many attempts",
      retryAfter: err.retryAfter ?? defaultRetryAfter,
    };
  }
  if (err.status === 422 && err.fields) {
    return { kind: "fields", message: err.message, fields: err.fields };
  }
  if (err.status === 409) {
    // Bootstrap only: someone else initialized this instance first (ADR 0017).
    return { kind: "closed", message: "This instance is already set up. Sign in instead." };
  }
  if (err.status === 401) {
    return { kind: "credentials", message: "Wrong email or password" };
  }
  if (err.status >= 500) {
    return { kind: "server", message: "The server hit a problem. Try again in a moment." };
  }
  return { kind: "server", message: err.message };
}

/** formatWait renders a countdown in seconds as the screen says it: seconds up
 *  to a minute, then whole minutes, because "in 93s" is worse than "in 2 min". */
export function formatWait(seconds: number): string {
  const s = Math.max(0, Math.ceil(seconds));
  if (s < 60) return `${s}s`;
  return `${Math.ceil(s / 60)} min`;
}
