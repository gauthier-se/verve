import * as React from "react";
import { Link } from "@tanstack/react-router";
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceArea,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { useHistory } from "@/hooks/use-history";
import { useMetricMap } from "@/hooks/use-catalog";
import { AXIS, GRID, RECESSED, SERIES_COLORS } from "@/lib/chart";
import { formatDay, formatDayRange, formatExact } from "@/lib/format";
import { metricLabel } from "@/lib/metrics";
import type {
  HistoryBand,
  HistoryEvent,
  HistoryEventKind,
  HistoryFigure,
  PhaseKind,
} from "@/lib/types";
import { Card } from "./ui/card";
import { Chip, Figure, Key, LegendItem, Meta, ScreenTitle, SectionTitle } from "./ui/figure";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import { CenteredSpinner } from "./spinner";

/** A Phase's band colour. A cut and a bulk are directions, not verdicts, so they
 *  take two identity colours from the ramp rather than a red and a green — Verve
 *  does not know which one you wanted (README: it does not color a change good or
 *  bad). Maintenance gets the recessed tone: it is the absence of a direction. */
const PHASE_COLOR: Record<PhaseKind, string> = {
  cut: SERIES_COLORS[0],
  bulk: SERIES_COLORS[2],
  maintenance: RECESSED,
};

/** The dot beside each event on the rail, by what kind of thing happened. */
const EVENT_COLOR: Record<HistoryEventKind, string> = {
  import: SERIES_COLORS[0],
  phase: SERIES_COLORS[0],
  note: SERIES_COLORS[2],
  source: SERIES_COLORS[1],
  origin: RECESSED,
};

/** HistoryPage is the long view: everything you hold, and the context that explains
 *  it.
 *
 *  Every other screen answers a question about a window. This one answers the
 *  question the windows cannot: what is actually in here, how far back does it go,
 *  and why does the curve do that in the spring of 2022. Which makes the two things
 *  it shows the gaps and the events — a stretch with no data is a fact about the
 *  history, and the note explaining it is the only thing that will ever recover it. */
export function HistoryPage() {
  const [metric, setMetric] = React.useState<string>("body_mass");
  const query = useHistory(metric);
  const data = query.data;

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3.5">
        <div className="flex min-w-0 items-center gap-2.5">
          <ScreenTitle>History</ScreenTitle>
          {data?.first && (
            <Chip>
              {formatDayRange(data.first, data.last ?? data.first)} · {data.days.toLocaleString("fr-FR")} days
            </Chip>
          )}
        </div>
        <BandMetricPicker value={metric} onChange={setMetric} />
      </header>

      <div className="flex-1 space-y-5 overflow-y-auto p-6">
        {query.isLoading ? (
          <CenteredSpinner />
        ) : query.isError ? (
          <p className="py-6 text-center text-sm text-destructive">Couldn’t read your history.</p>
        ) : !data || (!data.band && data.events.length === 0) ? (
          <EmptyHistory />
        ) : (
          <>
            {data.band && <BandCard band={data.band} />}
            <EventLedger events={data.events} />
          </>
        )}
      </div>
    </div>
  );
}

/** BandMetricPicker chooses which Metric the long curve draws. Body mass by
 *  default — the Metric most Accounts have across their whole history, and the one
 *  a Phase is actually about — but the band is not hard-wired to it. */
function BandMetricPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const catalog = useMetricMap();
  const options = React.useMemo(
    () => [...catalog.map.values()].sort((a, b) => a.slug.localeCompare(b.slug)),
    [catalog.map],
  );

  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-8 w-56 text-xs" aria-label="Metric drawn over the whole history">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((m) => (
          <SelectItem key={m.slug} value={m.slug}>
            {metricLabel(m.slug)} <span className="text-muted-foreground">({m.unit})</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** BandCard draws one Metric over the whole history, with the Phases as translucent
 *  vertical bands behind it and the stretches with no data shaded.
 *
 *  The series arrives dense — one entry per bucket, empty ones marked — which is
 *  what lets a gap be drawn at all. Everywhere else in Verve an empty bucket is
 *  simply absent, because a gap is never a zero; here it is the point. */
