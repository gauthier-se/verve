import * as React from "react";
import { Link } from "@tanstack/react-router";
import { Check, Upload, XCircle } from "lucide-react";
import { useOnImportDone, useImportStatus, useUploadImport } from "@/hooks/use-import";
import { useDashboards } from "@/hooks/use-dashboards";
import { ApiError } from "@/lib/api";
import type { ImportJob } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { Eyebrow, Figure, ScreenTitle, SectionTitle, Track } from "./ui/figure";

/** ImportPage drives the browser end of a self-service import (ADR 0016): a
 *  drop-zone that streams an Apple Health .zip to the server, then a live two-phase
 *  progress bar and, when it settles, the report or a readable failure.
 *
 *  It is also, for most Accounts, the second screen they ever see, so it is built as
 *  a short numbered path rather than as a control panel: where you are, what to drop,
 *  what happened. The rail at the top is the whole of the onboarding — instance made,
 *  data in, panels waiting — and it retires itself once the third step is true. */
export function ImportPage() {
  const uploadMutation = useUploadImport();
  const status = useImportStatus(uploadMutation.isPending);
  const onImportDone = useOnImportDone();

  const job = status.data?.job ?? null;
  const running = job?.status === "pending" || job?.status === "running";
  const busy = uploadMutation.isPending || running;
  const hasData = status.data?.has_data ?? false;

  // Refill the seeded Panels once, when an import finishes.
  const doneHandled = React.useRef(false);
  React.useEffect(() => {
    if (job?.status === "done" && !doneHandled.current) {
      doneHandled.current = true;
      onImportDone();
    } else if (job?.status !== "done") {
      doneHandled.current = false;
    }
  }, [job?.status, onImportDone]);

  const [localError, setLocalError] = React.useState<string | null>(null);
  const uploadError = uploadMutation.error instanceof ApiError ? uploadMutation.error.message : null;

  const accept = (file: File | undefined) => {
    if (!file) return;
    if (!file.name.toLowerCase().endsWith(".zip")) {
      setLocalError("Choose the .zip export from Apple Health.");
      return;
    }
    setLocalError(null);
    uploadMutation.reset();
    uploadMutation.mutate(file);
  };

  return (
    <div className="flex h-full flex-col">
      <header className="border-b px-6 py-3.5">
        <ScreenTitle>Import data</ScreenTitle>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-8">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
          <Steps hasData={hasData} busy={busy} />

          {busy ? <Progress job={job} pending={uploadMutation.isPending} /> : <DropZone onFile={accept} />}

          {(localError || uploadError) && (
            <p className="text-xs text-destructive">{localError ?? uploadError}</p>
          )}

          {!busy && job?.status === "done" && job.report && <ReportCard job={job} />}
          {!busy && job?.status === "failed" && <FailureCard message={job.error} />}
        </div>
      </div>
    </div>
  );
}

/** Steps is the three-step rail: what is already true, what to do now, what is
 *  waiting. A dot filled means done, outlined means current, empty means ahead. */
function Steps({ hasData, busy }: { hasData: boolean; busy: boolean }) {
  const dashboards = useDashboards();
  const panels = dashboards.data?.reduce((n, d) => n + d.panels.length, 0) ?? 0;

  const steps = [
    {
      title: "Your instance is running",
      body: "An account on a machine you control. There is no cloud to sync to, which is the point.",
      done: true,
    },
    {
      title: "Bring your history in",
      body: "Everything Health has kept, back to the first day it recorded anything. It is read once and stored in Verve's own model, so it outlives the export it came from.",
      done: hasData,
    },
    {
      title:
        panels > 0
          ? `${panels} ${panels === 1 ? "panel is" : "panels are"} already arranged`
          : "Your panels are waiting",
      body: "A dashboard was seeded when the account was made. It fills itself in the moment the data lands — nothing to configure first.",
      done: false,
    },
  ];

  // The step being worked on right now: the first one not yet true.
  const current = busy ? 1 : steps.findIndex((s) => !s.done);

  return (
    <div className="flex flex-col">
      {steps.map((step, i) => (
        <div key={step.title} className="grid [grid-template-columns:1.375rem_1fr] gap-3">
          <div className="relative flex justify-center">
            {i < steps.length - 1 && (
              <span className="absolute inset-y-0 top-5 w-px bg-border" aria-hidden />
            )}
            <span
              className={cn(
                "relative mt-0.5 flex size-[1.125rem] items-center justify-center rounded-full border font-mono text-3xs tabular-nums",
                step.done && "border-primary bg-primary text-primary-foreground",
                !step.done && i === current && "border-muted-foreground/50 bg-muted text-foreground",
                !step.done && i !== current && "border-border text-muted-foreground",
              )}
            >
              {step.done ? <Check className="size-2.5" /> : i + 1}
            </span>
          </div>
          <div className="pb-5">
            <SectionTitle className={cn("whitespace-normal", step.done && "text-muted-foreground")}>
              {step.title}
            </SectionTitle>
            <p className="pt-0.5 text-xs leading-relaxed text-muted-foreground">{step.body}</p>
          </div>
        </div>
      ))}
    </div>
  );
}

/** DropZone accepts a dropped or picked file. It only forwards the file; the page
 *  validates the extension and starts the upload. */
