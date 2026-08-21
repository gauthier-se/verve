import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { Check, ChevronRight, Copy, Download, Pencil, StickyNote } from "lucide-react";
import { useAllAnnotations } from "@/hooks/use-annotations";
import { useLedger } from "@/hooks/use-ledger";
import { formatDuration, formatExact, formatSummaryValue } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { copyTsv, tsvNumber } from "@/lib/clipboard";
import type { Annotation, LedgerRow } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { CenteredSpinner } from "./spinner";
import { AnnotationDialog } from "./annotation-dialog";
import { ManualEntryDialog } from "./manual-entry-dialog";
import { MetricIcon } from "./metric-icon";
import { formatBucket } from "./panel-chart";

/** DataPage is the Ledger (ADR 0021): the numbers behind the graphs as tables. A
 *  scoreboard lists every Metric with data — latest value, ~7-day and ~30-day figures,
 *  and a week-over-week delta — and a row opens that Metric's own page (chart, highs
 *  and lows, and the chronological detail table). */
export function DataPage() {
  const ledger = useLedger();
  const notes = useAllAnnotations();
  const [entryOpen, setEntryOpen] = React.useState(false);
  const empty = (ledger.data?.length ?? 0) === 0;

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <h1 className="text-xl font-semibold">Data</h1>
        <Button variant="outline" size="sm" className="h-7" onClick={() => setEntryOpen(true)}>
          <Pencil className="size-3.5" /> Enter a value
        </Button>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {ledger.isLoading ? (
          <CenteredSpinner />
        ) : empty ? (
          <EmptyState />
        ) : (
          <Scoreboard rows={ledger.data ?? []} />
        )}
        {/* The notes stand on their own: they need no import, so an Account with
            nothing but notes still finds them here. */}
        {!ledger.isLoading && (!empty || (notes.data?.length ?? 0) > 0) && <NotesSection />}
      </div>

      <ManualEntryDialog open={entryOpen} onOpenChange={setEntryOpen} />
    </div>
  );
}

