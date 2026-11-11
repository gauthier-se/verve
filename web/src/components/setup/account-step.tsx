import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { errorMessage, hasProblemCode } from "@/api/errors";
import { createFirstAdminMutation } from "@/api/generated/@tanstack/react-query.gen";
import { useAppForm } from "@/components/form/app-form";
import { NewPasswordFields } from "@/components/form/new-password-fields";
import { Button } from "@/components/ui/button";
import { enterSession } from "@/lib/session";

/** What the user entered for the admin account. */
export interface AccountEntry {
  username: string;
  password: string;
}

interface AccountStepProps {
  /** The setup token, from the link or entered by the user. */
  token: string;
  /** What the user entered before, when the step comes back. */
  entry?: AccountEntry | undefined;
  /** Called once the admin exists, with their session open. */
  onCreated: () => void;
  /** Called when the server rejects the token, with what the user entered. */
  onTokenRejected: (entry: AccountEntry) => void;
}

/**
 * Creates the first admin with the setup token, which opens their session as
 * a login does.
 */
export function AccountStep({
  token,
  entry,
  onCreated,
  onTokenRejected,
}: AccountStepProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const create = useMutation({
    ...createFirstAdminMutation(),
    onSuccess: (user) => {
      enterSession(queryClient, user);
      onCreated();
    },
    onError: (error, { body }) => {
      if (hasProblemCode(error, "setup_token_invalid")) {
        onTokenRejected({ username: body.username, password: body.password });
      }
    },
  });
  const form = useAppForm({
    defaultValues: {
      username: entry?.username ?? "",
      password: entry?.password ?? "",
      confirmation: entry?.password ?? "",
    },
    onSubmit: ({ value }) => {
      create.mutate({
        body: { token, username: value.username, password: value.password },
      });
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
      <p className="text-muted-foreground">{t("setup.account.description")}</p>
      {create.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(create.error, i18n)}
        </p>
      )}
      {hasProblemCode(create.error, "setup_completed") && (
        <Button asChild variant="outline" size="lg" className="min-h-11">
          <Link to="/login">{t("setup.account.goToLogin")}</Link>
        </Button>
      )}
      <form.AppField
        name="username"
        validators={{
          onChange: ({ value }) =>
            value === "" ? t("setup.account.usernameRequired") : undefined,
        }}
      >
        {(field) => (
          <field.TextField
            label={t("setup.account.username")}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            maxLength={64}
          />
        )}
      </form.AppField>
      <NewPasswordFields
        form={form}
        fields={{ password: "password", confirmation: "confirmation" }}
        passwordLabel={t("setup.account.password")}
        confirmationLabel={t("setup.account.confirmation")}
        requiredMessage={t("setup.account.passwordRequired")}
        mismatchMessage={t("setup.account.passwordMismatch")}
      />
      <Button
        type="submit"
        size="lg"
        className="min-h-11"
        disabled={create.isPending}
      >
        {t("setup.account.submit")}
      </Button>
    </form>
  );
}