function DropZone({ onFile }: { onFile: (file: File | undefined) => void }) {
  const inputRef = React.useRef<HTMLInputElement>(null);
  const [over, setOver] = React.useState(false);

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        onFile(e.dataTransfer.files?.[0]);
      }}
      className={cn(
        "flex flex-col items-center justify-center gap-3.5 rounded-xl border-2 border-dashed bg-card/40 px-6 py-11 text-center transition-colors",
        over ? "border-primary bg-accent/50" : "border-border",
      )}
    >
      <div className="flex size-11 items-center justify-center rounded-full bg-muted">
        <Upload className="size-[1.125rem] text-muted-foreground" />
      </div>
      <div>
        <p className="text-heading font-medium">Drop your Apple Health export here</p>
        <p className="pt-1.5 text-xs leading-relaxed text-muted-foreground">
          The <span className="font-mono text-foreground">export.zip</span> from Health → your profile
          → Export All Health Data. Nothing to unzip, and nothing leaves this machine.
        </p>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept=".zip,application/zip"
        className="hidden"
        onChange={(e) => onFile(e.target.files?.[0])}
      />
      <Button size="sm" className="h-8 px-3.5" onClick={() => inputRef.current?.click()}>
        Choose file
      </Button>
    </div>
  );
}

/** Progress renders the two-phase bar. Until the first status snapshot arrives it
 *  shows an indeterminate "Uploading…". Both the phase and the percent are the
 *  server's: the bar reports a job running on the other side of the connection, and
 *  a client-side animation would be a guess dressed as a measurement. */
function Progress({ job, pending }: { job: ImportJob | null; pending: boolean }) {
  const phase = job?.phase ?? "upload";
  const percent = job?.percent ?? 0;
  const label = phase === "import" ? "Importing…" : "Uploading…";
  const showBar = job !== null || !pending;

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="flex items-baseline justify-between text-heading">
        <span className="font-medium">{label}</span>
        {showBar && <span className="font-mono text-2xs tabular-nums text-muted-foreground">{percent} %</span>}
      </div>
      <Track fill={percent / 100} color="hsl(var(--primary))" animated />
      <p className="text-2xs leading-relaxed text-muted-foreground/70">
        Large exports take a few minutes. You can leave this page open — the job runs on the server.
      </p>
    </Card>
  );
}

/** ReportCard shows the counts of a finished import and a way back to the data.
 *
 *  Three numbers, and the middle one is the promise: what was skipped is what you
 *  already had. That is what makes re-dropping a fresh export every month a safe
 *  habit rather than a risk (ADR 0006). */
function ReportCard({ job }: { job: ImportJob }) {
  const r = job.report!;
  return (
    <>
      <Card className="overflow-hidden">
        <div className="flex items-center gap-2.5 px-4 pb-3 pt-4">
          <span className="flex size-[1.125rem] items-center justify-center rounded-full bg-primary text-primary-foreground">
            <Check className="size-2.5" />
          </span>
          <SectionTitle>Imported {r.source_file}</SectionTitle>
        </div>
        <dl className="grid grid-cols-3 border-t">
          <ReportStat label="Added" value={r.added} />
          <ReportStat label="Already had" value={r.skipped} className="border-x border-border/60" />
          <ReportStat label="Unmapped" value={r.unmapped} />
        </dl>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3.5">
          <p className="max-w-[21rem] text-xs leading-relaxed text-muted-foreground">
            Your panels were filled from what you actually have. Re-drop a fresh export any month —
            only what is new is added.
          </p>
          <Button asChild size="sm" className="h-8 px-3.5">
            <Link to="/">View your dashboard</Link>
          </Button>
        </div>
      </Card>

      {r.unmapped > 0 && <UnmappedCard count={r.unmapped} />}
    </>
  );
}

function ReportStat({ label, value, className }: { label: string; value: number; className?: string }) {
  return (
    <div className={cn("px-4 py-4 text-center", className)}>
      <dd>
        <Figure size="strip">{value.toLocaleString("fr-FR")}</Figure>
      </dd>
      <dt className="pt-1">
        <Eyebrow>{label}</Eyebrow>
      </dt>
    </div>
  );
}

/** UnmappedCard accounts for what the Catalog could not read. It is a quieter card
 *  than the report on purpose: it is not a failure, it is the other half of "nothing
 *  incoming is discarded" (ADR 0002). The records are kept, and they land in Verve
 *  the day the Catalog learns their type. */
function UnmappedCard({ count }: { count: number }) {
  return (
    <Card className="bg-card/40 px-4 py-3.5">
      <p className="text-xs font-medium">
        {count.toLocaleString("fr-FR")} {count === 1 ? "record" : "records"} could not be mapped
      </p>
      <p className="pt-1 text-2xs leading-relaxed text-muted-foreground">
        Their type is not in the catalog yet. They are kept as they arrived rather than dropped, so
        they become readable the day the catalog covers them — no re-export needed.
      </p>
    </Card>
  );
}

/** FailureCard shows a readable reason and leaves the drop-zone ready to retry. */
function FailureCard({ message }: { message?: string }) {
  return (
    <Card className="border-destructive/50 px-4 py-3.5">
      <div className="flex items-center gap-2">
        <XCircle className="size-4 text-destructive" />
        <SectionTitle>Import failed</SectionTitle>
      </div>
      <p className="pt-1 text-xs leading-relaxed text-muted-foreground">
        {message ?? "Something went wrong."} Whatever landed before the failure was kept, and
        dropping the same export again picks up where it stopped without duplicating any of it.
      </p>
    </Card>
  );
}
