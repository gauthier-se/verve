import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";

/**
 * Tells the admin how their Apple Health data reaches Verve, with a link to
 * the upload page of an export.
 */
export function ImportStep() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground">{t("setup.import.description")}</p>
      <ul className="flex list-disc flex-col gap-2 pl-5">
        <li>{t("setup.import.export")}</li>
        <li>{t("setup.import.sync")}</li>
      </ul>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button asChild size="lg" className="min-h-11 px-4">
          <Link to="/imports/xml">{t("setup.import.upload")}</Link>
        </Button>
        <Button asChild variant="outline" size="lg" className="min-h-11 px-4">
          <Link to="/">{t("setup.import.open")}</Link>
        </Button>
      </div>
    </div>
  );
}