function BandCard({ band }: { band: HistoryBand }) {
  const data = React.useMemo(
    () =>
      band.points.map((p) => ({
        bucket: p.bucket,
        // A gap is undefined, not zero: `connectNulls` off then breaks the line
        // there, so the curve stops rather than descending to the floor.
        value: p.gap ? undefined : p.value,
      })),
    [band.points],
  );

  const withData = band.points.filter((p) => !p.gap).length;
  const kinds = new Set(band.phases.map((p) => p.kind));

  return (
    <Card className="flex flex-col p-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 pb-2.5">
        <SectionTitle>{metricLabel(band.metric)}, everything you have</SectionTitle>
        <Meta>
          {withData} of {band.points.length} {band.bucket}s · {band.unit}
        </Meta>
      </div>

      <div className="h-44">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
            <CartesianGrid stroke={GRID} strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey="bucket"
              tickFormatter={(v: string) => v.slice(0, 4)}
              minTickGap={48}
              stroke={AXIS}
              fontSize={10}
              tickLine={false}
              axisLine={false}
            />
            <YAxis
              width={40}
              domain={["dataMin", "dataMax"]}
              stroke={AXIS}
              fontSize={10}
              tickLine={false}
              axisLine={false}
              tickFormatter={compact}
            />

            {/* Phases first, so they sit behind the curve. Each is drawn on the
                bucket keys the server folded it onto — a span computed here from
                dates would land between categories and silently draw nothing. */}
            {band.phases.map((phase) => (
              <ReferenceArea
                key={phase.id}
                x1={phase.from}
                x2={phase.to}
                fill={PHASE_COLOR[phase.kind]}
                fillOpacity={0.12}
                strokeOpacity={0}
                isFront={false}
              />
            ))}
            {band.gaps.map((gap) => (
              <ReferenceArea
                key={`gap-${gap.from}`}
                x1={gap.from}
                x2={gap.to}
                fill={RECESSED}
                fillOpacity={0.35}
                strokeOpacity={0}
                isFront={false}
              />
            ))}

            <Tooltip
              cursor={{ stroke: GRID }}
              content={({ active, payload }) => {
                if (!active || !payload?.length) return null;
                const d = payload[0].payload as { bucket: string; value?: number };
                return (
                  <div className="rounded-md border bg-popover px-2.5 py-1.5 text-2xs shadow-md">
                    <div className="font-mono tabular-nums">{d.bucket}</div>
                    <div className="text-muted-foreground">
                      {d.value === undefined ? (
                        "no data"
                      ) : (
                        <>
                          <span className="font-mono tabular-nums">{formatExact(d.value)}</span> {band.unit}
                        </>
                      )}
                    </div>
                  </div>
                );
              }}
            />
            <Area
              type="monotone"
              dataKey="value"
              stroke="none"
              fill={SERIES_COLORS[0]}
              fillOpacity={0.1}
              connectNulls={false}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="value"
              stroke={SERIES_COLORS[0]}
              strokeWidth={1.5}
              dot={false}
              connectNulls={false}
              isAnimationActive={false}
            />
          </ComposedChart>
        </ResponsiveContainer>
      </div>

      <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1 pt-2.5">
        {[...kinds].map((kind) => (
          <LegendItem key={kind} color={PHASE_COLOR[kind]}>
            {kind}
          </LegendItem>
        ))}
        {band.gaps.length > 0 && (
          <span className="flex items-baseline gap-1.5 text-2xs text-muted-foreground">
            <Key color={RECESSED} className="translate-y-px opacity-50" />
            gap in the data
          </span>
        )}
      </div>
    </Card>
  );
}

/** EventLedger is the dated list under the band: a rail, a dot per entry coloured
 *  by kind, and the entry itself. It reads newest first, because the question that
 *  brings you here is usually about the recent end of the curve. */
function EventLedger({ events }: { events: HistoryEvent[] }) {
  if (events.length === 0) return null;
  return (
    <div className="flex flex-col">
      {events.map((event, i) => (
        <EventRow key={`${event.kind}-${event.date}-${i}`} event={event} />
      ))}
    </div>
  );
}

