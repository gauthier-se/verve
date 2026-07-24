import * as React from "react";
import { Link } from "@tanstack/react-router";
import { Check, ChevronDown, ChevronRight, Copy, Download } from "lucide-react";
import { useLedger } from "@/hooks/use-ledger";
import { formatExact, formatSummaryValue } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { copyTsv, tsvNumber } from "@/lib/clipboard";
import { RANGE_PRESETS, type RangeTokens } from "@/lib/time-range";
import type { LedgerRow } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { CenteredSpinner } from "./spinner";
import { LedgerDetailTable } from "./ledger-detail-table";
import { MetricIcon } from "./metric-icon";
import { formatBucket } from "./panel-chart";

type Preset = (typeof RANGE_PRESETS)[number]["value"];

/** DataPage is the Ledger (ADR 0021): the numbers behind the graphs as tables. A
 *  scoreboard lists every Metric with data — latest value, ~7-day and ~30-day figures,
 *  and a week-over-week delta — and expanding a row reveals that Metric's chronological
 *  detail table at a chosen granularity, over a page-wide date range. */
export function DataPage() {
  const ledger = useLedger();
  const [expanded, setExpanded] = React.useState<string | null>(null);
  const [preset, setPreset] = React.useState<Preset>("1y");
  const range: RangeTokens = { preset, from: null, to: null };

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <h1 className="text-xl font-semibold">Data</h1>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">Detail range</span>
          <div className="flex items-center rounded-md border p-0.5">
            {RANGE_PRESETS.map((p) => (
              <Button
                key={p.value}
                variant={preset === p.value ? "secondary" : "ghost"}
                size="sm"
                className="h-7 px-2.5"
                onClick={() => setPreset(p.value)}
              >
                {p.label}
              </Button>
            ))}
          </div>
        </div>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {ledger.isLoading ? (
          <CenteredSpinner />
        ) : (ledger.data?.length ?? 0) === 0 ? (
          <EmptyState />
        ) : (
          <Scoreboard rows={ledger.data ?? []} expanded={expanded} onToggle={setExpanded} range={range} />
        )}
      </div>
    </div>
  );
}

/** Scoreboard is the overview table: one row per Metric, expandable into its detail. */
function Scoreboard({
  rows,
  expanded,
  onToggle,
  range,
}: {
  rows: LedgerRow[];
  expanded: string | null;
  onToggle: (metric: string | null) => void;
  range: RangeTokens;
}) {
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
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => {
              const isOpen = expanded === r.metric;
              return (
                <React.Fragment key={r.metric}>
                  <TableRow className="cursor-pointer" onClick={() => onToggle(isOpen ? null : r.metric)}>
                    <TableCell className="font-medium">
                      <span className="inline-flex items-center gap-1.5">
                        {isOpen ? (
                          <ChevronDown className="size-4 text-muted-foreground" />
                        ) : (
                          <ChevronRight className="size-4 text-muted-foreground" />
                        )}
                        <MetricIcon slug={r.metric} className="size-4" />
                        {metricLabel(r.metric)}
                      </span>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {r.latest ? (
                        <span title={`${formatExact(r.latest.value)} ${r.unit}`.trim()}>
                          {formatSummaryValue(r.latest.value, r.aggregation)}
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
                  </TableRow>
                  {isOpen && (
                    <TableRow className="hover:bg-transparent">
                      <TableCell colSpan={5} className="bg-muted/30 p-3">
                        <LedgerDetailTable
                          metric={r.metric}
                          unit={r.unit}
                          aggregation={r.aggregation}
                          range={range}
                        />
                      </TableCell>
                    </TableRow>
                  )}
                </React.Fragment>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function WindowCell({ value, aggregation }: { value: number | undefined; aggregation: LedgerRow["aggregation"] }) {
  return (
    <TableCell className="text-right tabular-nums">
      {value === undefined ? (
        <span className="text-muted-foreground">—</span>
      ) : (
        formatSummaryValue(value, aggregation)
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
      : formatSummaryValue(Math.abs(row.delta_abs), row.aggregation);
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
