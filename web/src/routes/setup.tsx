import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setup")({
  component: Setup,
});

/**
 * Tells how to create the first admin until the setup wizard: the setup link
 * of the server log opens this page too.
 */
function Setup() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2">
      <h1 className="font-heading text-2xl font-semibold">
        {t("setup.title")}
      </h1>
      <p className="text-muted-foreground">{t("setup.description")}</p>
    </div>
  );
}
