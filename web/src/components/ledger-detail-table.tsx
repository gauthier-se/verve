import * as React from "react";
import { ArrowDown, ArrowUp, Check, Copy } from "lucide-react";
import { useSeries } from "@/hooks/use-series";
import { computeDelta, formatBucketKey, formatDuration, formatExact } from "@/lib/format";
import { copyTsv, tsvNumber } from "@/lib/clipboard";
import { stageLabel, stagesPresent } from "@/lib/sleep";
import type { RangeTokens } from "@/lib/time-range";
import type { Aggregation, Bucket, Point } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { Meta, SectionTitle } from "./ui/figure";
import { CenteredSpinner } from "./spinner";

const BUCKETS: { value: Bucket; label: string }[] = [
  { value: "day", label: "Day" },
  { value: "week", label: "Week" },
  { value: "month", label: "Month" },
];

type SortKey = "period" | "value";

/** LedgerDetailTable is one Metric's Points as a chronological table (ADR 0021): the
 *  numbers behind its graph. A granularity toggle (day/week/month) refetches the
 *  Series; a per-row delta compares each Point to the previous one; header clicks sort;
 *  and Copy puts the visible table on the clipboard as TSV. It reuses GET /v1/series —
 *  the delta is a plain point-to-point read, not a re-aggregation (ADR 0012 holds). */
export function LedgerDetailTable({
  metric,
  unit,
  aggregation,
  range,
}: {
  metric: string;
  unit: string;
  aggregation: Aggregation | "";
  range: RangeTokens;
}) {
  const [bucket, setBucket] = React.useState<Bucket>("day");
  const [sort, setSort] = React.useState<{ key: SortKey; desc: boolean }>({ key: "period", desc: true });
  const [copied, setCopied] = React.useState(false);

  const query = useSeries({ metrics: [metric], range, bucket });
  const points = query.data?.series[0]?.points ?? [];
  const isAverage = aggregation === "average";
  // A duration_by_state Metric decomposes into Stages, so the table grows one column
  // per Stage present — the same shape the average rule's min/max columns already
  // take. A stacked bar is the chart whose segments are least readable by eye, which
  // makes "the numbers behind the curves" load-bearing here rather than nice to have.
  const isDuration = aggregation === "duration_by_state";
  const stages = React.useMemo(() => (isDuration ? stagesPresent(points) : []), [isDuration, points]);
  const showValue = (v: number) => (isDuration ? formatDuration(v) : formatExact(v));

  // Delta is versus the previous chronological Point, so it is computed on ascending
  // order before any display sort. Rows then sort by the chosen column.
  const rows = React.useMemo(() => {
    const withDelta = points.map((point, i) => ({
      point,
      delta: i > 0 ? computeDelta(point.value, points[i - 1].value, aggregation, false) : undefined,
    }));
    const dir = sort.desc ? -1 : 1;
    return [...withDelta].sort((a, b) => {
      const cmp = sort.key === "value" ? a.point.value - b.point.value : a.point.bucket.localeCompare(b.point.bucket);
      return cmp * dir;
    });
  }, [points, aggregation, sort]);

  const toggleSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: true }));

  const onCopy = async () => {
    const headers = [
      "Date",
      "Value",
      ...(isAverage ? ["Min", "Max"] : []),
      ...stages.map(stageLabel),
      isDuration ? "Nights" : "Readings",
    ];
    // The copied rows carry the raw bucket start, not the key: a spreadsheet reads
    // "2026-08-17" as a date and "2026-W34" as a string.
    const body = rows.map(({ point }) => [
      point.bucket,
      tsvNumber(point.value),
      ...(isAverage ? [numOrEmpty(point.min), numOrEmpty(point.max)] : []),
      ...stages.map((stage) => numOrEmpty(point.states?.[stage])),
      point.count === undefined ? "" : String(point.count),
    ]);
    await copyTsv(headers, body);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
        <div className="flex min-w-0 items-baseline gap-2.5">
          <SectionTitle>The numbers behind the curve</SectionTitle>
          <Meta>
            {bucket === "day" ? "daily" : bucket === "week" ? "weekly" : "monthly"} buckets · copyable
          </Meta>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-0.5 rounded-md border p-0.5">
            {BUCKETS.map((b) => (
              <button
                key={b.value}
                type="button"
                onClick={() => setBucket(b.value)}
                className={cn(
                  "rounded px-2 py-1 text-2xs transition-colors",
                  bucket === b.value
                    ? "bg-secondary font-medium text-secondary-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {b.label}
              </button>
            ))}
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 px-2.5 text-xs"
            onClick={onCopy}
            disabled={points.length === 0}
          >
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>
      </div>

      {query.isLoading ? (
        <CenteredSpinner />
      ) : query.isError ? (
        <p className="px-3 py-6 text-center text-sm text-muted-foreground">
          {query.error instanceof Error ? query.error.message : "Couldn’t load this metric."}
        </p>
      ) : points.length === 0 ? (
        <p className="px-3 py-6 text-center text-sm text-muted-foreground">No data in this range.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <SortHead label="Period" active={sort.key === "period"} desc={sort.desc} onClick={() => toggleSort("period")} />
              <SortHead
                label={`Value${unit ? ` (${unit})` : ""}`}
                active={sort.key === "value"}
                desc={sort.desc}
                onClick={() => toggleSort("value")}
                className="text-right"
              />
              {isAverage && <TableHead className="text-right">Min</TableHead>}
              {isAverage && <TableHead className="text-right">Max</TableHead>}
              {stages.map((stage) => (
                <TableHead key={stage} className="text-right">
                  {stageLabel(stage)}
                </TableHead>
              ))}
              {/* The evidence behind each row. An average of 52 over three hundred
                  readings and an average of 52 over two are the same number and not
                  the same fact, and this is the column that says which one it is. */}
              <TableHead className="text-right">{isDuration ? "Nights" : "Readings"}</TableHead>
              <TableHead className="text-right">Δ vs previous</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(({ point, delta }) => (
              <TableRow key={point.bucket}>
                <TableCell className="whitespace-nowrap font-mono tabular-nums text-muted-foreground">
                  {formatBucketKey(point.bucket, bucket)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">{showValue(point.value)}</TableCell>
                {isAverage && <TableCell className="text-right tabular-nums text-muted-foreground">{bandCell(point.min)}</TableCell>}
                {isAverage && <TableCell className="text-right tabular-nums text-muted-foreground">{bandCell(point.max)}</TableCell>}
                {stages.map((stage) => (
                  <TableCell key={stage} className="text-right font-mono tabular-nums text-muted-foreground">
                    {point.states?.[stage] === undefined ? "—" : formatDuration(point.states[stage])}
                  </TableCell>
                ))}
                <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                  {point.count === undefined ? "—" : point.count}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                  {delta ? `${delta.arrow} ${delta.label}` : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

/** SortHead is a clickable column header showing the active sort direction. */
function SortHead({
  label,
  active,
  desc,
  onClick,
  className,
}: {
  label: string;
  active: boolean;
  desc: boolean;
  onClick: () => void;
  className?: string;
}) {
  return (
    <TableHead className={className}>
      <button
        type="button"
        onClick={onClick}
        className={cn("inline-flex items-center gap-1 hover:text-foreground", active && "text-foreground")}
      >
        {label}
        {active && (desc ? <ArrowDown className="size-3" /> : <ArrowUp className="size-3" />)}
      </button>
    </TableHead>
  );
}

function bandCell(v: number | undefined): string {
  return v === undefined ? "—" : formatExact(v);
}

function numOrEmpty(v: Point["min"]): string {
  return v === undefined ? "" : tsvNumber(v);
}
