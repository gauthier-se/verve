import * as React from "react";
import { RouterProvider } from "@tanstack/react-router";
import { useAuthState, useMe } from "./hooks/use-auth";
import { LoginPage } from "./components/login-page";
import { RegisterPage } from "./components/register-page";
import { CenteredSpinner } from "./components/spinner";
import { router } from "./router";

/** App is the auth gate: while the session resolves it shows a spinner, a
 *  logged-in Account gets the routed dashboards, and an unauthenticated visitor
 *  gets the create-account screen on a fresh instance or the login screen once
 *  it is initialized (ADR 0017). Routing only exists once authenticated, so no
 *  route can render Account data without a session. */
export function App() {
  const me = useMe();
  const authState = useAuthState();
  const needsBootstrap = authState.data?.needs_bootstrap ?? false;

  // Signup is closed: /register is a dead route, so send it to /login (ADR 0017).
  React.useEffect(() => {
    if (!needsBootstrap && window.location.pathname === "/register") {
      window.history.replaceState(null, "", "/login");
    }
  }, [needsBootstrap]);

  if (me.isLoading || authState.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <CenteredSpinner />
      </div>
    );
  }

  if (me.data) return <RouterProvider router={router} />;

  return needsBootstrap ? <RegisterPage /> : <LoginPage />;
}
