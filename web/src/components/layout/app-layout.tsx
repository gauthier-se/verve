import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { metaQueryOptions } from "@/api/meta";
import { cn } from "@/lib/utils";

const containerClassName = "mx-auto w-full max-w-5xl px-4";

/**
 * The frame of every page: a header with the product name, the page, and a
 * footer with the version of the server once it is known.
 */
export function AppLayout({ children }: { children: ReactNode }) {
  const { data: meta } = useQuery(metaQueryOptions());

  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b">
        <div className={cn(containerClassName, "flex h-14 items-center")}>
          {/* The brand name is not translated. */}
          <Link to="/" className="font-heading text-lg font-semibold">
            Verve
          </Link>
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
