import { Link } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";

interface InvalidLinkProps {
  /** Why the link is not valid, translated. */
  message: string;
  /** The text of the link to the login page, translated. */
  loginLabel: string;
}

/**
 * Says that the link that opened the page is not valid, and leads to the
 * login page.
 */
export function InvalidLink({ message, loginLabel }: InvalidLinkProps) {
  return (
    <div className="flex flex-col gap-4">
      <p role="alert" className="text-sm text-destructive">
        {message}
      </p>
      <Button asChild variant="outline" size="lg" className="min-h-11">
        <Link to="/login">{loginLabel}</Link>
      </Button>
    </div>
  );
}
