import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import type { CurrentUser } from "@/api/generated";
import {
  getCurrentUserQueryKey,
  updatePreferencesMutation,
} from "@/api/generated/@tanstack/react-query.gen";
import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { browserTimezone } from "@/lib/timezone";

interface TimezoneDialogProps {
  /** The user of the session. */
  user: CurrentUser;
}

/**
 * Asks a user who has no timezone yet to confirm the one of the browser, which
 * they can edit, and stores it as their preference. The user can put it off:
 * the dialog then asks again the next time the app loads, and their days stay
 * in UTC until they confirm one.
 */
export function TimezoneDialog({ user }: TimezoneDialogProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(user.timezone === null);
  const update = useMutation({
    ...updatePreferencesMutation(),
    onSuccess: ({ timezone }) => {
      queryClient.setQueryData<CurrentUser>(
        getCurrentUserQueryKey(),
        (current) => current && { ...current, timezone },
      );
      setOpen(false);
    },
  });
  const form = useAppForm({
    defaultValues: { timezone: browserTimezone() },
    onSubmit: ({ value }) => {
      update.mutate({ body: { timezone: value.timezone } });
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <form
          noValidate
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <DialogHeader>
            <DialogTitle>{t("timezone.title")}</DialogTitle>
            <DialogDescription>{t("timezone.description")}</DialogDescription>
          </DialogHeader>
          {update.isError && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(update.error, i18n)}
            </p>
          )}
          <form.AppField
            name="timezone"
            validators={{
              onChange: ({ value }) =>
                value === "" ? t("timezone.required") : undefined,
            }}
          >
            {(field) => (
              <field.TextField
                label={t("timezone.label")}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            )}
          </form.AppField>
          <DialogFooter>
            <DialogClose asChild>
              <Button
                type="button"
                variant="outline"
                size="lg"
                className="min-h-11"
              >
                {t("timezone.later")}
              </Button>
            </DialogClose>
            <Button
              type="submit"
              size="lg"
              className="min-h-11"
              disabled={update.isPending}
            >
              {t("timezone.confirm")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
