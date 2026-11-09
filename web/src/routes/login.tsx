import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { loginMutationOptions } from "@/api/auth";
import { errorMessage } from "@/api/errors";
import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";
import { enterSession } from "@/lib/session";

interface LoginSearch {
  /** The URL of the app to go to after login. */
  redirect?: string | undefined;
  /** Whether the user just changed their password through a reset link. */
  passwordChanged?: true | undefined;
}

export const Route = createFileRoute("/login")({
  // The router merges the result over the raw search: a rejected target must
  // be replaced with undefined, not left out.
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    redirect: appPath(search.redirect),
    passwordChanged: search.passwordChanged === true ? true : undefined,
  }),
  component: Login,
});

/**
 * Returns the target when it is a URL of the app given by its path, such as
 * "/imports?page=2", and undefined for anything else, such as a URL of another
 * origin, so that the login page cannot send to another site.
 */
function appPath(target: unknown): string | undefined {
  if (typeof target !== "string" || !target.startsWith("/")) {
    return undefined;
  }
  const { origin } = window.location;
  // "//host" and "/\host" name another origin.
  const url = new URL(target, origin);
  return url.origin === origin
    ? `${url.pathname}${url.search}${url.hash}`
    : undefined;
}

function Login() {
  const { t, i18n } = useTranslation();
  const { redirect, passwordChanged } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const login = useMutation({
    ...loginMutationOptions(),
    onSuccess: async (user) => {
      enterSession(queryClient, user);
      await navigate({ href: redirect ?? "/", replace: true });
    },
  });
  const form = useAppForm({
    defaultValues: { login: "", password: "" },
    onSubmit: ({ value }) => {
      login.mutate(value);
    },
  });

  return (
    <div className="mx-auto flex w-full max-w-sm flex-col gap-6 py-6">
      <h1 className="font-heading text-2xl font-semibold">
        {t("login.title")}
      </h1>
      <form
        noValidate
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          void form.handleSubmit();
        }}
      >
        {passwordChanged && !login.isError && (
          <p role="status" className="text-sm text-muted-foreground">
            {t("login.passwordChanged")}
          </p>
        )}
        {login.isError && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(login.error, i18n)}
          </p>
        )}
        <form.AppField
          name="login"
          validators={{
            onChange: ({ value }) =>
              value === "" ? t("login.usernameOrEmailRequired") : undefined,
          }}
        >
          {(field) => (
            <field.TextField
              label={t("login.usernameOrEmail")}
              autoComplete="username"
              autoCapitalize="none"
              spellCheck={false}
              maxLength={254}
            />
          )}
        </form.AppField>
        <form.AppField
          name="password"
          validators={{
            onChange: ({ value }) =>
              value === "" ? t("login.passwordRequired") : undefined,
          }}
        >
          {(field) => (
            <field.TextField
              label={t("login.password")}
              type="password"
              autoComplete="current-password"
              maxLength={128}
            />
          )}
        </form.AppField>
        <Button
          type="submit"
          size="lg"
          className="min-h-11"
          disabled={login.isPending}
        >
          {t("login.submit")}
        </Button>
      </form>
    </div>
  );
}
