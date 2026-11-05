import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ChevronDownIcon, LogOutIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

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
import { cn } from "@/lib/utils";

interface UserMenuProps {
  user: CurrentUser;
  className?: string;
}

/**
 * A menu named after the user of the session, with their account and an item
 * to log out. Logging out goes to the login page and clears the query cache,
 * so that no data of the account outlives its session.
 */
export function UserMenu({ user, className }: UserMenuProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const logout = useMutation({
    ...logoutMutation(),
    onSuccess: async () => {
      // Leaving the protected page first stops its queries from refetching.
      await navigate({ to: "/login" });
      queryClient.clear();
    },
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
          onSelect={() => {
            logout.mutate({});
          }}
        >
          <LogOutIcon aria-hidden />
          {t("userMenu.logout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
