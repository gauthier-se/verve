import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useNight } from "@/hooks/use-night";
import { clockTime, efficiencyBasisLabel, hourTicks, nightBands } from "@/lib/night";
import { formatDuration } from "@/lib/format";
import { segmentColor, segmentLabel } from "@/lib/breakdown";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Meta } from "./ui/figure";
import { CenteredSpinner } from "./spinner";

/** NightPage is one Night read as an entity: the stages against the clock, and
 *  the figures that only exist because there is an axis to compute them on
 *  (ADR 0041).
 *
 *  It is drawn from the very intervals the sleep Metric folded for that night,
 *  resolved by the same rule, so this page and the bar on the Metric page cannot
 *  disagree about the same night (ADR 0027). The colours are the Stage slots the
 *  stacked bar already uses, for the same reason: two screens showing one night
 *  in different colours teach a reader that one of them is decorative. */
export function NightPage() {
  const { date } = useParams({ from: "/nights/$date" });
  const night = useNight(date);

  if (night.isLoading) return <CenteredSpinner />;
  if (night.isError || !night.data) {
    return (
      <div className="flex h-full flex-col">
        <Header date={date} />
        <p className="px-6 py-10 text-center text-sm text-muted-foreground">
          No night was recorded on that morning.
        </p>
      </div>
    );
  }

  const data = night.data;
  const bands = nightBands(data.intervals);
  const ticks = hourTicks(data.intervals);
  const basis = efficiencyBasisLabel(data.efficiency_basis);

  return (
    <div className="flex h-full flex-col">
      <Header date={date} />
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">
          <Card>
            <CardHeader className="flex flex-row flex-wrap items-baseline justify-between gap-3">
              <CardTitle>The night</CardTitle>
              <Meta>recorded by {data.source}</Meta>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-x-10 gap-y-4">
              <Figure label="Asleep" value={formatDuration(data.asleep)} />
              {data.awake !== undefined && (
                <Figure label="Awake in the night" value={formatDuration(data.awake)} />
              )}
              {data.onset && <Figure label="Fell asleep" value={clockTime(data.onset)} />}
              {data.wake && <Figure label="Woke" value={clockTime(data.wake)} />}
              {data.efficiency !== undefined && (
                <Figure
                  label="Efficiency"
                  value={`${data.efficiency.toFixed(0)} %`}
                  note={basis}
                />
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Stages</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {/* The axis is the night's own span, from the first interval to the
                  last. A midnight-to-midnight axis would cut the night in two,
                  which is the thing a Night exists to stop (ADR 0027). */}
              <div className="relative h-10 w-full overflow-hidden rounded-md border bg-muted/30">
                {bands.map((band, i) => (
                  <div
                    key={`${band.state}-${band.startAt}-${i}`}
                    className="absolute inset-y-0"
                    style={{
                      left: `${band.leftPct}%`,
                      width: `${band.widthPct}%`,
                      background: segmentColor("sleep", band.state, i),
                    }}
                    title={`${segmentLabel("sleep", band.state)} · ${clockTime(band.startAt)}–${clockTime(band.endAt)}`}
                  />
                ))}
              </div>
              <div className="relative h-4 w-full">
                {ticks.map((tick) => (
                  <span
                    key={tick.label}
                    className="absolute -translate-x-1/2 font-mono text-2xs text-muted-foreground"
                    style={{ left: `${tick.leftPct}%` }}
                  >
                    {tick.label}
                  </span>
                ))}
              </div>
              <Legend states={[...new Set(bands.map((b) => b.state))]} />
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function Header({ date }: { date: string }) {
  return (
    <header className="flex items-center gap-3 border-b px-6 py-3.5">
      <Button asChild variant="ghost" size="sm" className="h-7 gap-1.5 px-2">
        <Link to="/data/$metric" params={{ metric: "sleep" }}>
          <ArrowLeft className="size-3.5" /> Sleep
        </Link>
      </Button>
      <span className="text-sm font-medium">Night of {date}</span>
    </header>
  );
}

/** Legend names the Stages this night actually had, in the order they were
 *  drawn. A stacked shape is the one chart whose parts cannot be told apart by
 *  eye, so its key is not optional. */
function Legend({ states }: { states: string[] }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      {states.map((state, i) => (
        <span key={state} className="flex items-center gap-1.5 text-2xs text-muted-foreground">
          <span
            className="inline-block size-2 shrink-0 rounded-[2px]"
            style={{ background: segmentColor("sleep", state, i) }}
          />
          {segmentLabel("sleep", state)}
        </span>
      ))}
    </div>
  );
}

/** Figure is one number with its label, and an optional note under it for a
 *  figure whose meaning depends on how it was computed: an efficiency says what
 *  it was divided by, because the classic denominator is not the one available
 *  here (ADR 0041). */
function Figure({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="min-w-24">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      {note && <p className="text-2xs text-muted-foreground/70">{note}</p>}
    </div>
  );
}
