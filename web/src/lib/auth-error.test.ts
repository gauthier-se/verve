import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api";
import { classifyAuthError, formatWait } from "@/lib/auth-error";

describe("classifyAuthError", () => {
  it("has nothing to say without an error", () => {
    expect(classifyAuthError(null)).toBeNull();
    expect(classifyAuthError(undefined)).toBeNull();
  });

  it("names a refused login as credentials, never as a server fault", () => {
    const failure = classifyAuthError(new ApiError(401, "invalid email or password"));
    expect(failure?.kind).toBe("credentials");
  });

  it("carries the server's Retry-After on a throttled attempt", () => {
    const err = new ApiError(429, "too many requests");
    err.retryAfter = 45;
    const failure = classifyAuthError(err);
    expect(failure).toMatchObject({ kind: "throttled", retryAfter: 45 });
  });

  it("falls back to the limiter's refill when the 429 carries no header", () => {
    const failure = classifyAuthError(new ApiError(429, "too many requests"));
    expect(failure).toMatchObject({ kind: "throttled", retryAfter: 12 });
  });

  it("tells an unreachable server apart from a refused password", () => {
    expect(classifyAuthError(new ApiError(0, "cannot reach the server"))?.kind).toBe("offline");
  });

  it("keeps the per-field messages of a validation failure", () => {
    const err = new ApiError(422, "must be provided", { email: "must be provided" });
    expect(classifyAuthError(err)).toMatchObject({
      kind: "fields",
      fields: { email: "must be provided" },
    });
  });

  it("reads a 409 as a closed bootstrap", () => {
    expect(classifyAuthError(new ApiError(409, "signup is closed"))?.kind).toBe("closed");
  });

  it("reads a 500 as the server's problem", () => {
    expect(classifyAuthError(new ApiError(500, "boom"))?.kind).toBe("server");
  });

  it("does not assume an ApiError", () => {
    expect(classifyAuthError(new Error("kaboom"))?.kind).toBe("server");
  });
});

describe("formatWait", () => {
  it("counts in seconds under a minute", () => {
    expect(formatWait(45)).toBe("45s");
    expect(formatWait(0.2)).toBe("1s");
  });

  it("switches to whole minutes past a minute", () => {
    expect(formatWait(93)).toBe("2 min");
  });

  it("never counts below zero", () => {
    expect(formatWait(-3)).toBe("0s");
  });
});
