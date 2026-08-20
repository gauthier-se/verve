import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { Map as MapIcon } from "lucide-react";
import { useSessions, type SessionFilter } from "@/hooks/use-sessions";
import { activityIcon, ACTIVITY_GROUPS } from "@/lib/activities";
import { formatExact, formatSessionDuration } from "@/lib/format";
import { RANGE_PRESETS, type RangeTokens } from "@/lib/time-range";
import type { Activity, Session, SessionTotals } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { CenteredSpinner } from "./spinner";

type Preset = (typeof RANGE_PRESETS)[number]["value"];

/** SessionsPage is the workout list: what you did, newest first.
 *
 *  It carries its own range and its own Activity filters, deliberately
 *  independent of the active Dashboard's (ADR 0028). A workout list is browsed by
 *  "what did I do", and binding it to a Dashboard's window would mean switching
 *  Dashboard to find last year's race.
 *
 *  A Session is an entity and not a Metric, so there is no bucket, no Baseline
 *  and no summary rule here: a row is a workout, and clicking it opens that
 *  workout rather than a period containing it. */
export function SessionsPage() {
  const [preset, setPreset] = React.useState<Preset>("3m");
  const [groups, setGroups] = React.useState<Activity["group"][]>([]);
  const range: RangeTokens = { preset, from: null, to: null };
  const filter: SessionFilter = { range, groups };

  const query = useSessions(filter);
  const pages = query.data?.pages ?? [];
  const sessions = pages.flatMap((p) => p.sessions);
  const totals = pages[0]?.totals;

  const toggleGroup = (g: Activity["group"]) =>
    setGroups((current) => (current.includes(g) ? current.filter((x) => x !== g) : [...current, g]));

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
        <h1 className="text-xl font-semibold">Workouts</h1>
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
      </header>

      <div className="flex flex-wrap items-center gap-1.5 border-b px-6 py-2">
        {ACTIVITY_GROUPS.map((g) => (
          <Button
            key={g.value}
            variant={groups.includes(g.value) ? "secondary" : "ghost"}
            size="sm"
            className="h-7 px-2.5"
            onClick={() => toggleGroup(g.value)}
          >
            {g.label}
          </Button>
        ))}
        {groups.length > 0 && (
          <Button variant="ghost" size="sm" className="h-7 px-2.5 text-muted-foreground" onClick={() => setGroups([])}>
            Clear
          </Button>
        )}
      </div>

      {totals && <TotalsHeader totals={totals} />}

      <div className="flex-1 overflow-y-auto px-6 py-4">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : sessions.length === 0 ? (
          <EmptyState />
        ) : (
          <>
            <SessionTable sessions={sessions} />
            {query.hasNextPage && (
              <div className="flex justify-center py-4">
                <Button variant="outline" size="sm" onClick={() => query.fetchNextPage()} disabled={query.isFetchingNextPage}>
                  {query.isFetchingNextPage ? "Loading…" : "Load more"}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

/** TotalsHeader states what it is totalling and over how many workouts. The
 *  figures come from the server and describe the whole filter, not the loaded
 *  pages: a total without its domain reads as a truth and is not one, which is
 *  the same rule the sleep read path follows with its Night count. */
function TotalsHeader({ totals }: { totals: SessionTotals }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1 border-b bg-card/40 px-6 py-2 text-sm">
      <span className="font-medium">
        {totals.count} {totals.count === 1 ? "workout" : "workouts"}
      </span>
      <span className="text-muted-foreground">{formatSessionDuration(totals.duration)} total</span>
      {totals.distance !== undefined && (
        <span className="text-muted-foreground">{formatExact(totals.distance)} km</span>
      )}
      {totals.energy !== undefined && (
        <span className="text-muted-foreground">{formatExact(totals.energy)} kcal</span>
      )}
      <span className="ml-auto text-xs text-muted-foreground">
        {formatDay(totals.from)} to {formatDay(totals.to)}
      </span>
    </div>
  );
}

function SessionTable({ sessions }: { sessions: Session[] }) {
  const navigate = useNavigate();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Date</TableHead>
          <TableHead>Activity</TableHead>
          <TableHead className="text-right">Duration</TableHead>
          <TableHead className="text-right">Distance</TableHead>
          <TableHead className="text-right">Energy</TableHead>
          <TableHead>Source</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sessions.map((s) => {
          const Icon = activityIcon(s.activity);
          return (
            <TableRow
              key={s.id}
              className="cursor-pointer"
              onClick={() => navigate({ to: "/workouts/$sessionId", params: { sessionId: String(s.id) } })}
            >
              <TableCell className="whitespace-nowrap">{formatDayTime(s.start_at)}</TableCell>
              <TableCell>
                <span className="flex items-center gap-2">
                  <Icon className="size-4 text-muted-foreground" />
                  {s.activity.label}
                  {s.has_route && <MapIcon className="size-3.5 text-muted-foreground" aria-label="Has a route" />}
                </span>
              </TableCell>
              <TableCell className="text-right tabular-nums">{formatSessionDuration(s.duration)}</TableCell>
              <TableCell className={cn("text-right tabular-nums", s.distance === undefined && "text-muted-foreground")}>
                {s.distance === undefined ? "—" : `${formatExact(s.distance)} km`}
              </TableCell>
              <TableCell className={cn("text-right tabular-nums", s.energy === undefined && "text-muted-foreground")}>
                {s.energy === undefined ? "—" : `${formatExact(s.energy)} kcal`}
              </TableCell>
              <TableCell className="text-muted-foreground">{s.source}</TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
      <p className="text-sm text-muted-foreground">No workouts in this range.</p>
      <Button asChild variant="outline" size="sm">
        <Link to="/import">Import an export</Link>
      </Button>
    </div>
  );
}

/** formatDay and formatDayTime render a stored UTC instant. The stored times are
 *  UTC throughout Verve, so these read the instant rather than re-zoning it. */
export function formatDay(iso: string): string {
  return new Date(iso).toLocaleDateString("fr-FR", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" });
}

export function formatDayTime(iso: string): string {
  const d = new Date(iso);
  return `${d.toLocaleDateString("fr-FR", { day: "numeric", month: "short", timeZone: "UTC" })} ${d.toLocaleTimeString(
    "fr-FR",
    { hour: "2-digit", minute: "2-digit", timeZone: "UTC" },
  )}`;
}
