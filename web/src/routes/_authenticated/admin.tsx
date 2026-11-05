import { createFileRoute, notFound } from "@tanstack/react-router";

/**
 * The parent of the admin pages. A member gets the not-found page, as for a
 * page that does not exist.
 */
export const Route = createFileRoute("/_authenticated/admin")({
  beforeLoad: ({ context: { user } }) => {
    if (user.role !== "admin") {
      throw notFound();
    }
  },
});