/** Scoreboard is the overview table: one row per Metric, linking to its own page. */
function Scoreboard({ rows }: { rows: LedgerRow[] }) {
  const navigate = useNavigate();
  const [copied, setCopied] = React.useState(false);

  const onCopy = async () => {
    const headers = ["Metric", "Unit", "Latest", "Latest date", "7-day", "30-day", "Delta %"];
    const body = rows.map((r) => [
      metricLabel(r.metric),
      r.unit,
      r.latest ? tsvNumber(r.latest.value) : "",
      r.latest?.date ?? "",
      r.week !== undefined ? tsvNumber(r.week) : "",
      r.month !== undefined ? tsvNumber(r.month) : "",
      r.delta_pct !== undefined ? tsvNumber(r.delta_pct) : "",
    ]);
    await copyTsv(headers, body);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button variant="outline" size="sm" className="h-7" onClick={onCopy}>
          {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          {copied ? "Copied" : "Copy table"}
        </Button>
      </div>

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Metric</TableHead>
              <TableHead className="text-right">Latest</TableHead>
              <TableHead className="text-right">~7 days</TableHead>
              <TableHead className="text-right">~30 days</TableHead>
              <TableHead className="text-right">vs last week</TableHead>
              <TableHead className="w-6" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow
                key={r.metric}
                className="cursor-pointer"
                onClick={() => navigate({ to: "/data/$metric", params: { metric: r.metric } })}
              >
                <TableCell className="font-medium">
                  <span className="inline-flex items-center gap-1.5">
                    <MetricIcon slug={r.metric} className="size-4" />
                    {metricLabel(r.metric)}
                  </span>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {r.latest ? (
                    <span title={`${formatExact(r.latest.value)} ${r.unit}`.trim()}>
                      {ledgerFigure(r.latest.value, r.aggregation)}
                      <span className="ml-1.5 text-xs text-muted-foreground">
                        {formatBucket(r.latest.date, "day")}
                      </span>
                    </span>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </TableCell>
                <WindowCell value={r.week} aggregation={r.aggregation} />
                <WindowCell value={r.month} aggregation={r.aggregation} />
                <TableCell className="text-right tabular-nums text-muted-foreground">
                  <DeltaLabel row={r} />
                </TableCell>
                <TableCell className="w-6 pl-0">
                  <ChevronRight className="size-4 text-muted-foreground" />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

/** ledgerFigure renders a scoreboard number: sleep's minutes as a duration ("7h 12m"),
 *  everything else as a plain figure. A night reported as "432" is a number nobody
 *  reads as a duration (ADR 0027). */
function ledgerFigure(value: number, aggregation: LedgerRow["aggregation"]): string {
  return aggregation === "duration_by_state" ? formatDuration(value) : formatSummaryValue(value, aggregation);
}

function WindowCell({ value, aggregation }: { value: number | undefined; aggregation: LedgerRow["aggregation"] }) {
  return (
    <TableCell className="text-right tabular-nums">
      {value === undefined ? (
        <span className="text-muted-foreground">—</span>
      ) : (
        ledgerFigure(value, aggregation)
      )}
    </TableCell>
  );
}

/** DeltaLabel renders the week-over-week change: a direction arrow plus a percentage
 *  (or the absolute change when there is no percentage base), never colored good/bad
 *  (ADR 0019 — Verve does not know which way is good). */
function DeltaLabel({ row }: { row: LedgerRow }) {
  if (row.delta_abs === undefined) return <span>—</span>;
  const arrow = row.delta_abs > 0 ? "↑" : row.delta_abs < 0 ? "↓" : "→";
  const label =
    row.delta_pct !== undefined
      ? `${Math.abs(Math.round(row.delta_pct))} %`
      : ledgerFigure(Math.abs(row.delta_abs), row.aggregation);
  return (
    <span className={cn(row.delta_abs === 0 && "opacity-70")}>
      {arrow} {label}
    </span>
  );
}

/** EmptyState mirrors the dashboard's no-data CTA: the Ledger is empty until the first
 *  import lands Measurements. */
function EmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <p className="text-sm text-muted-foreground">No data yet — import your Apple Health export to fill the Ledger.</p>
      <Button asChild>
        <Link to="/import">
          <Download className="size-4" /> Import data
        </Link>
      </Button>
    </div>
  );
}

/** NotesSection is the third face of the Data page, beside the Ledger's overview and
 *  its per-Metric detail: every Annotation the Account has written, most recent span
 *  first (ADR 0030). It is the only view that reaches a note outside the current
 *  range, and the only one that answers "what have I written down", which no chart
 *  can. A row opens the same dialog that wrote it. */
function NotesSection() {
  const notes = useAllAnnotations();
  const [editing, setEditing] = React.useState<Annotation | null>(null);
  const [open, setOpen] = React.useState(false);

  const rows = notes.data ?? [];
  const edit = (a: Annotation) => {
    setEditing(a);
    setOpen(true);
  };
  const add = () => {
    setEditing(null);
    setOpen(true);
  };

  return (
    <section className="mt-8 space-y-2">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-medium">Notes</h2>
        <Button variant="outline" size="sm" className="h-7" onClick={add}>
          <StickyNote className="size-3.5" /> Add a note
        </Button>
      </div>

      {rows.length === 0 ? (
        <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
          A note is a dated line about what was happening: an illness, a trip, a change of
          program. It shows on every chart covering those days, so a curve can be read
          against it.
        </p>
      ) : (
        <div className="divide-y rounded-lg border">
          {rows.map((a) => (
            <button
              key={a.id}
              type="button"
              onClick={() => edit(a)}
              className="flex w-full items-baseline gap-3 px-3 py-2 text-left transition-colors hover:bg-accent/50"
            >
              <span className="w-40 shrink-0 text-xs tabular-nums text-muted-foreground">
                {a.ends_on ? `${a.starts_on} → ${a.ends_on}` : a.starts_on}
              </span>
              <span className="min-w-0 flex-1">
                <span className="text-sm">{a.label}</span>
                {a.body && <span className="ml-2 truncate text-xs text-muted-foreground">{a.body}</span>}
              </span>
            </button>
          ))}
        </div>
      )}

      <AnnotationDialog open={open} onOpenChange={setOpen} annotation={editing} />
    </section>
  );
}
