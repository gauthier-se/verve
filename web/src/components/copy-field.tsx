import { CopyIcon } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface CopyFieldProps {
  label: string;
  value: string;
  /** The text of the button that copies the value. */
  copyLabel: string;
  autoFocus?: boolean;
}

/**
 * A labelled read-only value with a button that copies it to the clipboard,
 * and says whether it did. The value is selected on focus, to copy it by hand
 * where the browser gives no clipboard, as over plain HTTP.
 */
export function CopyField({
  label,
  value,
  copyLabel,
  autoFocus,
}: CopyFieldProps) {
  const { t } = useTranslation();
  const id = useId();
  const [copied, setCopied] = useState<boolean>();

  async function copy() {
    try {
      // Browsers give no clipboard outside a secure context.
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Input
          id={id}
          readOnly
          value={value}
          autoFocus={autoFocus}
          spellCheck={false}
          onFocus={(event) => {
            event.currentTarget.select();
          }}
          className="h-11 min-w-0 font-mono sm:flex-1"
        />
        <Button
          type="button"
          variant="outline"
          size="lg"
          className="min-h-11"
          onClick={() => {
            void copy();
          }}
        >
          <CopyIcon aria-hidden />
          {copyLabel}
        </Button>
      </div>
      <p role="status" className="text-sm text-muted-foreground">
        {copied === true && t("copy.copied")}
        {copied === false && t("copy.failed")}
      </p>
    </div>
  );
}
