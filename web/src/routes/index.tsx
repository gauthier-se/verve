import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/")({
  component: Index,
});

function Index() {
  const { t } = useTranslation();
  return (
    <>
      {/* The brand name is not translated. */}
      <h1>Verve</h1>
      <p>{t("home.tagline")}</p>
    </>
  );
}
