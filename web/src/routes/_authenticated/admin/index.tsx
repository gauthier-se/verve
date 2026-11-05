import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/_authenticated/admin/")({
  component: Admin,
});

function Admin() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2">
      <h1 className="font-heading text-2xl font-semibold">
        {t("admin.title")}
      </h1>
      <p className="text-muted-foreground">{t("admin.description")}</p>
    </div>
  );
}
