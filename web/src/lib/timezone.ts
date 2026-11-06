import { useMutation, useQueryClient } from "@tanstack/react-query";

import type { CurrentUser } from "@/api/generated";
import {
  getCurrentUserQueryKey,
  updatePreferencesMutation,
} from "@/api/generated/@tanstack/react-query.gen";

/** The timezone of the browser, as an IANA name such as Europe/Paris. */
export function browserTimezone(): string {
  return new Intl.DateTimeFormat().resolvedOptions().timeZone;
}

/**
 * The mutation that stores the timezone of the user of the session, then
 * writes it into their cached user, so that the app does not ask for it
 * again, and calls `onStored`.
 */
export function useUpdateTimezone(onStored: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    ...updatePreferencesMutation(),
    onSuccess: ({ timezone }) => {
      queryClient.setQueryData<CurrentUser>(
        getCurrentUserQueryKey(),
        (current) => current && { ...current, timezone },
      );
      onStored();
    },
  });
}
