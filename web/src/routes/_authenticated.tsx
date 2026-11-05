import { createFileRoute, redirect } from "@tanstack/react-router";

import { isUnauthorized } from "@/api/errors";
import {
  getCurrentUserOptions,
  getSetupStatusOptions,
} from "@/api/generated/@tanstack/react-query.gen";

/**
 * The pathless parent of every route that needs a session. It puts the user
 * of the session in the route context, from the query cache when it holds
 * them, and without a session goes to the login page with the current URL as
 * the target, or to the setup page while the instance has no account.
 */
export const Route = createFileRoute("/_authenticated")({
  beforeLoad: async ({ context: { queryClient }, location }) => {
    try {
      return {
        user: await queryClient.query({
          ...getCurrentUserOptions(),
          staleTime: "static",
        }),
      };
    } catch (error) {
      if (!isUnauthorized(error)) {
        throw error;
      }
    }

    const setup = await queryClient.query(getSetupStatusOptions());
    if (setup.required) {
      throw redirect({ to: "/setup" });
    }
    throw redirect({ to: "/login", search: { redirect: location.href } });
  },
});
