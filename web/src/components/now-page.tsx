import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Map as MapIcon, Moon, Upload } from "lucide-react";
import { useNow, useNowUnusual } from "@/hooks/use-now";
import { activityIcon } from "@/lib/activities";
import { segmentColor } from "@/lib/breakdown";
import {
  figureUnit,
  formatDay,
  formatDuration,
  formatExact,
  formatFigure,
  formatSessionDuration,
} from "@/lib/format";
import { attainmentText, boundText, goalValue } from "@/lib/goals";
import { metricLabel } from "@/lib/metrics";
import { clockTime, nightBands } from "@/lib/night";
import { ageText, lagText, needsExport, splitSources } from "@/lib/now";
import { usualBrief, usualLine } from "@/lib/usual";
import { cn } from "@/lib/utils";
import type { Freshness, NowCard, NowGoal, NowNight, NowWorkout, SourceFreshness } from "@/lib/types";
import { MetricIcon } from "./metric-icon";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader } from "./ui/card";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { Eyebrow, Figure, Meta, ScreenTitle, Track, Unit } from "./ui/figure";

/** NowPage is where the Account stands, rather than what a window looked like (ADR
 *  0045): how old its data is, each Pin at its Latest value, and the readings beside
 *  them (ADR 0049): what sits outside its Usual, the last Night, the last workout and
 *  the Goals in force today.
 *
 *  There is no range and no bucket here, and that is the decision the page holds.
 *  Every reading is one value or one entity, never a window: a card with a sparkline
 *  has one, and a Pin with a window is a one-Panel Dashboard (ADR 0025). The only
 *  axis drawn is the last Night's, which belongs to the entity (ADR 0041). An age is
 *  printed as an age and never coloured: how old is too old depends on the Metric
 *  and on the owner, and Verve knows neither. */
export function NowPage() {
  const now = useNow();

  const header = (freshness?: Freshness) => (
    <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3.5">
      <ScreenTitle>Now</ScreenTitle>
      {freshness && <FreshnessLine freshness={freshness} />}
    </header>
  );

  if (now.isLoading) {
    return (
      <Screen header={header()}>
        <NowSkeleton />
      </Screen>
    );
  }
  if (now.isError || !now.data) {
    return (
      <Screen header={header()}>
        <Note>Now could not be read.</Note>
      </Screen>
    );
  }

  const { freshness, cards } = now.data;
  if (!freshness.last_day && cards.length === 0) {
    return (
      <Screen header={header()}>
        <Empty />
      </Screen>
    );
  }

  const { last_night: night, last_workout: workout, goals } = now.data;

  return (
    <Screen header={header(freshness)}>
      {needsExport(freshness.last_day, freshness.age_days) && <ExportNudge ageDays={freshness.age_days} />}

      {cards.length === 0 ? (
        <Note>
          Pin a Metric from its page to see it here at its latest value. The pin is in the header of
          every Metric under <Link to="/data" className="underline underline-offset-2">Data</Link>.
        </Note>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {cards.map((card) => (
            <PinCard key={card.metric} card={card} />
          ))}
        </div>
      )}

      {(night || workout) && (
        <div className="grid gap-3 md:grid-cols-2">
          {night && <LastNight night={night} />}
          {workout && <LastWorkout workout={workout} />}
        </div>
      )}

      {goals.length > 0 && <Goals goals={goals} />}

      {/* Last, because it loads last: a section that arrives after the rest pushes
          nothing down from the bottom of the page. */}
      <Unusual />
    </Screen>
  );
}

/** Bone is one block of a skeleton: the shape of what is coming, in the muted
 *  tone, so the page holds its layout while it loads instead of jumping when it
 *  arrives. */
function Bone({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-muted/60", className)} aria-hidden />;
}

/** NowSkeleton is the page's shape before its first read: two Pin cards and the
 *  Night and workout pair, which is what most Accounts see once it loads. */
function NowSkeleton() {
  const card = (
    <Card className="flex flex-col gap-4 p-4">
      <Bone className="h-4 w-1/3" />
      <Bone className="h-7 w-1/2" />
      <Bone className="h-3 w-2/3" />
    </Card>
  );
  return (
    <div className="flex flex-col gap-4" aria-busy aria-label="Loading">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {card}
        {card}
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        {card}
        {card}
      </div>
    </div>
  );
}

/** Section is a titled group below the Pins: a quiet label, and the readings under
 *  it. The Pins carry no title because they are what the page is. */
function Section({ title, meta, children }: { title: string; meta?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-2 pt-2">
      <div className="flex items-baseline justify-between gap-3 px-1">
        <Eyebrow>{title}</Eyebrow>
        {meta}
      </div>
      {children}
    </section>
  );
}

/** ExportNudge asks for a new export once the data is two weeks old. It says what
 *  to do and nothing about whether the age is bad: how old is too old is the
 *  owner's to judge (ADR 0045), and this is a reminder, not a warning. */
function ExportNudge({ ageDays }: { ageDays: number }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
      <p className="flex items-center gap-2.5 text-sm">
        <Upload className="size-4 shrink-0 text-muted-foreground" />
        <span>
          Your data stops {ageText(ageDays)}.{" "}
          <span className="text-muted-foreground">Export again from your phone to bring it up to date.</span>
        </span>
      </p>
      <Button asChild size="sm" variant="outline" className="h-7 text-xs">
        <Link to="/import">Import</Link>
      </Button>
    </div>
  );
}

