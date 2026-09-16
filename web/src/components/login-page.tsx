import * as React from "react";
import { useLogin } from "@/hooks/use-auth";
import { useCountdown } from "@/hooks/use-countdown";
import { classifyAuthError, formatWait } from "@/lib/auth-error";
import { AuthScreen, PasswordField } from "./auth-screen";
import { Button } from "./ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "./ui/field";
import { Input } from "./ui/input";

/** LoginPage is the unauthenticated entry point: it posts credentials and, on
 *  success, the primed `me` cache flips the app to the dashboards. A failure is
 *  never just "it didn't work": the screen distinguishes wrong credentials from a
 *  throttled client, an unreachable server and a broken one, because only the
 *  first is worth retyping a password over. */
export function LoginPage() {
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const login = useLogin();

  const failure = classifyAuthError(login.error);
  // The wait restarts on every submission, since two throttled attempts in a row
  // ask for the same duration and would otherwise leave a frozen countdown.
  const wait = useCountdown(failure?.kind === "throttled" ? failure.retryAfter : null, login.submittedAt);
  const throttled = wait > 0;

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (throttled) return;
    login.mutate({ email, password });
  };

  const fieldError = (name: string) =>
    failure?.kind === "fields" ? failure.fields[name] : undefined;
  // A refused login is about the pair, not either field, so it is stated once
  // under the form rather than blamed on the email.
  const formError =
    failure && failure.kind !== "fields"
      ? failure.kind === "throttled"
        ? throttled
          ? `Too many attempts. Try again in ${formatWait(wait)}.`
          : "Too many attempts. You can try again now."
        : failure.message
      : null;

  return (
    <AuthScreen title="Welcome back to Verve" description="Sign in to your health data">
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="email">Email</FieldLabel>
            <Input
              id="email"
              type="email"
              placeholder="you@example.com"
              autoComplete="username"
              required
              aria-invalid={fieldError("email") ? true : undefined}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <FieldError>{fieldError("email")}</FieldError>
          </Field>

          <PasswordField
            value={password}
            onChange={setPassword}
            autoComplete="current-password"
            error={fieldError("password")}
          />

          <Field>
            <FieldError>{formError}</FieldError>
            <Button type="submit" disabled={login.isPending || throttled}>
              {login.isPending ? "Signing in…" : throttled ? `Wait ${formatWait(wait)}` : "Sign in"}
            </Button>
          </Field>
        </FieldGroup>
      </form>
    </AuthScreen>
  );
}
