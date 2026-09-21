import * as React from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useHotkeys } from "react-hotkeys-hook";
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Map as MapIcon,
  Pin,
  Plus,
  StickyNote,
  Trash2,
} from "lucide-react";
import { useDay } from "@/hooks/use-day";
import { useDeleteManualMeasurement } from "@/hooks/use-measurements";
import { isEmptyDay, isValidDay, rowState, shiftDay, today } from "@/lib/day";
import { activityIcon } from "@/lib/activities";
import {
  figureUnit,
  formatDay,
  formatDuration,
  formatExact,
  formatFigure,
  formatSessionDuration,
} from "@/lib/format";
import { boundText } from "@/lib/goals";
import { metricLabel } from "@/lib/metrics";
import { clockTime, efficiencyBasisLabel } from "@/lib/night";
import { cn } from "@/lib/utils";
import type { Annotation, DayMetric, ManualMeasurement, NightSummary, Session } from "@/lib/types";
import { AnnotationDialog } from "./annotation-dialog";
import { ManualEntryDialog } from "./manual-entry-dialog";
import { MetricIcon } from "./metric-icon";
import { CenteredSpinner } from "./spinner";
import { Button } from "./ui/button";
import { Calendar } from "./ui/calendar";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Chip, Figure, Meta, Unit } from "./ui/figure";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";

/** DayPage is one calendar date read as an index: everything the Account holds on
 *  it, and the per-day Source election the read engine already performs (ADR 0043).
 *
 *  There is no chart here, and that is the decision the page exists to hold. A Day
 *  is a Bucket and not an entity, so the only chart a single date could carry is the
 *  intra-day axis ADR 0041 confines to entities. The shape of the night is on the
 *  Night, the curve of the ride is on the ride, and this page links to both. */
export function DayPage() {
  const { date } = useParams({ from: "/days/$date" });
  const navigate = useNavigate();
  const day = useDay(date);
  const [noteOpen, setNoteOpen] = React.useState(false);
  const [entryOpen, setEntryOpen] = React.useState(false);

  // The shift is expressed against the params the router holds rather than the date
  // this render closed over, so a held arrow key advances a day per press: two
  // presses inside one render would otherwise both shift from the same date and one
  // of them would be lost.
  const go = React.useCallback(
    (delta: number) =>
      navigate({
        to: "/days/$date",
        params: (prev: { date?: string }) => ({ date: shiftDay(prev.date ?? date, delta) }),
      }),
    [date, navigate],
  );
  // Navigation is unbounded in both directions: a date before the Account's first
  // row is a valid, empty Day, and the page says so rather than refusing to go.
  useHotkeys("left", () => go(-1), { preventDefault: true }, [go]);
  useHotkeys("right", () => go(1), { preventDefault: true }, [go]);

  const header = <Header date={date} onShift={go} onNote={() => setNoteOpen(true)} onEntry={() => setEntryOpen(true)} />;

  if (!isValidDay(date)) {
    return (
      <Screen header={header}>
        <Note>That is not a date. Try one shaped like 2026-03-03.</Note>
      </Screen>
    );
  }
  if (day.isLoading) {
    return (
      <Screen header={header}>
        <CenteredSpinner />
      </Screen>
    );
  }
  if (day.isError || !day.data) {
    return (
      <Screen header={header}>
        <Note>That day could not be read.</Note>
      </Screen>
    );
  }

  const data = day.data;
  return (
    <>
      <Screen header={header}>
        {data.phase && (
          <Meta className="px-0.5">
            in a {data.phase.rate_pct_per_week < 0 ? "cut" : data.phase.rate_pct_per_week > 0 ? "bulk" : "maintenance"} since{" "}
            {formatDay(data.phase.started_at.slice(0, 10))}
          </Meta>
        )}

        {isEmptyDay(data) ? (
          <Note>Nothing was recorded on this day.</Note>
        ) : (
          <>
            {data.metrics.length > 0 && <Figures rows={data.metrics} />}
            {data.night && <Night night={data.night} date={data.date} />}
            {data.sessions.length > 0 && <Workouts sessions={data.sessions} />}
            {data.annotations.length > 0 && <Notes notes={data.annotations} />}
            {data.manual_entries.length > 0 && <Entries rows={data.manual_entries} />}
          </>
        )}
      </Screen>

      <AnnotationDialog open={noteOpen} onOpenChange={setNoteOpen} defaultDay={date} />
      <ManualEntryDialog open={entryOpen} onOpenChange={setEntryOpen} date={date} />
    </>
  );
}

function Screen({ header, children }: { header: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      {header}
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">{children}</div>
      </div>
    </div>
  );
}

function Note({ children }: { children: React.ReactNode }) {
  return <p className="px-6 py-10 text-center text-sm text-muted-foreground">{children}</p>;
}

/** Header carries the date, the way off it in both directions, and the two things a
 *  Day is written on. The write affordances live here rather than in their own
 *  sections so that a day with nothing on it is still a day you can write on. */