function EventRow({ event }: { event: HistoryEvent }) {
  const { title, body } = describe(event);
  return (
    <div className="grid items-stretch [grid-template-columns:5.5rem_1.25rem_1fr]">
      <div className="py-3.5 pr-3 text-right font-mono text-2xs tabular-nums text-muted-foreground">
        {formatDay(event.date, { year: false })}
        <div className="text-3xs opacity-60">{event.date.slice(0, 4)}</div>
      </div>
      {/* The rail is one continuous line with a ringed dot on it, so a run of
          entries reads as a single timeline rather than as a stack of cards. */}
      <div className="relative flex justify-center">
        <span className="absolute inset-y-0 w-px bg-border" aria-hidden />
        <span
          className="relative mt-[1.15rem] size-2.5 rounded-full ring-2 ring-background"
          style={{ background: EVENT_COLOR[event.kind] }}
          aria-hidden
        />
      </div>
      <div className="py-3 pl-3.5">
        <div className="flex flex-wrap items-baseline gap-2">
          <SectionTitle className="whitespace-normal">{title}</SectionTitle>
          <Chip className="rounded-[4px] px-1.5 py-px">{event.kind}</Chip>
          {event.ends_on && (
            <Meta>
              through {formatDay(event.ends_on)}
            </Meta>
          )}
        </div>
        <p className="max-w-[41rem] pt-1 text-xs leading-relaxed text-muted-foreground">{body}</p>
        {event.figures && event.figures.length > 0 && (
          <div className="flex flex-wrap gap-2 pt-2">
            {event.figures.map((f) => (
              <FigureChip key={f.key} figure={f} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function FigureChip({ figure }: { figure: HistoryFigure }) {
  return (
    <span className="flex items-baseline gap-1.5 rounded border bg-muted/40 px-2 py-1 text-2xs">
      <span className="text-muted-foreground">{FIGURE_LABEL[figure.key] ?? figure.key}</span>
      <Figure size="inline">{formatExact(figure.value)}</Figure>
      {figure.unit && <span className="text-muted-foreground">{figure.unit}</span>}
    </span>
  );
}

const FIGURE_LABEL: Record<string, string> = {
  added: "added",
  skipped: "already had",
  unmapped: "unmapped",
  rows: "records",
  unmapped_kept: "kept unmapped",
};

/** describe turns a typed event into the sentence the page shows.
 *
 *  The words live here rather than in the API because they are interface copy, and
 *  because most of them are Verve's promises about your data rather than facts about
 *  a row: that a re-import adds only what is new, that a gap is never filled in with
 *  a zero, that nothing incoming is thrown away. The place to state a promise is
 *  next to the evidence for it. */
function describe(event: HistoryEvent): { title: string; body: string } {
  switch (event.kind) {
    case "import":
      return {
        title: `Imported ${event.label ?? "an export"}`,
        body:
          "Only what was new was added. Re-dropping the same export changes nothing, so a monthly " +
          "import costs you one file and never a duplicate.",
      };
    case "phase":
      return {
        title: phaseTitle(event),
        body:
          "A phase is a target rate you committed to, not a prediction. Adherence is read against " +
          "it on the Plan page, and the band behind the curve above is the stretch it covers.",
      };
    case "note":
      return {
        title: event.label ?? "A note",
        body:
          event.body ||
          "A note you wrote on the time axis. It shows on every curve that crosses these dates, " +
          "which is what makes a strange fortnight readable a year later.",
      };
    case "source":
      return {
        title: `${event.label ?? "A source"} started recording`,
        body:
          "The day this source first appears in your data. When two sources overlap, one wins per " +
          "day and nothing is counted twice — a step change in a curve is usually a device, not a body.",
      };
    default:
      return {
        title: "The earliest thing you hold",
        body:
          "Your history starts here. Nothing before it was dropped: what the catalog could not map " +
          "is kept in an inspectable bin rather than discarded, and a stretch with no data stays a " +
          "gap rather than becoming a zero.",
      };
  }
}

function phaseTitle(event: HistoryEvent): string {
  const kind = event.label ?? "phase";
  if (event.rate_pct_per_week === undefined) return `${capitalize(kind)} committed`;
  const rate = Math.abs(event.rate_pct_per_week).toFixed(2);
  return `${capitalize(kind)} committed at ${rate} %/week`;
}

function EmptyHistory() {
  return (
    <Card className="flex flex-col items-center gap-2 px-6 py-12 text-center">
      <p className="text-heading font-medium">Nothing to look back on yet</p>
      <p className="max-w-md text-xs leading-relaxed text-muted-foreground">
        This page fills itself in as you use Verve: every import, every phase, every note, and the
        day each of your devices first recorded something.{" "}
        <Link to="/import" className="text-primary hover:underline">
          Import your export
        </Link>{" "}
        to start it.
      </p>
    </Card>
  );
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function compact(v: number): string {
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)}k`;
  return Number.isInteger(v) ? String(v) : v.toFixed(1);
}
