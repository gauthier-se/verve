import type { i18n } from "i18next";

import type { Problem } from "@/api/generated";
import type Resources from "@/i18n/resources";
import { formatDuration } from "@/i18n/format";

/**
 * What an API error means for the app: the server cannot be reached, the
 * server is starting, or anything else.
 */
export type ApiErrorKind = "unreachable" | "starting" | "other";

/**
 * The code of the problem that the API client makes of an error response
 * that is not a problem, such as the error page of a reverse proxy.
 */
export const unexpectedResponseCode = "unexpected_response";

/**
 * Classifies an error thrown by the generated API client. The server is
 * unreachable when no response arrives (fetch rejects with a TypeError), when
 * a reverse proxy in front of it answers 502 or 504, or when a 503 does not
 * come from Verve, as a proxy answers when its backend is down. It is starting
 * when it answers 503 with the code "starting", while it applies its
 * migrations.
 */
export function classifyApiError(error: unknown): ApiErrorKind {
  if (error instanceof TypeError) {
    return "unreachable";
  }
  if (!isProblem(error)) {
    return "other";
  }
  if (error.status === 502 || error.status === 504) {
    return "unreachable";
  }
  if (error.status === 503) {
    if (error.code === "starting") {
      return "starting";
    }
    if (error.code === unexpectedResponseCode) {
      return "unreachable";
    }
  }
  return "other";
}

/**
 * Tells whether an error thrown by the API client is a 401, which the server
 * answers to a request without a valid session.
 */
export function isUnauthorized(error: unknown): boolean {
  return isProblem(error) && error.status === 401;
}

/** Tells whether an error thrown by the API client is a problem of `code`. */
export function hasProblemCode(error: unknown, code: string): boolean {
  return isProblem(error) && error.code === code;
}

/**
 * Adds to a problem the wait of the Retry-After header of its response, as
 * `retryAfter` in seconds, when the header gives it in seconds. Any other
 * error is returned unchanged.
 */
export function withRetryAfter(
  error: unknown,
  response: Response | undefined,
): unknown {
  const header = response?.headers.get("Retry-After");
  const seconds = header ? Number(header) : Number.NaN;
  if (!isProblem(error) || !Number.isFinite(seconds)) {
    return error;
  }
  return { ...error, retryAfter: seconds };
}

type ErrorCode = keyof Resources["common"]["errors"];

/**
 * The message of an error thrown by the API client, translated from the code
 * of its problem, with the wait for `rate_limited` when its response gave one.
 * A code without a text of its own, or an error that is not a problem, such as
 * a network failure, gets a generic message.
 */
export function errorMessage(error: unknown, i18n: i18n): string {
  if (!isProblem(error)) {
    return i18n.t("unexpectedError");
  }
  if (
    error.code === "rate_limited" &&
    "retryAfter" in error &&
    typeof error.retryAfter === "number"
  ) {
    return i18n.t("errors.rate_limited_wait", {
      wait: formatDuration(error.retryAfter, i18n.language),
    });
  }
  return hasMessage(error.code, i18n)
    ? i18n.t(`errors.${error.code}`)
    : i18n.t("unexpectedError");
}

function hasMessage(code: string, i18n: i18n): code is ErrorCode {
  return i18n.exists(`errors.${code}`);
}

function isProblem(error: unknown): error is Pick<Problem, "status" | "code"> {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    typeof error.status === "number" &&
    "code" in error &&
    typeof error.code === "string"
  );
}