function Header({
  date,
  onShift,
  onNote,
  onEntry,
}: {
  date: string;
  onShift: (delta: number) => void;
  onNote: () => void;
  onEntry: () => void;
}) {
  const isToday = date === today();
  return (
    <header className="flex flex-wrap items-center gap-2 border-b px-6 py-3.5">
      <Button variant="ghost" size="icon" className="size-7" aria-label="Previous day" onClick={() => onShift(-1)}>
        <ChevronLeft className="size-4" />
      </Button>
      <Button variant="ghost" size="icon" className="size-7" aria-label="Next day" onClick={() => onShift(1)}>
        <ChevronRight className="size-4" />
      </Button>
      <DatePicker date={date} />
      <span className="text-sm font-medium">{isValidDay(date) ? formatDay(date) : date}</span>
      {isToday && <Chip>today</Chip>}

      <span className="ml-auto flex items-center gap-1.5">
        {!isToday && (
          <Button asChild variant="ghost" size="sm" className="h-7 px-2 text-xs">
            <Link to="/days/$date" params={{ date: today() }}>
              Today
            </Link>
          </Button>
        )}
        <Button variant="ghost" size="sm" className="h-7 gap-1.5 px-2 text-xs" onClick={onNote}>
          <StickyNote className="size-3.5" /> Note
        </Button>
        <Button variant="ghost" size="sm" className="h-7 gap-1.5 px-2 text-xs" onClick={onEntry}>
          <Plus className="size-3.5" /> Entry
        </Button>
      </span>
    </header>
  );
}

/** DatePicker jumps to any date. It is a single-date picker and not the Time-range
 *  one: a Day has no window to pick, which is the whole shape of the concept. */
function DatePicker({ date }: { date: string }) {
  const navigate = useNavigate();
  const [open, setOpen] = React.useState(false);
  // Parsed as UTC and rendered back as UTC, so the calendar never highlights the
  // neighbouring day for a reader west of Greenwich.
  const [y, m, d] = date.split("-").map(Number);
  const selected = isValidDay(date) ? new Date(Date.UTC(y, m - 1, d, 12)) : undefined;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="size-7" aria-label="Pick a date">
          <CalendarDays className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="single"
          selected={selected}
          defaultMonth={selected}
          onSelect={(picked) => {
            if (!picked) return;
            setOpen(false);
            const pad = (n: number) => String(n).padStart(2, "0");
            const next = `${picked.getFullYear()}-${pad(picked.getMonth() + 1)}-${pad(picked.getDate())}`;
            void navigate({ to: "/days/$date", params: { date: next } });
          }}
        />
      </PopoverContent>
    </Popover>
  );
}

/** Figures is the day's numbers as a list. Pinned Metrics come first, present or
 *  absent, because a pinned row with no value is the most informative row here: it
 *  says the day is missing, not that the Metric is. The server orders them; this
 *  draws what it was handed (ADR 0025, ADR 0043). */
function Figures({ rows }: { rows: DayMetric[] }) {
  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-baseline justify-between gap-3">
        <CardTitle>Figures</CardTitle>
        <Meta>{rows.length} metrics</Meta>
      </CardHeader>
      <CardContent className="flex flex-col">
        {rows.map((row) => (
          <MetricRow key={row.metric} row={row} />
        ))}
      </CardContent>
    </Card>
  );
}

/** MetricRow is one figure. An absence says which kind it is: a gap ("—") is
 *  nothing recorded, a refusal says so in words, and the row links to the Metric
 *  page either way, which is where the rule behind a refusal is lifted. */
function MetricRow({ row }: { row: DayMetric }) {
  const state = rowState(row);
  const unit = figureUnit(row.unit, row.aggregation);
  return (
    <Link
      to="/data/$metric"
      params={{ metric: row.metric }}
      className="flex items-center gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-accent/50"
    >
      <MetricIcon slug={row.metric} />
      <span className="truncate text-sm">{metricLabel(row.metric)}</span>
      {row.pinned && <Pin className="size-3 shrink-0 text-muted-foreground/60" aria-label="Pinned" />}
      {state === "excluded" && <Chip>excluded</Chip>}

      <span className="ml-auto flex items-baseline gap-1.5">
        {state === "value" ? (
          <>
            <Figure size="inline" className="text-sm">
              {formatFigure(row.value as number, row.aggregation, row.unit)}
            </Figure>
            {unit && <Unit>{unit}</Unit>}
          </>
        ) : (
          <span className="text-sm text-muted-foreground">—</span>
        )}
        {/* The bound in force that day, beside the value and in the same words the
            Panel's counts use. No "met", no icon: the two numbers side by side say
            it, and the Day never grades a date (ADR 0043, ADR 0044). */}
        {row.goal && (
          <Meta className="whitespace-nowrap" title="The goal in force on this date">
            goal {boundText(row.goal, row)}
          </Meta>
        )}
      </span>
      <Meta className="w-36 shrink-0 text-right">{row.source}</Meta>
    </Link>
  );
}

/** Night carries the figures and links to the shape. The hypnogram is on the Night's
 *  own page because the axis belongs to the entity, not to the date (ADR 0041). */
