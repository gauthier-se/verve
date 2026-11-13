import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { CurrentUser } from "@/api/generated";
import { metaQueryOptions } from "@/api/meta";
import { UserMenu } from "@/components/layout/user-menu";
import { cn } from "@/lib/utils";

const containerClassName = "mx-auto w-full max-w-5xl px-4";

const navLinkClassName =
  "inline-flex min-h-11 items-center rounded-lg px-3 text-sm font-medium text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 data-[status=active]:text-foreground";

interface AppLayoutProps {
  /** The user of the session, on the pages that need one. */
  user?: CurrentUser | undefined;
  children: ReactNode;
}

/**
 * The frame of every page: a header with the product name, the page, and a
 * footer with the version of the server once it is known. With the user of a
 * session, the header also shows the navigation, with the admin section for
 * an admin, and the user menu.
 */
export function AppLayout({ user, children }: AppLayoutProps) {
  const { data: meta } = useQuery(metaQueryOptions());

  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b">
        <div
          className={cn(
            containerClassName,
            "flex min-h-14 flex-wrap items-center gap-x-4",
          )}
        >
          {/* The brand name is not translated. */}
          <Link
            to="/"
            className="inline-flex min-h-11 items-center font-heading text-lg font-semibold"
          >
            Verve
          </Link>
          {user && (
            <>
              <Navigation
                user={user}
                // On a phone, the navigation takes a row of its own.
                className="order-last -mx-3 w-[calc(100%+1.5rem)] pb-1 sm:order-none sm:mx-0 sm:w-auto sm:pb-0"
              />
              <UserMenu user={user} className="-mr-3 ml-auto" />
            </>
          )}
        </div>
      </header>
      <main className={cn(containerClassName, "flex-1 py-6")}>{children}</main>
      <footer className="border-t">
        <p
          className={cn(
            containerClassName,
            "py-4 text-sm text-muted-foreground",
          )}
        >
          Verve{meta && ` ${meta.version}`}
        </p>
      </footer>
    </div>
  );
}

function Navigation({
  user,
  className,
}: {
  user: CurrentUser;
  className: string;
}) {
  const { t } = useTranslation();

  return (
    <div className={cn("flex flex-wrap items-center gap-x-1", className)}>
      <nav aria-label={t("nav.main")}>
        <ul className="flex flex-wrap">
          <li>
            <Link
              to="/"
              activeOptions={{ exact: true }}
              className={navLinkClassName}
            >
              {t("nav.home")}
            </Link>
          </li>
          <li>
            <Link to="/imports" className={navLinkClassName}>
              {t("nav.imports")}
            </Link>
          </li>
        </ul>
      </nav>
      {user.role === "admin" && (
        <nav aria-label={t("nav.admin")} className="border-l pl-1">
          <ul className="flex flex-wrap">
            <li>
              <Link to="/admin" className={navLinkClassName}>
                {t("nav.admin")}
              </Link>
            </li>
          </ul>
        </nav>
      )}
    </div>
  );
}
