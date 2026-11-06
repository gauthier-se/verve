import { useTranslation } from "react-i18next";

import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";

interface TokenStepProps {
  /** Why the token is asked again, once the server refused one. */
  error?: string | undefined;
  /** Called with the token that the user entered. */
  onSubmit: (token: string) => void;
}

/**
 * Asks for the setup token, when the page was opened without the link. The
 * user can paste the whole link, which the server log gives.
 */
export function TokenStep({ error, onSubmit }: TokenStepProps) {
  const { t } = useTranslation();
  const form = useAppForm({
    defaultValues: { token: "" },
    onSubmit: ({ value }) => {
      onSubmit(tokenOf(value.token));
    },
  });

  return (
    <form
      noValidate
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <p className="text-muted-foreground">{t("setup.token.description")}</p>
      {error !== undefined && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <form.AppField
        name="token"
        validators={{
          onChange: ({ value }) =>
            tokenOf(value) === "" ? t("setup.token.required") : undefined,
        }}
      >
        {(field) => (
          <field.TextField
            label={t("setup.token.label")}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
          />
        )}
      </form.AppField>
      <Button type="submit" size="lg" className="min-h-11">
        {t("setup.continue")}
      </Button>
    </form>
  );
}

/**
 * The token of an entry: what follows the # of a setup link, up to the end of
 * the link, since the line of the log can go on after it, or the entry itself
 * when it is not a link.
 */
function tokenOf(entry: string): string {
  const afterHash = entry.slice(entry.lastIndexOf("#") + 1);
  return afterHash.trim().split(/\s/)[0] ?? "";
}
