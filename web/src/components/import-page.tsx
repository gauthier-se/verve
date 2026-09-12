import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowDownToLine, Ban, Check, Plus, Upload, X, XCircle } from "lucide-react";
import { useOnImportDone, useImportStatus, useUploadImport } from "@/hooks/use-import";
import { useDashboards } from "@/hooks/use-dashboards";
import { useExclusions, useRemoveExclusion } from "@/hooks/use-exclusions";
import { ApiError } from "@/lib/api";
import { formatDay } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { archiveHref } from "@/lib/series-url";
import type { Exclusion, ImportJob } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Card } from "./ui/card";
import { Eyebrow, Figure, ScreenTitle, SectionTitle, Track } from "./ui/figure";
import { ExclusionDialog } from "./exclusion-dialog";

/** ImportPage drives the browser end of a self-service import (ADR 0016): a
 *  drop-zone that streams a health export .zip to the server, then a live two-phase
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
      setLocalError("Choose a .zip export: Apple Health, or Google Health from Takeout.");
      return;
    }
    setLocalError(null);
    uploadMutation.reset();
    uploadMutation.mutate(file);
  };

  return (
    <div className="flex h-full flex-col">
      <header className="border-b px-6 py-3.5">
        <ScreenTitle>Import & export</ScreenTitle>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-8">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
          <Steps hasData={hasData} busy={busy} />

          <Exclusions />

          {busy ? <Progress job={job} pending={uploadMutation.isPending} /> : <DropZone onFile={accept} />}

          {(localError || uploadError) && (
            <p className="text-xs text-destructive">{localError ?? uploadError}</p>
          )}

          {!busy && job?.status === "done" && job.report && <ReportCard job={job} />}
          {!busy && job?.status === "failed" && <FailureCard message={job.error} />}

          <ExportCard />
        </div>
      </div>
    </div>
  );
}

/** ExportCard is the way out (ADR 0039). It sits under the drop zone because the
 *  page reads top to bottom as data coming in and then data going out, and because
 *  a person looking for their data will not look for it under a settings screen.
 *
 *  A link, not a button with a fetch behind it: the response has no Content-Length,
 *  so the browser download bar is the only honest progress there is. */
