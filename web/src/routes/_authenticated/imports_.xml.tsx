import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { type DragEvent, useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import { getImportOptions } from "@/api/generated/@tanstack/react-query.gen";
import { ImportReport } from "@/components/imports/import-report";
import {
  activePollMs,
  failureLabel,
  isActive,
  statusLabel,
} from "@/components/imports/labels";
import { uploadRatio, useUpload } from "@/components/imports/upload-provider";
import { Button, buttonVariants } from "@/components/ui/button";
import { formatPercent } from "@/i18n/format";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/imports_/xml")({
  component: UploadPage,
});

/**
 * The upload of an Apple Health export: a guide from the Health app to the
 * export file, and a drop zone that uploads it with resumption. The upload
 * belongs to the session, not the page, so that it continues elsewhere. Once
 * it completes, the page follows its import, and when it fails, it explains
 * why and offers to try again.
 */
function UploadPage() {
  const { t } = useTranslation();
  const guideId = useId();
  const { state, start, retry } = useUpload();

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
      {state.status === "failed" && (
        <div className="flex flex-col items-start gap-2">
          <p role="alert" className="text-sm text-destructive">
            {failureLabel(t, state.failure)}
          </p>
          <Button size="lg" className="min-h-11 px-4" onClick={retry}>
            {t("upload.retry")}
          </Button>
        </div>
      )}
      {state.status === "completed" &&
        (state.importId === undefined ? (
          <div className="flex flex-col items-start gap-2">
            <p>{t("upload.complete", { fileName: state.fileName })}</p>
            <Button
              asChild
              variant="outline"
              size="lg"
              className="min-h-11 px-4"
            >
              <Link to="/imports">{t("upload.viewImports")}</Link>
            </Button>
          </div>
        ) : (
          <FollowedImport id={state.importId} fileName={state.fileName} />
        ))}
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
  // Whether the file dropped last is not an export, which the file picker
  // already filters.
  const [refused, setRefused] = useState(false);

  function onDrop(event: DragEvent) {
    event.preventDefault();
    setDragging(false);
    const file = event.dataTransfer.files[0];
    if (file === undefined || disabled) {
      return;
    }
    const isExport = /\.(zip|xml)$/i.test(file.name);
    setRefused(!isExport);
    if (isExport) {
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
            setRefused(false);
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
      {refused && (
        <p role="alert" className="text-sm text-destructive">
          {t("upload.dropZone.refused")}
        </p>
      )}
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
  const ratio = state.status === "uploading" ? uploadRatio(state) : 0;
  const percent = (value: number) => formatPercent(value, i18n.language);

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
          <ProgressBar
            label={t("upload.progressLabel", { fileName: state.fileName })}
            ratio={ratio}
          />
        </div>
      )}
    </>
  );
}

/**
 * The import of the completed upload, `id` in `Verve-Import-Id`: its
 * progress while it runs, then its status and its report, polled while it is
 * queued or running.
 */
function FollowedImport({ id, fileName }: { id: number; fileName: string }) {
  const { t, i18n } = useTranslation();
  const { user } = Route.useRouteContext();
  const headingId = useId();
  const followed = useQuery({
    ...getImportOptions({ path: { id } }),
    refetchInterval: (query) =>
      query.state.data && isActive(query.state.data.status)
        ? activePollMs
        : false,
  });
  const imp = followed.data;

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-6 rounded-xl border p-4"
    >
      <h2 id={headingId} className="font-heading text-lg font-medium">
        {t("upload.import.title", { fileName })}
      </h2>
      {followed.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(followed.error, i18n)}
        </p>
      )}
      {imp && (
        <>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4">
            <dt className="text-muted-foreground">
              {t("upload.import.status")}
            </dt>
            <dd>{statusLabel(t, imp.status)}</dd>
          </dl>
          {imp.status === "running" && imp.progress !== undefined && (
            <div className="flex flex-col gap-2">
              <p>
                {t("upload.import.progress", {
                  percent: formatPercent(imp.progress, i18n.language),
                })}
              </p>
              <ProgressBar
                label={t("upload.import.progressLabel")}
                ratio={imp.progress}
              />
            </div>
          )}
          <ImportReport report={imp.report} timeZone={user.timezone} />
        </>
      )}
    </section>
  );
}

/** A bar filled to `ratio`, from 0 to 1, named `label`. */
function ProgressBar({ label, ratio }: { label: string; ratio: number }) {
  return (
    <div
      role="progressbar"
      aria-label={label}
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
  );
}