/** Unusual is every followed Metric, on a Panel or under a Goal and not pinned,
 *  whose Latest value sits outside its Usual, the furthest out first (ADR 0049). It
 *  loads after the rest of the page and appears when it has something to say. A position and never a verdict (ADR 0046): the row
 *  says above or below, and nothing about whether that is good. */
function Unusual() {
  const unusual = useNowUnusual();
  if (unusual.isLoading) {
    return (
      <Section title="Outside your usual">
        <Card className="flex flex-col gap-3 px-4 py-3" aria-busy>
          {[0, 1, 2].map((i) => (
            <Bone key={i} className="h-4 w-full" />
          ))}
        </Card>
      </Section>
    );
  }
  const cards = unusual.data ?? [];
  if (cards.length === 0) return null;
  return (
    <Section title="Outside your usual">
      <Card className="flex flex-col py-1">
        {cards.map((card) => {
          const latest = card.latest;
          if (!latest?.usual) return null;
          const unit = figureUnit(card.unit, card.aggregation);
          const show = (v: number) => goalValue(v, card);
          const usualUnit = card.aggregation === "duration_by_state" || card.unit === "count" ? "" : card.unit;
          const above = latest.value > latest.usual.high;
          const Arrow = above ? ArrowUp : ArrowDown;
          return (
            <Link
              key={card.metric}
              to="/data/$metric"
              params={{ metric: card.metric }}
              title={usualLine(latest.value, latest.usual, "day", show, usualUnit)}
              className="flex items-center gap-3 px-4 py-2 transition-colors hover:bg-accent/40"
            >
              <MetricIcon slug={card.metric} />
              <span className="min-w-0 flex-1 truncate text-sm">{metricLabel(card.metric)}</span>
              <span className="hidden text-xs text-muted-foreground sm:inline">
                usual {show(latest.usual.low)}–{show(latest.usual.high)}
              </span>
              <span className="flex shrink-0 items-baseline justify-end gap-1 sm:w-28">
                <Arrow className="size-3 self-center text-muted-foreground" aria-label={above ? "above" : "below"} />
                <Figure size="inline" className="text-sm">
                  {formatFigure(latest.value, card.aggregation, card.unit)}
                </Figure>
                {unit && <Unit>{unit}</Unit>}
              </span>
              <span className="hidden w-24 shrink-0 text-right text-xs text-muted-foreground sm:block">
                {ageText(latest.age_days)}
              </span>
            </Link>
          );
        })}
      </Card>
    </Section>
  );
}

/** LastNight is the Account's last Night: its figures and its stages as a strip.
 *  It is the Night entity itself, so it may draw its own axis (ADR 0041); the strip
 *  opens the Night's page, where the clock is marked. */
function LastNight({ night }: { night: NowNight }) {
  const bands = nightBands(night.intervals);
  return (
    <Card className="relative flex flex-col transition-colors hover:bg-accent/30">
      <CardHeader className="flex flex-row items-center gap-2 space-y-0 pb-3">
        <Moon className="size-4 shrink-0 text-muted-foreground" />
        <Link
          to="/nights/$date"
          params={{ date: night.night }}
          className="truncate text-sm font-medium after:absolute after:inset-0"
        >
          Latest night
        </Link>
        <span className="ml-auto shrink-0 text-xs text-muted-foreground" title={formatDay(night.night)}>
          {ageText(night.age_days)}
        </span>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-3">
        <p className="flex items-baseline gap-3">
          <Figure size="wide">{formatDuration(night.asleep)}</Figure>
          {night.onset && night.wake && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground">
              {clockTime(night.onset)} → {clockTime(night.wake)}
            </span>
          )}
        </p>
        {bands.length > 0 && (
          <div className="relative h-3 w-full overflow-hidden rounded-sm bg-muted/30">
            {bands.map((band, i) => (
              <div
                key={`${band.state}-${band.startAt}-${i}`}
                className="absolute inset-y-0"
                style={{
                  left: `${band.leftPct}%`,
                  width: `${band.widthPct}%`,
                  background: segmentColor("sleep", band.state, i),
                }}
              />
            ))}
          </div>
        )}
        <p className="mt-auto flex flex-wrap gap-x-3">
          {night.awake !== undefined && <Meta>awake {formatDuration(night.awake)}</Meta>}
          {night.efficiency !== undefined && <Meta>efficiency {night.efficiency.toFixed(0)} %</Meta>}
          <Meta>{night.source}</Meta>
        </p>
      </CardContent>
    </Card>
  );
}

