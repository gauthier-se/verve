import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";
import { browserTimezone, useUpdateTimezone } from "@/lib/timezone";

interface TimezoneStepProps {
  /** Called once the timezone is stored. */
  onDone: () => void;
}

/**
 * Proposes the timezone of the browser, which the admin can edit, and stores
 * it as their preference, so that the app does not ask for it again.
 */
export function TimezoneStep({ onDone }: TimezoneStepProps) {
  const { t, i18n } = useTranslation();
  const update = useUpdateTimezone(onDone);
  const form = useAppForm({
    defaultValues: { timezone: browserTimezone() },
    onSubmit: ({ value }) => {
      update.mutate({ body: { timezone: value.timezone } });
    },
  });

  return (
    <form
      noValidate
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <p className="text-muted-foreground">{t("timezone.description")}</p>
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
      <Button
        type="submit"
        size="lg"
        className="min-h-11"
        disabled={update.isPending}
      >
        {t("setup.continue")}
      </Button>
    </form>
  );
}
