import { RouterProvider } from "@tanstack/react-router";
import { useMe } from "./hooks/use-auth";
import { LoginPage } from "./components/login-page";
import { CenteredSpinner } from "./components/spinner";
import { router } from "./router";

/** App is the auth gate: while the session resolves it shows a spinner, an
 *  unauthenticated visitor gets the login screen, and a logged-in Account gets
 *  the routed dashboards. Routing only exists once authenticated, so no route
 *  can render Account data without a session. */
export function App() {
  const me = useMe();

  if (me.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <CenteredSpinner />
      </div>
    );
  }

  if (!me.data) return <LoginPage />;

  return <RouterProvider router={router} />;
}
