import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { errorMessage, hasProblemCode } from "@/api/errors";
import {
  createFirstAdminMutation,
  getCurrentUserQueryKey,
} from "@/api/generated/@tanstack/react-query.gen";
import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";

interface AccountStepProps {
  /** The setup token, from the link or entered by the user. */
  token: string;
  /** Called once the admin exists, with their session open. */
  onCreated: () => void;
  /** Called when the server rejects the token. */
  onTokenRejected: () => void;
}

/**
 * Creates the first admin with the setup token, which opens their session.
 * The user of the session goes into the query cache, as after a login.
 */
export function AccountStep({
  token,
  onCreated,
  onTokenRejected,
}: AccountStepProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const create = useMutation({
    ...createFirstAdminMutation(),
    onSuccess: (user) => {
      queryClient.removeQueries();
      queryClient.setQueryData(getCurrentUserQueryKey(), user);
      onCreated();
    },
    onError: (error) => {
      if (hasProblemCode(error, "setup_token_invalid")) {
        onTokenRejected();
      }
    },
  });
  const form = useAppForm({
    defaultValues: { username: "", password: "", confirmation: "" },
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
      <form.AppField
        name="password"
        validators={{
          onChange: ({ value }) =>
            value === "" ? t("setup.account.passwordRequired") : undefined,
        }}
      >
        {(field) => (
          <field.TextField
            label={t("setup.account.password")}
            type="password"
            autoComplete="new-password"
            maxLength={128}
          />
        )}
      </form.AppField>
      <form.AppField
        name="confirmation"
        validators={{
          onChangeListenTo: ["password"],
          onChange: ({ value, fieldApi }) =>
            value === fieldApi.form.getFieldValue("password")
              ? undefined
              : t("setup.account.passwordMismatch"),
        }}
      >
        {(field) => (
          <field.TextField
            label={t("setup.account.confirmation")}
            type="password"
            autoComplete="new-password"
            maxLength={128}
          />
        )}
      </form.AppField>
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
