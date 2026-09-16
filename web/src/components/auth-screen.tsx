import * as React from "react";
import { Activity, Eye, EyeOff } from "lucide-react";
import { useAuthState } from "@/hooks/use-auth";
import { cn } from "@/lib/utils";
import { Field, FieldDescription, FieldLabel } from "./ui/field";
import { Input } from "./ui/input";

/** AuthScreen is the shell both unauthenticated screens share: a centered card-less
 *  column with the mark, the greeting, the form, and a footer naming the build and
 *  the host. Login and bootstrap are the same screen with different verbs, so the
 *  layout lives here once (ADR 0017). */
export function AuthScreen({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-background p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex flex-col items-center gap-2 text-center">
          <div className="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Activity className="size-6" />
          </div>
          <h1 className="text-xl font-bold">{title}</h1>
          <FieldDescription>{description}</FieldDescription>
        </div>
        {children}
        <InstanceFooter />
      </div>
    </div>
  );
}

/** InstanceFooter says which Verve this is: the build version from the public
 *  auth state, and the host from the address bar. On a homelab with a staging
 *  instance beside the real one, that line is the difference between typing a
 *  password into the right box and the wrong one. */
function InstanceFooter() {
  const authState = useAuthState();
  const version = authState.data?.version;

  return (
    <FieldDescription className="text-center text-xs">
      Verve {version ?? "…"} · {window.location.host}
    </FieldDescription>
  );
}

/** PasswordField is the password input with the two things a typed-in-the-dark
 *  secret needs: a reveal toggle, and a Caps Lock warning, which is the single
 *  most common cause of a password that is right but rejected. */
export function PasswordField({
  value,
  onChange,
  autoComplete,
  error,
}: {
  value: string;
  onChange: (value: string) => void;
  autoComplete: "current-password" | "new-password";
  error?: string;
}) {
  const [revealed, setRevealed] = React.useState(false);
  const [capsLock, setCapsLock] = React.useState(false);

  const trackCapsLock = (e: React.KeyboardEvent<HTMLInputElement>) =>
    setCapsLock(e.getModifierState("CapsLock"));

  return (
    <Field>
      <FieldLabel htmlFor="password">Password</FieldLabel>
      <div className="relative">
        <Input
          id="password"
          type={revealed ? "text" : "password"}
          autoComplete={autoComplete}
          required
          aria-invalid={error ? true : undefined}
          className={cn("pr-9", error && "border-destructive")}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyUp={trackCapsLock}
          onKeyDown={trackCapsLock}
          onBlur={() => setCapsLock(false)}
        />
        <button
          type="button"
          onClick={() => setRevealed((r) => !r)}
          aria-label={revealed ? "Hide password" : "Show password"}
          className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
        >
          {revealed ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
        </button>
      </div>
      {capsLock && <FieldDescription className="text-xs">Caps Lock is on.</FieldDescription>}
      {error && <p className="text-sm text-destructive">{error}</p>}
    </Field>
  );
}