function ExportCard() {
  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SectionTitle>Take your data out</SectionTitle>
        <Button asChild variant="outline" size="sm" className="h-7 gap-1.5 px-2.5 text-xs">
          <a href={archiveHref} download>
            <ArrowDownToLine className="size-3.5" /> Download archive
          </a>
        </Button>
      </div>
      <p className="text-2xs leading-relaxed text-muted-foreground/70">
        One zip holding every measurement, night, workout and GPX trace this account
        holds, plus what the catalog could not read. Drop it back on this page to read
        it into another Verve: it is an export like any other.
      </p>
      <p className="text-2xs leading-relaxed text-muted-foreground/70">
        Dashboards, panels and pins stay with this instance. The file is your whole
        history in clear, and nothing encrypts it for you.
      </p>
    </Card>
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
      body: "Everything your export holds, back to the first day it recorded anything. It is read once and stored in Verve's own model, so it outlives the export it came from.",
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

/** Exclusions is what the import you are about to run will refuse (ADR 0033).
 *
 *  It lives here rather than under settings because the request behind the feature
 *  is "when I come back, propose the same import". A set of standing rules read on
 *  the page where the import starts, seconds before it starts, is that proposal;
 *  filed elsewhere it would be a thing to remember to check, which is the opposite.
 *
 *  The card is absent when the set is empty, so a first import is the screen it has
 *  always been, and the way in is the same dialog the Metric page opens. */
function Exclusions() {
  const exclusions = useExclusions();
  const [open, setOpen] = React.useState(false);
  const list = exclusions.data ?? [];

  return (
    <>
      {list.length > 0 ? (
        <Card className="overflow-hidden">
          <div className="flex items-center gap-2.5 border-b px-4 py-3">
            <Ban className="size-3.5 text-muted-foreground" />
            <SectionTitle>Excluded from every import</SectionTitle>
          </div>
          <ul className="divide-y">
            {list.map((e) => (
              <ExclusionRow key={e.id} exclusion={e} />
            ))}
          </ul>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3">
            <p className="max-w-[24rem] text-2xs leading-relaxed text-muted-foreground">
              Removing one restores nothing by itself. The next import brings back whatever the
              export still holds, which is how you undo this.
            </p>
            <Button variant="outline" size="sm" className="h-7 gap-1.5 px-2.5 text-xs" onClick={() => setOpen(true)}>
              <Plus className="size-3.5" /> Exclude a metric
            </Button>
          </div>
        </Card>
      ) : (
        <div className="flex justify-end">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs text-muted-foreground"
            onClick={() => setOpen(true)}
          >
            <Ban className="size-3.5" /> Exclude a metric
          </Button>
        </div>
      )}
      <ExclusionDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

/** ExclusionRow is one standing rule and its removal. The purge count is shown as
 *  what that decision cost, once, on the day it was taken: it is a historical figure
 *  and not a running total, so it never changes again. */
function ExclusionRow({ exclusion }: { exclusion: Exclusion }) {
  const remove = useRemoveExclusion();
  return (
    <li className="flex items-center justify-between gap-3 px-4 py-2.5">
      <div className="min-w-0">
        <p className="truncate text-xs font-medium">{metricLabel(exclusion.metric)}</p>
        <p className="pt-0.5 text-2xs text-muted-foreground">
          {spanLabel(exclusion)}
          {exclusion.purged > 0 && ` · ${exclusion.purged.toLocaleString("fr-FR")} deleted`}
        </p>
      </div>
      <Button
        variant="ghost"
        size="icon"
        className="size-7 shrink-0 text-muted-foreground"
        aria-label={`Stop excluding ${metricLabel(exclusion.metric)}`}
        disabled={remove.isPending}
        onClick={() => remove.mutate(exclusion.id)}
      >
        <X className="size-3.5" />
      </Button>
    </li>
  );
}

/** spanLabel says which days a rule covers, in the words the bounds actually mean:
 *  an absent bound is not a date to print, it is the absence of one. */
function spanLabel({ starts_on, ends_on }: Exclusion): string {
  if (!starts_on && !ends_on) return "Everything";
  if (!ends_on) return `From ${formatDay(starts_on)}`;
  if (!starts_on) return `Up to ${formatDay(ends_on)}`;
  return `${formatDay(starts_on)} → ${formatDay(ends_on)}`;
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
        <p className="text-heading font-medium">Drop your health export here</p>
        <p className="pt-1.5 text-xs leading-relaxed text-muted-foreground">
          <span className="font-mono text-foreground">export.zip</span> from Apple Health → your
          profile → Export All Health Data, or your Google Health archive from takeout.google.com.
          Nothing to unzip, and nothing leaves this machine.
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
          <SectionTitle>
            Imported {r.source_file}
            {r.connector && <span className="font-normal text-muted-foreground"> · {r.connector}</span>}
          </SectionTitle>
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

      {r.excluded > 0 && <ExcludedCard count={r.excluded} />}
      {r.unmapped > 0 && <UnmappedCard count={r.unmapped} />}
    </>
  );
}

/** ExcludedCard accounts for what your own rules refused. It is a fourth number and
 *  not a fourth cell in the grid above, because it is a different kind of thing from
 *  the three: added, already-had and unmapped are what the export contained, and
 *  this is what you decided about it.
 *
 *  It is never silent, for the reason the whole feature exists: an import that drops
 *  data without saying how much is worse than the delete that did not stick. */
function ExcludedCard({ count }: { count: number }) {
  return (
    <Card className="bg-card/40 px-4 py-3.5">
      <p className="text-xs font-medium">
        {count.toLocaleString("fr-FR")} {count === 1 ? "record was" : "records were"} not imported
      </p>
      <p className="pt-1 text-2xs leading-relaxed text-muted-foreground">
        They matched an exclusion you set, so they were refused rather than stored. Remove the
        exclusion above and import again to take them.
      </p>
    </Card>
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
