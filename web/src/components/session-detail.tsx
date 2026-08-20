import * as React from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, Download } from "lucide-react";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { useSession, useSessionRoutes } from "@/hooks/use-sessions";
import { activityIcon } from "@/lib/activities";
import { formatExact, formatPace, formatSessionDuration, formatSpeed } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import type { RouteGeometry, SessionStat } from "@/lib/types";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { CenteredSpinner } from "./spinner";

import { formatDayTime } from "./sessions-page";

// Lazily loaded: leaflet and its stylesheet are only ever needed by a workout that
// has a trace, and the rest of the app should not carry them on first paint.
const RouteMap = React.lazy(() => import("./route-map"));

const STAT_LABEL: Record<SessionStat["stat"], string> = {
  sum: "Total",
  average: "Average",
  min: "Minimum",
  max: "Maximum",
};

/** SessionDetail is one workout: its figures, the summary statistics its device
 *  reported, and its Routes drawn.
 *
 *  Every distance on this page is the Session's own total, which is what the
 *  device measured. The trace has a length of its own and it is deliberately not
 *  shown as a figure: it is our simplified reconstruction, it disagrees with the
 *  watch by a little, and a page reading 9,7 km under a trace Apple calls 10,0 km
 *  makes its reader distrust every other number on it (ADR 0028). The geometric
 *  length appears only as the profile's axis. */
export function SessionDetailPage() {
  const { sessionId } = useParams({ from: "/workouts/$sessionId" });
  const id = Number(sessionId);
  const detail = useSession(id);
  const routes = useSessionRoutes(id, detail.data?.session.has_route ?? false);

  if (detail.isLoading) return <CenteredSpinner />;

  if (!detail.data) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
        <p className="text-sm text-muted-foreground">That workout is not in your data.</p>
        <Button asChild variant="outline" size="sm">
          <Link to="/workouts">
            <ArrowLeft className="size-3.5" /> Back to Workouts
          </Link>
        </Button>
      </div>
    );
  }

  const { session, stats } = detail.data;
  const Icon = activityIcon(session.activity);
  const speed = session.distance !== undefined && session.duration > 0
    ? session.distance / (session.duration / 3600)
    : undefined;

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b px-6 py-3">
        <Button asChild variant="ghost" size="icon" className="size-7" aria-label="Back to Workouts">
          <Link to="/workouts">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
        <Icon className="size-5" />
        <h1 className="text-xl font-semibold">{session.activity.label}</h1>
        <span className="text-sm text-muted-foreground">{formatDayTime(session.start_at)}</span>
        <span className="ml-auto text-xs text-muted-foreground">{session.source}</span>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto p-6">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Figure label="Duration" value={formatSessionDuration(session.duration)} />
          <Figure label="Distance" value={session.distance === undefined ? "—" : `${formatExact(session.distance)} km`} />
          <Figure label="Energy" value={session.energy === undefined ? "—" : `${formatExact(session.energy)} kcal`} />
          <Figure
            label={session.activity.reading === "pace" ? "Pace" : "Speed"}
            value={
              speed === undefined || session.activity.reading === "none"
                ? "—"
                : session.activity.reading === "pace"
                  ? formatPace(speed)
                  : formatSpeed(speed)
            }
          />
        </div>

        {stats.length > 0 && (
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Recorded by the device</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              {stats.map((s) => (
                <div key={`${s.metric}-${s.stat}`}>
                  <p className="text-xs text-muted-foreground">
                    {STAT_LABEL[s.stat]} {metricLabel(s.metric).toLowerCase()}
                  </p>
                  <p className="text-lg font-semibold tabular-nums">
                    {formatExact(s.value)} <span className="text-xs font-normal text-muted-foreground">{s.unit}</span>
                  </p>
                </div>
              ))}
            </CardContent>
          </Card>
        )}

        {routes.data && routes.data.length > 0 && (
          <RouteSection sessionId={id} routes={routes.data} reading={session.activity.reading} />
        )}
      </div>
    </div>
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="p-4">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="text-2xl font-semibold tabular-nums">{value}</p>
      </CardContent>
    </Card>
  );
}

function RouteSection({
  sessionId,
  routes,
  reading,
}: {
  sessionId: number;
  routes: RouteGeometry[];
  reading: "pace" | "speed" | "none";
}) {
  const climb = routes.reduce((sum, r) => sum + r.profiles.ascent_m, 0);
  const samples = routes.flatMap((r) => r.profiles.samples);
  const hasElevation = samples.some((s) => s.ele !== undefined);
  const hasSpeed = samples.some((s) => s.speed !== undefined);

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm">
          Route{routes.length > 1 ? `s (${routes.length} segments)` : ""}
          {climb > 0 && <span className="ml-2 font-normal text-muted-foreground">{Math.round(climb)} m climbed</span>}
        </CardTitle>
        <div className="flex gap-1">
          {routes.map((r, i) => (
            <Button key={r.id} asChild variant="ghost" size="sm" className="h-7 px-2">
              {/* The stored file itself, not our simplified version: your data is yours. */}
              <a href={`/v1/sessions/${sessionId}/routes/${r.id}.gpx`} download>
                <Download className="size-3.5" /> GPX{routes.length > 1 ? ` ${i + 1}` : ""}
              </a>
            </Button>
          ))}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <React.Suspense fallback={<div className="h-80 w-full rounded-lg border" />}>
          <RouteMap routes={routes} className="h-80 w-full rounded-lg border" />
        </React.Suspense>
        {hasElevation && <Profile samples={samples} field="ele" label="Elevation (m)" />}
        {hasSpeed && reading !== "none" && (
          <Profile samples={samples} field="speed" label={reading === "pace" ? "Pace" : "Speed (km/h)"} pace={reading === "pace"} />
        )}
      </CardContent>
    </Card>
  );
}

/** Profile draws one series against distance travelled. The axis is the route's
 *  own geometric length, which is the one place that figure legitimately appears. */
function Profile({
  samples,
  field,
  label,
  pace,
}: {
  samples: { km: number; ele?: number; speed?: number }[];
  field: "ele" | "speed";
  label: string;
  pace?: boolean;
}) {
  const data = samples.filter((s) => s[field] !== undefined);
  if (data.length < 2) return null;

  return (
    <div>
      <p className="mb-1 text-xs text-muted-foreground">{label}</p>
      <ResponsiveContainer width="100%" height={140}>
        <LineChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-border" vertical={false} />
          <XAxis
            dataKey="km"
            type="number"
            domain={["dataMin", "dataMax"]}
            tickFormatter={(v: number) => `${v.toFixed(1)}`}
            tick={{ fontSize: 11 }}
            className="fill-muted-foreground"
          />
          <YAxis
            tick={{ fontSize: 11 }}
            width={44}
            className="fill-muted-foreground"
            tickFormatter={(v: number) => (pace ? formatPace(v).replace("/km", "") : String(Math.round(v)))}
          />
          <Tooltip
            contentStyle={{ fontSize: 12 }}
            labelFormatter={(v: number) => `${v.toFixed(2)} km`}
            formatter={(v: number) => [pace ? formatPace(v) : formatExact(v), label]}
          />
          <Line type="monotone" dataKey={field} dot={false} stroke="hsl(var(--chart-1))" strokeWidth={2} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