function Night({ night, date }: { night: NightSummary; date: string }) {
  const basis = efficiencyBasisLabel(night.efficiency_basis);
  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-baseline justify-between gap-3">
        <CardTitle>The night</CardTitle>
        <span className="flex items-center gap-3">
          <Meta>recorded by {night.source}</Meta>
          <Button asChild variant="ghost" size="sm" className="h-7 px-2 text-xs">
            <Link to="/nights/$date" params={{ date }}>
              See the stages
            </Link>
          </Button>
        </span>
      </CardHeader>
      <CardContent className="flex flex-wrap gap-x-10 gap-y-4">
        <Stat label="Asleep" value={formatDuration(night.asleep)} />
        {night.awake !== undefined && <Stat label="Awake in the night" value={formatDuration(night.awake)} />}
        {night.onset && <Stat label="Fell asleep" value={clockTime(night.onset)} />}
        {night.wake && <Stat label="Woke" value={clockTime(night.wake)} />}
        {night.efficiency !== undefined && (
          <Stat label="Efficiency" value={`${night.efficiency.toFixed(0)} %`} note={basis} />
        )}
      </CardContent>
    </Card>
  );
}

function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="min-w-24">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      {note && <p className="text-2xs text-muted-foreground/70">{note}</p>}
    </div>
  );
}

/** Workouts are the Sessions that started on this date, with none of the Night's
 *  shift: a ride past midnight is one its owner names by the evening it began
 *  (ADR 0040). The date column the list carries is dropped, because the page is it. */
function Workouts({ sessions }: { sessions: Session[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Workouts</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col">
        {sessions.map((s) => {
          const Icon = activityIcon(s.activity);
          return (
            <Link
              key={s.id}
              to="/workouts/$sessionId"
              params={{ sessionId: String(s.id) }}
              className="flex items-center gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-accent/50"
            >
              <Icon className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate text-sm">{s.activity.label}</span>
              {s.has_route && <MapIcon className="size-3.5 shrink-0 text-muted-foreground" aria-label="Has a route" />}
              <Meta className="shrink-0">{clockTime(s.start_at)}</Meta>
              <span className="ml-auto flex items-baseline gap-3 text-sm tabular-nums">
                <span>{formatSessionDuration(s.duration)}</span>
                <span className={cn(s.distance === undefined && "text-muted-foreground")}>
                  {s.distance === undefined ? "—" : `${formatExact(s.distance)} km`}
                </span>
                <span className={cn(s.energy === undefined && "text-muted-foreground")}>
                  {s.energy === undefined ? "—" : `${formatExact(s.energy)} kcal`}
                </span>
              </span>
            </Link>
          );
        })}
      </CardContent>
    </Card>
  );
}

/** Notes are the Annotations dated on this day, editable in place through the same
 *  dialog that writes them everywhere else (ADR 0030). */
function Notes({ notes }: { notes: Annotation[] }) {
  const [editing, setEditing] = React.useState<Annotation | null>(null);
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Notes</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col">
          {notes.map((note) => (
            <button
              key={note.id}
              type="button"
              onClick={() => setEditing(note)}
              className="flex items-start gap-3 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent/50"
            >
              <StickyNote className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{note.label}</span>
                {note.body && <span className="block truncate text-xs text-muted-foreground">{note.body}</span>}
              </span>
              {note.ends_on && (
                <Meta className="shrink-0">
                  {formatDay(note.starts_on, { year: false })} → {formatDay(note.ends_on, { year: false })}
                </Meta>
              )}
            </button>
          ))}
        </CardContent>
      </Card>
      <AnnotationDialog
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        annotation={editing}
      />
    </>
  );
}

/** Entries are the Manual rows measured on this date, deletable in place because
 *  they are yours (ADR 0022). Correcting one is delete-then-re-enter, here as
 *  everywhere: there is no edit, deliberately. */
function Entries({ rows }: { rows: ManualMeasurement[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Your entries</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col">
        {rows.map((row) => (
          <EntryRow key={row.id} row={row} />
        ))}
      </CardContent>
    </Card>
  );
}

function EntryRow({ row }: { row: ManualMeasurement }) {
  const remove = useDeleteManualMeasurement();
  // A `%` Metric is stored as a fraction and typed in 0–100; formatExact reads it
  // back the way it was typed (the rule is toDisplayValue's, in lib/metrics).
  return (
    <div className="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-accent/50">
      <MetricIcon slug={row.metric} />
      <span className="truncate text-sm">{metricLabel(row.metric)}</span>
      <Meta className="shrink-0">{clockTime(row.measured_at)}</Meta>
      <span className="ml-auto flex items-baseline gap-1.5">
        <Figure size="inline" className="text-sm">
          {formatExact(row.value, row.unit)}
        </Figure>
        <Unit>{row.unit}</Unit>
      </span>
      <Button
        variant="ghost"
        size="icon"
        className="size-6 shrink-0"
        disabled={remove.isPending}
        aria-label={`Delete ${metricLabel(row.metric)} entry`}
        onClick={() => remove.mutate(row.id)}
      >
        <Trash2 className="size-3.5" />
      </Button>
    </div>
  );
}
