import {
  createColumnHelper,
  tableFeatures,
  useTable,
} from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import type { ImportSummary } from "@/api/generated";
import { ImportDrawer } from "@/components/imports/import-drawer";
import {
  counterLabel,
  type counterNames,
  formatImportDate,
  originLabel,
  statusLabel,
} from "@/components/imports/labels";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDuration, formatNumber } from "@/i18n/format";

/** The counters that the table shows, the others being in the drawer. */
const tableCounters = [
  "received",
  "inserted",
  "updated",
  "duplicates",
  "invalid",
] as const satisfies readonly (typeof counterNames)[number][];

const features = tableFeatures({});
const columnHelper = createColumnHelper<typeof features, ImportSummary>();

interface ImportsTableProps {
  imports: ImportSummary[];
  /** The timezone of the user, in which the dates are shown. */
  timeZone: string | null;
  "aria-labelledby": string;
}

/**
 * The imports of the user, in the order given, with their origin, client,
 * creation date, duration, status and main counters, and an action that
 * opens the drawer of each. The table scrolls on its own when it is wider
 * than the page.
 */
export function ImportsTable({
  imports,
  timeZone,
  "aria-labelledby": labelledBy,
}: ImportsTableProps) {
  const { t, i18n } = useTranslation();
  const lng = i18n.language;

  const columns = useMemo(
    () =>
      columnHelper.columns([
        columnHelper.accessor("origin", {
          header: () => t("imports.columns.origin"),
          cell: ({ getValue }) => originLabel(t, getValue()),
        }),
        columnHelper.display({
          id: "client",
          header: () => t("imports.columns.client"),
          cell: ({ row }) => <Client summary={row.original} t={t} />,
        }),
        columnHelper.accessor("createdAt", {
          header: () => t("imports.columns.date"),
          cell: ({ getValue }) => formatImportDate(getValue(), lng, timeZone),
        }),
        columnHelper.display({
          id: "duration",
          header: () => t("imports.columns.duration"),
          cell: ({ row }) => durationLabel(row.original, t, lng),
        }),
        columnHelper.accessor("status", {
          header: () => t("imports.columns.status"),
          cell: ({ getValue }) => (
            <Badge
              variant={getValue() === "failed" ? "destructive" : "outline"}
            >
              {statusLabel(t, getValue())}
            </Badge>
          ),
        }),
        ...tableCounters.map((counter) =>
          columnHelper.accessor((summary) => summary.report.total[counter], {
            id: counter,
            header: () => counterLabel(t, counter),
            cell: ({ getValue }) => (
              <span className="tabular-nums">
                {formatNumber(getValue(), lng)}
              </span>
            ),
          }),
        ),
        columnHelper.display({
          id: "actions",
          header: () => t("imports.columns.actions"),
          cell: ({ row }) => (
            <ImportDrawer summary={row.original} timeZone={timeZone} />
          ),
        }),
      ]),
    [t, lng, timeZone],
  );

  const table = useTable({
    features,
    columns,
    data: imports,
    getRowId: (summary) => String(summary.id),
  });

  return (
    <Table aria-labelledby={labelledBy}>
      <TableHeader>
        {table.getHeaderGroups().map((group) => (
          <TableRow key={group.id}>
            {group.headers.map((header) => (
              <TableHead key={header.id}>
                {header.isPlaceholder ? null : (
                  <table.FlexRender header={header} />
                )}
              </TableHead>
            ))}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <TableRow key={row.id}>
            {row.getAllCells().map((cell) => (
              <TableCell key={cell.id}>
                <table.FlexRender cell={cell} />
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

/**
 * The client that sent the data, by its name and version, and the token it
 * used, or none when the import has neither.
 */
function Client({ summary, t }: { summary: ImportSummary; t: TFunction }) {
  const name = summary.client?.name;
  const version = summary.client?.version;
  if (name === undefined && summary.tokenName === undefined) {
    return t("imports.noClient");
  }
  return (
    <span className="flex flex-col">
      {name !== undefined && (
        <span>{version === undefined ? name : `${name} ${version}`}</span>
      )}
      {summary.tokenName !== undefined && (
        <span className="text-xs text-muted-foreground">
          {t("imports.token", { name: summary.tokenName })}
        </span>
      )}
    </span>
  );
}

/**
 * How long an import ran, from its first run to its end, or what keeps it
 * from having a duration yet.
 */
function durationLabel(summary: ImportSummary, t: TFunction, lng: string) {
  if (summary.startedAt === undefined) {
    return t("imports.notStarted");
  }
  if (summary.finishedAt === undefined) {
    return t("imports.inProgress");
  }
  const seconds =
    (Date.parse(summary.finishedAt) - Date.parse(summary.startedAt)) / 1000;
  return formatDuration(seconds, lng);
}
