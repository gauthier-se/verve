import type { Problem } from "@/api/generated";

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
