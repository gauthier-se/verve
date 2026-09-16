import * as React from "react";
import { useRegister } from "@/hooks/use-auth";
import { useCountdown } from "@/hooks/use-countdown";
import { classifyAuthError, formatWait } from "@/lib/auth-error";
import { AuthScreen, PasswordField } from "./auth-screen";
import { Button } from "./ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "./ui/field";
import { Input } from "./ui/input";

/** RegisterPage is the first-run bootstrap screen, shown only while the instance
 *  has no Account (ADR 0017). It creates the admin Account and, via the auto-login,
 *  the primed `me` cache flips the app straight to the seeded dashboard. It shares
 *  the login screen's shell and its failure vocabulary, plus one of its own: a 409
 *  means someone else initialized this instance first. */
export function RegisterPage() {
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const register = useRegister();

  const failure = classifyAuthError(register.error);
  const wait = useCountdown(
    failure?.kind === "throttled" ? failure.retryAfter : null,
    register.submittedAt,
  );
  const throttled = wait > 0;

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (throttled) return;
    register.mutate({ email, password });
  };

  const fieldError = (name: string) =>
    failure?.kind === "fields" ? failure.fields[name] : undefined;
  const formError =
    failure && failure.kind !== "fields"
      ? failure.kind === "throttled"
        ? throttled
          ? `Too many attempts. Try again in ${formatWait(wait)}.`
          : "Too many attempts. You can try again now."
        : failure.message
      : null;

  return (
    <AuthScreen
      title="Welcome to Verve"
      description="Create your account to set up this instance"
    >
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
            autoComplete="new-password"
            error={fieldError("password")}
          />

          <Field>
            <FieldError>{formError}</FieldError>
            <Button type="submit" disabled={register.isPending || throttled}>
              {register.isPending
                ? "Creating account…"
                : throttled
                  ? `Wait ${formatWait(wait)}`
                  : "Create account"}
            </Button>
            <FieldDescription>
              This is the only account the web can create. Further accounts are added with
              <code className="px-1">verve account create</code>.
            </FieldDescription>
          </Field>
        </FieldGroup>
      </form>
    </AuthScreen>
  );
}
