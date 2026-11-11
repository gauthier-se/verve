import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import type { ApiToken } from "@/api/generated";
import {
  listTokensQueryKey,
  revokeTokenMutation,
} from "@/api/generated/@tanstack/react-query.gen";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";

interface RevokeTokenDialogProps {
  token: ApiToken;
}

/**
 * The revoke action of a token, behind a confirmation. The confirmation stays
 * open until the token is revoked and the list of tokens is fresh, or with the
 * error of a revocation that failed.
 */
export function RevokeTokenDialog({ token }: RevokeTokenDialogProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const revoke = useMutation({
    ...revokeTokenMutation(),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: listTokensQueryKey() });
      setOpen(false);
    },
  });

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          revoke.reset();
        }
      }}
    >
      <AlertDialogTrigger asChild>
        <Button
          variant="outline"
          size="lg"
          className="min-h-11"
          aria-label={t("tokens.revoke.label", { name: token.name })}
        >
          {t("tokens.revoke.action")}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="break-words">
            {t("tokens.revoke.title", { name: token.name })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("tokens.revoke.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {revoke.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(revoke.error, i18n)}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel size="lg" className="min-h-11">
            {t("tokens.revoke.cancel")}
          </AlertDialogCancel>
          <Button
            variant="destructive"
            size="lg"
            className="min-h-11"
            disabled={revoke.isPending}
            onClick={() => {
              revoke.mutate({ path: { id: token.id } });
            }}
          >
            {t("tokens.revoke.confirm")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
