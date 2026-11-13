import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import {
  cancelImportMutation,
  getImportQueryKey,
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

interface CancelImportDialogProps {
  importId: number;
}

/**
 * The cancel action of a queued or running import, behind a confirmation.
 * The confirmation stays open until the import is cancelled and the lists of
 * imports are fresh, or with the error of a cancellation that failed.
 */
export function CancelImportDialog({ importId }: CancelImportDialogProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const cancel = useMutation({
    ...cancelImportMutation(),
    onSuccess: async (imp) => {
      queryClient.setQueryData(
        getImportQueryKey({ path: { id: importId } }),
        imp,
      );
      // The page of imports and the banner, whatever their parameters.
      await queryClient.invalidateQueries({
        queryKey: [{ _id: "listImports" }],
      });
      setOpen(false);
    },
  });

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          cancel.reset();
        }
      }}
    >
      <AlertDialogTrigger asChild>
        <Button variant="destructive" size="lg" className="min-h-11">
          {t("imports.cancel.action")}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("imports.cancel.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("imports.cancel.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {cancel.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(cancel.error, i18n)}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel size="lg" className="min-h-11">
            {t("imports.cancel.keep")}
          </AlertDialogCancel>
          <Button
            variant="destructive"
            size="lg"
            className="min-h-11"
            disabled={cancel.isPending}
            onClick={() => {
              cancel.mutate({ path: { id: importId } });
            }}
          >
            {t("imports.cancel.confirm")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
