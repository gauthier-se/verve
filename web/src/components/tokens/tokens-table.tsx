import {
  createColumnHelper,
  tableFeatures,
  useTable,
} from "@tanstack/react-table";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import type { ApiToken } from "@/api/generated";
import { RevokeTokenDialog } from "@/components/tokens/revoke-token-dialog";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDate } from "@/i18n/format";

/** How long a token goes without use before its row suggests revoking it. */
const unusedAfterMs = 90 * 24 * 60 * 60 * 1000;

const features = tableFeatures({});
const columnHelper = createColumnHelper<typeof features, ApiToken>();

interface TokensTableProps {
  tokens: ApiToken[];
  /** The timezone of the user, in which the dates are shown. */
  timeZone: string | null;
  "aria-labelledby": string;
}

/**
 * The tokens of the user, in the order given, with their scopes, prefix,
 * dates and a revoke action. A revoked token says so instead. An active token
 * not used for 90 days, or never used since its creation 90 days ago, suggests
 * revoking it. The table scrolls on its own when it is wider than the page.
 */
export function TokensTable({
  tokens,
  timeZone,
  "aria-labelledby": labelledBy,
}: TokensTableProps) {
  const { t, i18n } = useTranslation();
  const lng = i18n.language;
  // The time the page opened, against which the tokens are unused or expired.
  const [now] = useState(Date.now);

  const columns = useMemo(() => {
    const formatTokenDate = (iso: string) =>
      formatDate(new Date(iso), lng, {
        dateStyle: "medium",
        timeZone: timeZone ?? undefined,
      });

    return columnHelper.columns([
      columnHelper.accessor("name", {
        header: () => t("tokens.columns.name"),
        cell: ({ getValue }) => (
          <span className="font-medium">{getValue()}</span>
        ),
      }),
      columnHelper.accessor("scopes", {
        header: () => t("tokens.columns.scopes"),
        cell: ({ getValue }) => (
          <span className="flex gap-1">
            {getValue().map((scope) => (
              <Badge key={scope} variant="secondary" className="font-mono">
                {scope}
              </Badge>
            ))}
          </span>
        ),
      }),
      columnHelper.accessor("prefix", {
        header: () => t("tokens.columns.prefix"),
        cell: ({ getValue }) => <code className="font-mono">{getValue()}</code>,
      }),
      columnHelper.accessor("createdAt", {
        header: () => t("tokens.columns.created"),
        cell: ({ getValue }) => formatTokenDate(getValue()),
      }),
      columnHelper.accessor("lastUsedAt", {
        header: () => t("tokens.columns.lastUsed"),
        cell: ({ getValue, row }) => {
          const lastUsedAt = getValue();
          return (
            <span className="flex flex-col gap-1">
              {lastUsedAt === undefined
                ? t("tokens.never")
                : formatTokenDate(lastUsedAt)}
              {isUnused(row.original, now) && (
                <span className="max-w-56 text-xs whitespace-normal text-muted-foreground">
                  {t("tokens.unused")}
                </span>
              )}
            </span>
          );
        },
      }),
      columnHelper.accessor("expiresAt", {
        header: () => t("tokens.columns.expires"),
        cell: ({ getValue }) => {
          const expiresAt = getValue();
          if (expiresAt === undefined) {
            return t("tokens.never");
          }
          return (
            <span className="flex items-center gap-2">
              {formatTokenDate(expiresAt)}
              {isPast(expiresAt, now) && (
                <Badge variant="outline">{t("tokens.expired")}</Badge>
              )}
            </span>
          );
        },
      }),
      columnHelper.display({
        id: "actions",
        header: () => t("tokens.columns.actions"),
        cell: ({ row }) =>
          row.original.revokedAt === undefined ? (
            <RevokeTokenDialog token={row.original} />
          ) : (
            <Badge variant="outline">{t("tokens.revoked")}</Badge>
          ),
      }),
    ]);
  }, [t, lng, timeZone, now]);

  const table = useTable({
    features,
    columns,
    data: tokens,
    getRowId: (token) => String(token.id),
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
 * Tells whether an active token went 90 days or more without use, counting
 * from its creation when it was never used.
 */
function isUnused(token: ApiToken, now: number): boolean {
  if (
    token.revokedAt !== undefined ||
    (token.expiresAt !== undefined && isPast(token.expiresAt, now))
  ) {
    return false;
  }
  const since = Date.parse(token.lastUsedAt ?? token.createdAt);
  return now - since >= unusedAfterMs;
}

/** Tells whether `iso` is before `now`. A token is valid until its expiry. */
function isPast(iso: string, now: number): boolean {
  return Date.parse(iso) < now;
}
