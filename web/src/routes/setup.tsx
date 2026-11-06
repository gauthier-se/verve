import { createFileRoute, redirect } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { getSetupStatusOptions } from "@/api/generated/@tanstack/react-query.gen";
import { AccountStep } from "@/components/setup/account-step";
import { ImportStep } from "@/components/setup/import-step";
import { TimezoneStep } from "@/components/setup/timezone-step";
import { TokenStep } from "@/components/setup/token-step";

/**
 * The setup wizard, which creates the first admin with the setup token of the
 * link that the server writes to its log. The route reads the token from the
 * fragment of the link, outside render, which React can run twice, and puts it
 * in the route context. Once setup is done, the page goes to the login page.
 */
export const Route = createFileRoute("/setup")({
  beforeLoad: async ({ context: { queryClient, readFragmentSecret } }) => {
    // Before any request, so that the token leaves the address bar first.
    const token = readFragmentSecret();
    const setup = await queryClient.query(getSetupStatusOptions());
    if (!setup.required) {
      throw redirect({ to: "/login" });
    }
    return { token };
  },
  component: Setup,
});

type Step = "token" | "account" | "timezone" | "import";

const stepTitles = {
  token: "setup.token.title",
  account: "setup.account.title",
  timezone: "timezone.title",
  import: "setup.import.title",
} as const;

/**
 * Goes through the steps of setup: the token when the link did not give one,
 * the admin account, the timezone, then a guide to importing data. The steps
 * and the token live in component state only: a reload starts again.
 */
function Setup() {
  const { t } = useTranslation();
  const context = Route.useRouteContext();
  const [token, setToken] = useState(context.token);
  const [step, setStep] = useState<Step>(
    token === undefined ? "token" : "account",
  );
  // The token of the link, or the one entered before, that the server refused.
  const [tokenRejected, setTokenRejected] = useState(false);
  const steps: Step[] =
    context.token === undefined || tokenRejected
      ? ["token", "account", "timezone", "import"]
      : ["account", "timezone", "import"];
  const headingRef = useRef<HTMLHeadingElement>(null);
  const moved = useRef(false);

  // Once the user moves to another step, the focus goes to its heading, which
  // tells a screen reader that the page changed.
  useEffect(() => {
    if (moved.current) {
      headingRef.current?.focus();
    }
  }, [step]);

  function goTo(next: Step) {
    moved.current = true;
    setStep(next);
  }

  return (
    <div className="mx-auto flex w-full max-w-md flex-col gap-6 py-6">
      <h1 className="font-heading text-2xl font-semibold">
        {t("setup.title")}
      </h1>
      <section className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <p className="text-sm text-muted-foreground">
            {t("setup.progress", {
              step: steps.indexOf(step) + 1,
              total: steps.length,
            })}
          </p>
          <h2
            ref={headingRef}
            tabIndex={-1}
            className="font-heading text-lg font-semibold outline-none"
          >
            {t(stepTitles[step])}
          </h2>
        </div>
        {step === "token" && (
          <TokenStep
            error={tokenRejected ? t("errors.setup_token_invalid") : undefined}
            onSubmit={(entered) => {
              setToken(entered);
              goTo("account");
            }}
          />
        )}
        {step === "account" && token !== undefined && (
          <AccountStep
            token={token}
            onCreated={() => {
              goTo("timezone");
            }}
            onTokenRejected={() => {
              setTokenRejected(true);
              goTo("token");
            }}
          />
        )}
        {step === "timezone" && (
          <TimezoneStep
            onDone={() => {
              goTo("import");
            }}
          />
        )}
        {step === "import" && <ImportStep />}
      </section>
    </div>
  );
}