/** LastWorkout is the Session that began last, as the workout list shows it. */
function LastWorkout({ workout }: { workout: NowWorkout }) {
  const Icon = activityIcon(workout.activity);
  return (
    <Card className="relative flex flex-col transition-colors hover:bg-accent/30">
      <CardHeader className="flex flex-row items-center gap-2 space-y-0 pb-3">
        <Icon className="size-4 shrink-0 text-muted-foreground" />
        <Link
          to="/workouts/$sessionId"
          params={{ sessionId: String(workout.id) }}
          className="truncate text-sm font-medium after:absolute after:inset-0"
        >
          {workout.activity.label}
        </Link>
        {workout.has_route && <MapIcon className="size-3.5 shrink-0 text-muted-foreground" aria-label="Has a route" />}
        <span className="ml-auto shrink-0 text-xs text-muted-foreground" title={formatDay(workout.start_at.slice(0, 10))}>
          {ageText(workout.age_days)}
        </span>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-3">
        <Figure size="wide">{formatSessionDuration(workout.duration)}</Figure>
        <p className="mt-auto flex flex-wrap items-baseline gap-x-5">
          {workout.distance !== undefined && (
            <span className="flex items-baseline gap-1">
              <Figure size="stat">{formatExact(workout.distance)}</Figure>
              <Unit>km</Unit>
            </span>
          )}
          {workout.energy !== undefined && (
            <span className="flex items-baseline gap-1">
              <Figure size="stat">{formatExact(workout.energy)}</Figure>
              <Unit>kcal</Unit>
            </span>
          )}
          <Meta className="ml-auto self-center">{clockTime(workout.start_at)}</Meta>
        </p>
      </CardContent>
    </Card>
  );
}

/** Goals is every Goal in force today with the seven complete days before today
 *  counted against it: a count with its denominator, never a grade (ADR 0044). */
function Goals({ goals }: { goals: NowGoal[] }) {
  return (
    <Section title="Goals" meta={<Meta>last 7 days</Meta>}>
      <Card className="flex flex-col py-1">
        {goals.map((g) => {
          const counts = g.attainment ? attainmentText(g.attainment, g) : undefined;
          const measured = g.attainment?.measured ?? 0;
          return (
            <Link
              key={g.metric}
              to="/data/$metric"
              params={{ metric: g.metric }}
              className="flex items-center gap-3 px-4 py-2 transition-colors hover:bg-accent/40"
            >
              <MetricIcon slug={g.metric} />
              <span className="min-w-0 flex-1 truncate text-sm">{metricLabel(g.metric)}</span>
              <Meta className="shrink-0">{boundText(g.goal, g)}</Meta>
              {g.attainment && measured > 0 && (
                <span className="hidden w-24 shrink-0 sm:block" aria-hidden>
                  <Track fill={g.attainment.met / measured} color="hsl(var(--muted-foreground) / 0.6)" />
                </span>
              )}
              <span className="shrink-0 text-right text-xs text-muted-foreground sm:w-40" title={counts?.title}>
                {counts?.short ?? "nothing to count yet"}
              </span>
            </Link>
          );
        })}
      </Card>
    </Section>
  );
}

function Screen({ header, children }: { header: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      {header}
      <div className="flex-1 overflow-y-auto px-4 py-6 sm:px-6">
        <div className="mx-auto flex w-full max-w-5xl flex-col gap-4">{children}</div>
      </div>
    </div>
  );
}

function Note({ children }: { children: React.ReactNode }) {
  return <p className="px-6 py-6 text-center text-sm text-muted-foreground">{children}</p>;
}

/** Empty is an Account with nothing in it: no age to print and no Pin to read, so
 *  the only next step it has is the one this offers. */
function Empty() {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
      <p className="text-sm text-muted-foreground">Nothing has been imported yet.</p>
      <Button asChild size="sm">
        <Link to="/import">Import an export</Link>
      </Button>
    </div>
  );
}

/** FreshnessLine is how old the data is, said once in the header: the Account's last
 *  datum, the last day anything was recorded whatever was imported since. The Pins
 *  are what the page is for, so the Sources behind that day wait in a popover: each
 *  active one with how far behind it stops, the retired ones folded away because a
 *  phone replaced years ago is not late. The link goes to History, where the gaps
 *  themselves are drawn (ADR 0032). */
