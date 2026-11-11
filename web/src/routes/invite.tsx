import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage, hasProblemCode } from "@/api/errors";
import { inspectInvitation } from "@/api/generated";
import { acceptInvitationMutation } from "@/api/generated/@tanstack/react-query.gen";
import { useAppForm } from "@/components/form/app-form";
import { InvalidLink } from "@/components/form/invalid-link";
import { NewPasswordFields } from "@/components/form/new-password-fields";
import { Button } from "@/components/ui/button";
import { enterSession } from "@/lib/session";

/**
 * The invitation page, where the invitee chooses the password of the account
 * that an admin invited them to. The route reads the token from the fragment
 * of the link, outside render, as the setup page does, and inspects the
 * invitation before the page shows. The token stays in the route context, in
 * memory only.
 */
export const Route = createFileRoute("/invite")({
  beforeLoad: ({ context: { readFragmentSecret } }) => ({
    // Before any request, so that the token leaves the address bar first.
    token: readFragmentSecret(),
  }),
  loader: async ({ context: { token } }) => {
    if (token === undefined) {
      return { invitation: undefined };
    }
    try {
      const { data } = await inspectInvitation({
        body: { token },
        throwOnError: true,
      });
      return { invitation: data };
    } catch (error) {
      if (hasProblemCode(error, "invitation_invalid")) {
        return { invitation: undefined };
      }
      throw error;
    }
  },
  component: Invite,
});

function Invite() {
  const { t } = useTranslation();
  const { token } = Route.useRouteContext();
  const { invitation } = Route.useLoaderData();
  // Whether accepting found the invitation used or expired meanwhile.
  const [gone, setGone] = useState(false);

  return (
    <div className="mx-auto flex w-full max-w-sm flex-col gap-6 py-6">
      <h1 className="font-heading text-2xl font-semibold">
        {t("invite.title")}
      </h1>
      {token === undefined || invitation === undefined || gone ? (
        <InvalidLink
          message={t("errors.invitation_invalid")}
          loginLabel={t("invite.goToLogin")}
        />
      ) : (
        <PasswordForm
          token={token}
          username={invitation.username}
          onGone={() => {
            setGone(true);
          }}
        />
      )}
    </div>
  );
}

interface PasswordFormProps {
  /** The token of the invitation link. */
  token: string;
  /** The username of the account that the invitation creates. */
  username: string;
  /** Called when the server says that the invitation is no longer valid. */
  onGone: () => void;
}

/**
 * Asks the password twice and accepts the invitation with it, which creates
 * the account and opens its session, as a login does. The app then opens.
 */
function PasswordForm({ token, username, onGone }: PasswordFormProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const accept = useMutation({
    ...acceptInvitationMutation(),
    onSuccess: async (user) => {
      enterSession(queryClient, user);
      await navigate({ to: "/", replace: true });
    },
    onError: (error) => {
      if (hasProblemCode(error, "invitation_invalid")) {
        onGone();
      }
    },
  });
  const form = useAppForm({
    defaultValues: { password: "", confirmation: "" },
    onSubmit: ({ value }) => {
      accept.mutate({ body: { token, password: value.password } });
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
      <p className="text-muted-foreground">
        {t("invite.description", { username })}
      </p>
      {accept.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(accept.error, i18n)}
          {(hasProblemCode(accept.error, "username_taken") ||
            hasProblemCode(accept.error, "email_taken")) &&
            ` ${t("invite.askNewInvitation")}`}
        </p>
      )}
      <NewPasswordFields
        form={form}
        fields={{ password: "password", confirmation: "confirmation" }}
        passwordLabel={t("invite.password")}
        confirmationLabel={t("invite.confirmation")}
        requiredMessage={t("invite.passwordRequired")}
        mismatchMessage={t("invite.passwordMismatch")}
      />
      <Button
        type="submit"
        size="lg"
        className="min-h-11"
        disabled={accept.isPending}
      >
        {t("invite.submit")}
      </Button>
    </form>
  );
}
