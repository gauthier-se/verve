import * as React from "react";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, Upload, XCircle } from "lucide-react";
import { useOnImportDone, useImportStatus, useUploadImport } from "@/hooks/use-import";
import { ApiError } from "@/lib/api";
import type { ImportJob } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";

/** ImportPage drives the browser end of a self-service import (ADR 0016): a
 *  drop-zone that streams an Apple Health .zip to the server, then a live two-phase
 *  progress bar and, when it settles, the report or a readable failure. */
export function ImportPage() {
  const uploadMutation = useUploadImport();
  const status = useImportStatus(uploadMutation.isPending);
  const onImportDone = useOnImportDone();

  const job = status.data?.job ?? null;
  const running = job?.status === "pending" || job?.status === "running";
  const busy = uploadMutation.isPending || running;

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
  const uploadError =
    uploadMutation.error instanceof ApiError ? uploadMutation.error.message : null;

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
      <header className="border-b px-6 py-3">
        <h1 className="text-xl font-semibold">Import</h1>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto max-w-xl space-y-4">
          {busy ? (
            <Progress job={job} pending={uploadMutation.isPending} />
          ) : (
            <DropZone onFile={accept} />
          )}

          {(localError || uploadError) && (
            <p className="text-sm text-destructive">{localError ?? uploadError}</p>
          )}

          {!busy && job?.status === "done" && job.report && <ReportCard job={job} />}
          {!busy && job?.status === "failed" && <FailureCard message={job.error} />}
        </div>
      </div>
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
        "flex flex-col items-center justify-center gap-4 rounded-xl border-2 border-dashed p-12 text-center transition-colors",
        over ? "border-primary bg-accent/50" : "border-border",
      )}
    >
      <div className="flex size-12 items-center justify-center rounded-full bg-muted">
        <Upload className="size-6 text-muted-foreground" />
      </div>
      <div>
        <p className="font-medium">Drop your Apple Health export here</p>
        <p className="text-sm text-muted-foreground">
          The <code>export.zip</code> from Health → your profile → Export All Health Data.
        </p>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept=".zip,application/zip"
        className="hidden"
        onChange={(e) => onFile(e.target.files?.[0])}
      />
      <Button onClick={() => inputRef.current?.click()}>Choose file</Button>
    </div>
  );
}

/** Progress renders the two-phase bar. Until the first status snapshot arrives it
 *  shows an indeterminate "Uploading…". */
function Progress({ job, pending }: { job: ImportJob | null; pending: boolean }) {
  const phase = job?.phase ?? "upload";
  const percent = job?.percent ?? 0;
  const label = phase === "import" ? "Importing…" : "Uploading…";
  const showBar = job !== null || !pending;

  return (
    <div className="space-y-3 rounded-xl border p-6">
      <div className="flex items-center justify-between text-sm">
        <span className="font-medium">{label}</span>
        {showBar && <span className="text-muted-foreground">{percent}%</span>}
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${percent}%` }} />
      </div>
      <p className="text-xs text-muted-foreground">
        Large exports take a few minutes. You can leave this page open.
      </p>
    </div>
  );
}

/** ReportCard shows the counts of a finished import and a way back to the data. */
function ReportCard({ job }: { job: ImportJob }) {
  const r = job.report!;
  return (
    <Card>
      <CardHeader className="flex-row items-center gap-2 space-y-0">
        <CheckCircle2 className="size-5 text-primary" />
        <CardTitle>Imported {r.source_file}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid grid-cols-3 gap-4 text-center">
          <Stat label="Added" value={r.added} />
          <Stat label="Skipped" value={r.skipped} />
          <Stat label="Unmapped" value={r.unmapped} />
        </dl>
        <Button asChild variant="outline" className="w-full">
          <Link to="/">View your dashboard</Link>
        </Button>
      </CardContent>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dd className="text-2xl font-semibold tabular-nums">{value.toLocaleString()}</dd>
      <dt className="text-xs uppercase tracking-wide text-muted-foreground">{label}</dt>
    </div>
  );
}

/** FailureCard shows a readable reason and leaves the drop-zone ready to retry. */
function FailureCard({ message }: { message?: string }) {
  return (
    <Card className="border-destructive/50">
      <CardHeader className="flex-row items-center gap-2 space-y-0">
        <XCircle className="size-5 text-destructive" />
        <CardTitle>Import failed</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">
          {message ?? "Something went wrong."} You can try again above.
        </p>
      </CardContent>
    </Card>
  );
}
