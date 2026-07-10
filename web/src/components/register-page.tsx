import * as React from "react";
import { Activity } from "lucide-react";
import { useRegister } from "@/hooks/use-auth";
import { ApiError } from "@/lib/api";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

/** RegisterPage is the first-run bootstrap screen, shown only while the instance
 *  has no Account (ADR 0017). It creates the admin Account and, via the auto-login,
 *  the primed `me` cache flips the app straight to the seeded dashboard. */
export function RegisterPage() {
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const register = useRegister();

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    register.mutate({ email, password });
  };

  const message =
    register.error instanceof ApiError
      ? register.error.message
      : register.error
        ? "Something went wrong"
        : null;

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <div className="mb-2 flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Activity className="size-6" />
          </div>
          <CardTitle className="text-2xl">Welcome to Verve</CardTitle>
          <p className="text-sm text-muted-foreground">Create your account to set up this instance</p>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                autoComplete="username"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete="new-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            {message && <p className="text-sm text-destructive">{message}</p>}
            <Button type="submit" className="w-full" disabled={register.isPending}>
              {register.isPending ? "Creating account…" : "Create account"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
