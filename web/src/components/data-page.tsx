import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useHotkeys } from "react-hotkeys-hook";
import { Check, ChevronRight, Copy, Download, Pencil, StickyNote } from "lucide-react";
import { useAllAnnotations } from "@/hooks/use-annotations";
import { useMetricMap } from "@/hooks/use-catalog";
import { useLedger } from "@/hooks/use-ledger";
import { formatDuration, formatExact, formatSummaryValue } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import { textMatcher } from "@/lib/search";
import { copyTsv, tsvNumber } from "@/lib/clipboard";
import type { Annotation, LedgerRow } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Chip } from "./ui/figure";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { CenteredSpinner } from "./spinner";
import { AnnotationDialog } from "./annotation-dialog";
import { ManualEntryDialog } from "./manual-entry-dialog";
import { MetricIcon } from "./metric-icon";
import { SearchField } from "./search-field";
import { formatBucket } from "./panel-chart";

/** Nature is the scoreboard's second filter: where a Metric came from. */
type Nature = "all" | "imported" | "derived";

const NATURES: { value: Nature; label: string }[] = [
  { value: "all", label: "All" },
  { value: "imported", label: "Imported" },
  { value: "derived", label: "Derived" },
];

/** DataPage is the Ledger (ADR 0021): the numbers behind the graphs as tables. A
 *  scoreboard lists every Metric with data — latest value, ~7-day and ~30-day figures,
 *  and a week-over-week delta — and a row opens that Metric's own page (chart, highs
 *  and lows, and the chronological detail table).
 *
 *  Every Metric is the problem this page grows into: an Apple export lands well over a
 *  hundred rows, most of them nutrients nobody reads, and the one you came for is
 *  somewhere in the middle of them. So the scoreboard is filtered rather than paged or
 *  pre-grouped — the list stays complete and honest, and the search narrows it to the
 *  handful you asked for. The filter is client-side on purpose: the Ledger is one
 *  small folded row per Metric and already in hand, so narrowing it is a keystroke
 *  and never a round trip. */
