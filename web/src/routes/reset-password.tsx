import { useMutation } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage, hasProblemCode } from "@/api/errors";
import { resetPasswordMutation } from "@/api/generated/@tanstack/react-query.gen";
import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";

/**
 * The password reset page, where a user chooses a new password through the
 * reset link that an admin gave them. The route reads the token from the
 * fragment of the link, outside render, as the setup page does. The token
 * stays in the route context, in memory only. Once the password changed, the
 * page goes to the login page.
 */
export const Route = createFileRoute("/reset-password")({
  beforeLoad: ({ context: { readFragmentSecret } }) => ({
    // Before any request, so that the token leaves the address bar first.
    token: readFragmentSecret(),
  }),
  component: ResetPassword,
});

function ResetPassword() {
  const { t } = useTranslation();
  const { token } = Route.useRouteContext();
  // Whether the reset found the link used, expired or unknown.
  const [gone, setGone] = useState(false);

  return (
    <div className="mx-auto flex w-full max-w-sm flex-col gap-6 py-6">
      <h1 className="font-heading text-2xl font-semibold">
        {t("resetPassword.title")}
      </h1>
      {token === undefined || gone ? (
        <InvalidLink />
      ) : (
        <PasswordForm
          token={token}
          onGone={() => {
            setGone(true);
          }}
        />
      )}
    </div>
  );
}

/** Says that the link is not valid, and leads to the login page. */
function InvalidLink() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-4">
      <p role="alert" className="text-sm text-destructive">
        {t("errors.reset_link_invalid")}
      </p>
      <Button asChild variant="outline" size="lg" className="min-h-11">
        <Link to="/login">{t("resetPassword.goToLogin")}</Link>
      </Button>
    </div>
  );
}

interface PasswordFormProps {
  /** The token of the reset link. */
  token: string;
  /** Called when the server says that the link is no longer valid. */
  onGone: () => void;
}

/**
 * Asks the new password twice and resets the password with it, which revokes
 * every session of the user. The login page then opens.
 */
function PasswordForm({ token, onGone }: PasswordFormProps) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const reset = useMutation({
    ...resetPasswordMutation(),
    onSuccess: async () => {
      await navigate({
        to: "/login",
        search: { passwordChanged: true },
        replace: true,
      });
    },
    onError: (error) => {
      if (hasProblemCode(error, "reset_link_invalid")) {
        onGone();
      }
    },
  });
  const form = useAppForm({
    defaultValues: { password: "", confirmation: "" },
    onSubmit: ({ value }) => {
      reset.mutate({ body: { token, password: value.password } });
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
      <p className="text-muted-foreground">{t("resetPassword.description")}</p>
      {reset.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(reset.error, i18n)}
        </p>
      )}
      <form.AppField
        name="password"
        validators={{
          onChange: ({ value }) =>
            value === "" ? t("resetPassword.passwordRequired") : undefined,
        }}
      >
        {(field) => (
          <field.TextField
            label={t("resetPassword.password")}
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
              : t("resetPassword.passwordMismatch"),
        }}
      >
        {(field) => (
          <field.TextField
            label={t("resetPassword.confirmation")}
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
        disabled={reset.isPending}
      >
        {t("resetPassword.submit")}
      </Button>
    </form>
  );
}