function FreshnessLine({ freshness }: { freshness: Freshness }) {
  const [showRetired, setShowRetired] = React.useState(false);
  if (!freshness.last_day) return null;
  const { active, retired } = splitSources(freshness.sources);

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          Last data <span className="font-medium text-foreground">{ageText(freshness.age_days)}</span>
          <span className="font-mono text-3xs tabular-nums">{formatDay(freshness.last_day)}</span>
          <ChevronDown className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-3">
        <p className="px-1 pb-2 text-xs text-muted-foreground">
          The last day anything was recorded, whatever was imported since.
        </p>
        {active.length > 0 && (
          <ul className="flex flex-col">
            {active.map((s) => (
              <SourceRow key={s.source} source={s} />
            ))}
          </ul>
        )}
        {retired.length > 0 && (
          <div className="pt-1">
            <button
              type="button"
              onClick={() => setShowRetired((v) => !v)}
              aria-expanded={showRetired}
              className="flex items-center gap-1 px-1 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
            >
              {showRetired ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
              Retired sources ({retired.length})
            </button>
            {showRetired && (
              <ul className="flex flex-col">
                {retired.map((s) => (
                  <SourceRow key={s.source} source={s} />
                ))}
              </ul>
            )}
          </div>
        )}
        <div className="mt-2 flex justify-between gap-2 border-t pt-2">
          <Link
            to="/days/$date"
            params={{ date: freshness.last_day }}
            className="px-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          >
            Open {formatDay(freshness.last_day)}
          </Link>
          <Link
            to="/history"
            className="px-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          >
            See the gaps
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function SourceRow({ source }: { source: SourceFreshness }) {
  const lag = lagText(source.lag_days);
  return (
    <li className="flex items-baseline gap-3 px-1 py-1 text-xs">
      <span className="min-w-0 flex-1 truncate">{source.source}</span>
      <Meta className="shrink-0" title={formatDay(source.last_day)}>
        {lag ?? "last to record"}
      </Meta>
    </li>
  );
}

/** PinCard is one Pin at its Latest value: the figure, the day it fell on and how
 *  long ago that was. The bound in force today sits beside it as a fact, and the
 *  counts are the week before today, since today is never judged (ADR 0044). The
 *  card opens the Metric page; its date opens that Day. */
function PinCard({ card }: { card: NowCard }) {
  const unit = figureUnit(card.unit, card.aggregation);
  const latest = card.latest;
  const counts = card.attainment ? attainmentText(card.attainment, card) : undefined;
  const show = (v: number) => goalValue(v, card);
  const usualUnit = card.aggregation === "duration_by_state" || card.unit === "count" ? "" : card.unit;

  return (
    <Card className="relative flex flex-col transition-colors hover:bg-accent/30">
      <CardHeader className="flex flex-row items-center gap-2 space-y-0 pb-3">
        <MetricIcon slug={card.metric} />
        <Link
          to="/data/$metric"
          params={{ metric: card.metric }}
          className="truncate text-sm font-medium after:absolute after:inset-0"
        >
          {metricLabel(card.metric)}
        </Link>
        {latest && (
          // The date is its own link, lifted above the card's, because the Day is
          // where "what else happened then" is answered. Its age is the label, the
          // day itself the title: how long ago is what the eye wants first.
          <Link
            to="/days/$date"
            params={{ date: latest.date }}
            title={formatDay(latest.date)}
            className="relative z-10 ml-auto shrink-0 text-xs text-muted-foreground underline-offset-2 hover:underline"
          >
            {ageText(latest.age_days)}
          </Link>
        )}
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-2">
        {latest ? (
          <>
            <p className="flex items-baseline gap-1.5">
              <Figure size="wide">{formatFigure(latest.value, card.aggregation, card.unit)}</Figure>
              {unit && <Unit>{unit}</Unit>}
            </p>
            {/* The figure read against the owner's own past, as a position and never a
                verdict (ADR 0046): the sentence the Usual exists for. */}
            {latest.usual && (
              <p
                className="text-xs text-muted-foreground"
                title={usualLine(latest.value, latest.usual, "day", show, usualUnit)}
              >
                {usualBrief(latest.value, latest.usual, show, usualUnit)}
              </p>
            )}
          </>
        ) : (
          <p className="py-2 text-sm text-muted-foreground">Never measured.</p>
        )}
        {(card.goal || counts) && (
          <p className="mt-auto flex flex-wrap items-baseline gap-x-3 pt-1">
            {card.goal && <Meta title="The goal in force today">goal {boundText(card.goal, card)}</Meta>}
            {counts && (
              <Meta title={counts.title}>
                {counts.short} · last 7 days
              </Meta>
            )}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
