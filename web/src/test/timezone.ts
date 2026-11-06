import { http, HttpResponse } from "msw";
import { vi } from "vitest";

import type { UpdatePreferencesInputBody } from "@/api/generated";
import { server } from "@/test/server";

/** The path of the preferences of the user of the session. */
export const preferencesPath = "/api/v1/account/preferences";

/**
 * Makes the browser report `timeZone` as its timezone, until the test restores
 * its mocks.
 */
export function stubBrowserTimezone(timeZone: string) {
  const options = new Intl.DateTimeFormat().resolvedOptions();
  vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockReturnValue({
    ...options,
    timeZone,
  });
}

/**
 * Answers PATCH /api/v1/account/preferences as the server does for a valid
 * name, and returns the bodies it receives.
 */
export function acceptPreferences() {
  const bodies: UpdatePreferencesInputBody[] = [];
  server.use(
    http.patch<never, UpdatePreferencesInputBody>(
      preferencesPath,
      async ({ request }) => {
        const body = await request.json();
        bodies.push(body);
        return HttpResponse.json({ timezone: body.timezone ?? null });
      },
    ),
  );
  return bodies;
}