export function DataPage() {
  const ledger = useLedger();
  const catalog = useMetricMap();
  const notes = useAllAnnotations();
  const [entryOpen, setEntryOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [nature, setNature] = React.useState<Nature>("all");
  const searchRef = React.useRef<HTMLInputElement>(null);

  // "/" puts the cursor in the search box, the one convention every list-and-filter
  // interface shares (react-hotkeys-hook, ADR 0013). It does not fire while a field
  // already has focus, so typing a slash into a note stays a slash.
  useHotkeys("/", () => searchRef.current?.focus(), { preventDefault: true });

  const rows = ledger.data ?? [];
  const empty = rows.length === 0;

  const shown = React.useMemo(() => {
    const matches = textMatcher(query);
    return rows.filter((r) => {
      if (!matches(r.metric, r.unit)) return false;
      if (nature === "all") return true;
      // A Metric the Catalog has not answered for yet reads as imported: the Ledger
      // arrives before the Catalog on a cold load, and a row blinking out of the
      // table as a second request lands is worse than a row filed under the common
      // case for a moment.
      return (catalog.map.get(r.metric)?.nature ?? "imported") === nature;
    });
  }, [rows, query, nature, catalog.map]);

  const filtering = query.trim() !== "" || nature !== "all";
  const clear = () => {
    setQuery("");
    setNature("all");
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <h1 className="text-xl font-semibold">Data</h1>
          {/* The count only appears once something is filtered. "148 of 148" is not
              a fact anybody needs; "6 of 148" is the whole point of the box. */}
          {filtering && !empty && (
            <Chip>
              {shown.length} of {rows.length} metrics
            </Chip>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!empty && (
            <>
              <SearchField
                ref={searchRef}
                value={query}
                onChange={setQuery}
                label="Filter the metrics"
                placeholder="Search metrics…"
                hint="/"
                className="w-52"
              />
              <NatureFilter value={nature} onChange={setNature} />
            </>
          )}
          <Button variant="outline" size="sm" className="h-8" onClick={() => setEntryOpen(true)}>
            <Pencil className="size-3.5" /> Enter a value
          </Button>
        </div>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {ledger.isLoading ? (
          <CenteredSpinner />
        ) : empty ? (
          <EmptyState />
        ) : shown.length === 0 ? (
          <NoMatches query={query} onClear={clear} />
        ) : (
          <Scoreboard rows={shown} total={rows.length} />
        )}
        {/* The notes stand on their own: they need no import, so an Account with
            nothing but notes still finds them here. */}
        {!ledger.isLoading && (!empty || (notes.data?.length ?? 0) > 0) && <NotesSection />}
      </div>

      <ManualEntryDialog open={entryOpen} onOpenChange={setEntryOpen} />
    </div>
  );
}

/** NatureFilter separates the Metrics Verve computes from the ones it was given
 *  (ADR 0014). It is the one split the scoreboard can offer honestly: the Catalog
 *  states a Metric's nature, where a grouping by body system or by nutrient family
 *  would be a taxonomy invented in the client and wrong at its edges. */
function NatureFilter({ value, onChange }: { value: Nature; onChange: (v: Nature) => void }) {
  return (
    <div className="flex items-center rounded-md border p-0.5">
      {NATURES.map((n) => (
        <Button
          key={n.value}
          variant={value === n.value ? "secondary" : "ghost"}
          size="sm"
          className="h-7 px-2.5 text-xs"
          onClick={() => onChange(n.value)}
        >
          {n.label}
        </Button>
      ))}
    </div>
  );
}

/** Scoreboard is the overview table: one row per Metric, linking to its own page. */
function Scoreboard({ rows, total }: { rows: LedgerRow[]; total: number }) {
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
      {/* Copy takes the filtered rows, not all of them: the button sits under a
          search box, and a button that copies more than the screen shows is a
          spreadsheet nobody asked for. */}
      <div className="flex justify-end">
        <Button variant="outline" size="sm" className="h-7" onClick={onCopy}>
          {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          {copied ? "Copied" : rows.length < total ? `Copy ${rows.length} rows` : "Copy table"}
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

/** NoMatches is the filtered-to-nothing state, and it is deliberately not the
 *  no-data one: the Ledger is full, the query is simply too narrow. Saying "no data
 *  yet" here would send someone to the importer for data they already have. */
function NoMatches({ query, onClear }: { query: string; onClear: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
      <p className="text-sm text-muted-foreground">
        {query.trim() ? <>No metric matches “{query.trim()}”.</> : "No metric matches this filter."}
      </p>
      <Button variant="outline" size="sm" onClick={onClear}>
        Clear the filter
      </Button>
    </div>
  );
}

/** EmptyState mirrors the dashboard's no-data CTA: the Ledger is empty until the first
 *  import lands Measurements. */
function EmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <p className="text-sm text-muted-foreground">No data yet: import a health export to fill the Ledger.</p>
      <Button asChild>
        <Link to="/import">
          <Download className="size-4" /> Import data
        </Link>
      </Button>
    </div>
  );
}

/** How many notes it takes before the section carries a filter of its own. Under it
 *  the whole list is on the screen and a search box would be furniture. */
const NOTES_FILTER_FROM = 8;

/** NotesSection is the third face of the Data page, beside the Ledger's overview and
 *  its per-Metric detail: every Annotation the Account has written, most recent span
 *  first (ADR 0030). It is the only view that reaches a note outside the current
 *  range, and the only one that answers "what have I written down", which no chart
 *  can. A row opens the same dialog that wrote it.
 *
 *  Its search is its own rather than the header's. A note and a Metric are different
 *  kinds of thing, and one box narrowing both would answer "flu" with an empty
 *  scoreboard beside the note that was actually wanted. This one searches the label,
 *  the body and the dates, so a bare year finds the season. */
function NotesSection() {
  const notes = useAllAnnotations();
  const [editing, setEditing] = React.useState<Annotation | null>(null);
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");

  const all = notes.data ?? [];
  const shown = React.useMemo(() => {
    const matches = textMatcher(query);
    return all.filter((a) => matches(a.label, a.body, a.starts_on, a.ends_on));
  }, [all, query]);

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
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <h2 className="text-sm font-medium">Notes</h2>
          {query.trim() !== "" && (
            <Chip>
              {shown.length} of {all.length}
            </Chip>
          )}
        </div>
        <div className="flex items-center gap-2">
          {all.length >= NOTES_FILTER_FROM && (
            <SearchField
              value={query}
              onChange={setQuery}
              label="Filter the notes"
              placeholder="Search notes…"
              className="w-44"
            />
          )}
          <Button variant="outline" size="sm" className="h-8" onClick={add}>
            <StickyNote className="size-3.5" /> Add a note
          </Button>
        </div>
      </div>

      {all.length === 0 ? (
        <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
          A note is a dated line about what was happening: an illness, a trip, a change of
          program. It shows on every chart covering those days, so a curve can be read
          against it.
        </p>
      ) : shown.length === 0 ? (
        <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
          No note matches “{query.trim()}”.
        </p>
      ) : (
        <div className="divide-y rounded-lg border">
          {shown.map((a) => (
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
