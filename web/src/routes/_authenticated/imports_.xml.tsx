import { createFileRoute } from "@tanstack/react-router";
import { type DragEvent, useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { useUpload } from "@/components/imports/upload-provider";
import { buttonVariants } from "@/components/ui/button";
import { formatNumber } from "@/i18n/format";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/imports_/xml")({
  component: UploadPage,
});

/**
 * The upload of an Apple Health export: a guide from the Health app to the
 * export file, and a drop zone that uploads it with resumption. The upload
 * belongs to the session, not the page, so that it continues elsewhere.
 */
function UploadPage() {
  const { t } = useTranslation();
  const guideId = useId();
  const { state, start } = useUpload();

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="font-heading text-2xl font-semibold">
          {t("upload.title")}
        </h1>
        <p className="text-muted-foreground">{t("upload.description")}</p>
      </div>
      <section className="flex flex-col gap-2">
        <h2 id={guideId} className="font-heading text-lg font-medium">
          {t("upload.guide.title")}
        </h2>
        <ol
          aria-labelledby={guideId}
          className="flex list-decimal flex-col gap-2 pl-5"
        >
          <li>{t("upload.guide.open")}</li>
          <li>{t("upload.guide.profile")}</li>
          <li>{t("upload.guide.export")}</li>
          <li>{t("upload.guide.share")}</li>
        </ol>
        <p className="text-muted-foreground">{t("upload.duration")}</p>
      </section>
      <DropZone disabled={state.status === "uploading"} onFile={start} />
      <UploadProgress />
    </div>
  );
}

interface DropZoneProps {
  /** Whether the zone refuses a file, while an upload runs. */
  disabled: boolean;
  onFile: (file: File) => void;
}

/**
 * Takes the export file, dropped on it or chosen with its file picker, which
 * the keyboard reaches.
 */
function DropZone({ disabled, onFile }: DropZoneProps) {
  const { t } = useTranslation();
  const inputId = useId();
  const [dragging, setDragging] = useState(false);

  function onDrop(event: DragEvent) {
    event.preventDefault();
    setDragging(false);
    const file = event.dataTransfer.files[0];
    if (file !== undefined && !disabled) {
      onFile(file);
    }
  }

  return (
    <section
      aria-label={t("upload.dropZone.label")}
      onDragOver={(event) => {
        event.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => {
        setDragging(false);
      }}
      onDrop={onDrop}
      className={cn(
        "flex flex-col items-center gap-4 rounded-xl border-2 border-dashed p-8 text-center",
        dragging && "border-primary bg-muted",
      )}
    >
      <p>{t("upload.dropZone.description")}</p>
      <input
        id={inputId}
        type="file"
        accept=".zip,.xml"
        disabled={disabled}
        className="peer sr-only"
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          // Cleared, so that choosing the same file again uploads it again.
          event.currentTarget.value = "";
          if (file !== undefined) {
            onFile(file);
          }
        }}
      />
      <label
        htmlFor={inputId}
        className={cn(
          buttonVariants({ variant: "outline", size: "lg" }),
          "min-h-11 cursor-pointer px-4 peer-focus-visible:border-ring peer-focus-visible:ring-3 peer-focus-visible:ring-ring/50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
        )}
      >
        {t("upload.dropZone.choose")}
      </label>
    </section>
  );
}

/**
 * The percentage of the running upload, which screen readers hear at steps
 * of 10 percent, from a live region that stays on the page.
 */
function UploadProgress() {
  const { t, i18n } = useTranslation();
  const { state } = useUpload();
  const ratio =
    state.status === "uploading" && state.bytesTotal > 0
      ? state.bytesSent / state.bytesTotal
      : 0;
  const percent = (value: number) =>
    formatNumber(value, i18n.language, { style: "percent" });

  return (
    <>
      <p role="status" className="sr-only">
        {state.status === "uploading" &&
          t("upload.announce", {
            percent: percent(Math.floor(ratio * 10) / 10),
          })}
      </p>
      {state.status === "uploading" && (
        <div className="flex flex-col gap-2">
          <p>
            {t("upload.progress", {
              fileName: state.fileName,
              percent: percent(ratio),
            })}
          </p>
          <div
            role="progressbar"
            aria-label={t("upload.progressLabel", { fileName: state.fileName })}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(ratio * 100)}
            className="h-2 overflow-hidden rounded-full bg-muted"
          >
            <div
              className="h-full bg-primary transition-[width]"
              style={{ width: `${String(ratio * 100)}%` }}
            />
          </div>
        </div>
      )}
    </>
  );
}
