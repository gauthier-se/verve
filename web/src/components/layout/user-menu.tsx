import { useMutation } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { ChevronDownIcon, LogOutIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import type { CurrentUser } from "@/api/generated";
import { logoutMutation } from "@/api/generated/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { leaveSession } from "@/lib/session";
import { cn } from "@/lib/utils";

interface UserMenuProps {
  user: CurrentUser;
  className?: string;
}

/**
 * A menu named after the user of the session, with their account and an item
 * to log out. Logging out leaves the session. A logout that fails keeps the
 * menu open with its error, since the session may still be valid.
 */
export function UserMenu({ user, className }: UserMenuProps) {
  const { t, i18n } = useTranslation();
  const router = useRouter();
  const logout = useMutation({
    ...logoutMutation(),
    onSuccess: () => leaveSession(router),
  });

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="lg"
          className={cn("min-h-11 max-w-48 px-3", className)}
        >
          <span className="truncate">{user.username}</span>
          <ChevronDownIcon aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="w-64 max-w-[calc(100vw-2rem)]"
      >
        <DropdownMenuLabel className="flex flex-col gap-1">
          <span className="truncate text-foreground">{user.username}</span>
          {user.email && <span className="truncate">{user.email}</span>}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="min-h-11"
          disabled={logout.isPending}
          onSelect={(event) => {
            // The menu stays open until the app leaves the page.
            event.preventDefault();
            logout.mutate({});
          }}
        >
          <LogOutIcon aria-hidden />
          {t("userMenu.logout")}
        </DropdownMenuItem>
        {logout.isError && (
          <p role="alert" className="px-2 py-1.5 text-sm text-destructive">
            {errorMessage(logout.error, i18n)}
          </p>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
