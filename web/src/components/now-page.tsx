import * as React from "react";
import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Pin } from "lucide-react";
import { useNow } from "@/hooks/use-now";
import { figureUnit, formatDay, formatFigure } from "@/lib/format";
import { attainmentText, boundText } from "@/lib/goals";
import { metricLabel } from "@/lib/metrics";
import { ageText, lagText, splitSources } from "@/lib/now";
import type { Freshness, NowCard, SourceFreshness } from "@/lib/types";
import { MetricIcon } from "./metric-icon";
import { CenteredSpinner } from "./spinner";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Figure, Meta, ScreenTitle, Unit } from "./ui/figure";

/** NowPage is where the Account stands, rather than what a window looked like (ADR
 *  0045): how old its data is, and each Pin at its Latest value.
 *
 *  There is no range, no bucket and no chart here, and that is the decision the page
 *  holds. A card with a sparkline has a window, and a Pin with a window is a
 *  one-Panel Dashboard (ADR 0025). An age is printed as an age and never coloured:
 *  how old is too old depends on the Metric and on the owner, and Verve knows
 *  neither. */
export function NowPage() {
  const now = useNow();

  const header = (
    <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3.5">
      <ScreenTitle>Now</ScreenTitle>
    </header>
  );

  if (now.isLoading) {
    return (
      <Screen header={header}>
        <CenteredSpinner />
      </Screen>
    );
  }
  if (now.isError || !now.data) {
    return (
      <Screen header={header}>
        <Note>Now could not be read.</Note>
      </Screen>
    );
  }

  const { freshness, cards } = now.data;
  if (!freshness.last_day && cards.length === 0) {
    return (
      <Screen header={header}>
        <Empty />
      </Screen>
    );
  }

  return (
    <Screen header={header}>
      <FreshnessCard freshness={freshness} />
      {cards.length === 0 ? (
        <Note>
          Pin a Metric from its page to see it here at its latest value. The pin is in the header of
          every Metric under <Link to="/data" className="underline underline-offset-2">Data</Link>.
        </Note>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {cards.map((card) => (
            <PinCard key={card.metric} card={card} />
          ))}
        </div>
      )}
    </Screen>
  );
}

function Screen({ header, children }: { header: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      {header}
      <div className="flex-1 overflow-y-auto px-4 py-6 sm:px-6">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">{children}</div>
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

/** FreshnessCard is how old the data is: the Account's last datum, then each Source
 *  that is still active with how far behind it stops. Retired Sources are folded
 *  away rather than raised, because a phone replaced years ago is not late. The link
 *  goes to History, where the gaps themselves are drawn (ADR 0032). */
function FreshnessCard({ freshness }: { freshness: Freshness }) {
  const [showRetired, setShowRetired] = React.useState(false);
  if (!freshness.last_day) return null;
  const { active, retired } = splitSources(freshness.sources);

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-baseline justify-between gap-3">
        <CardTitle>Last data</CardTitle>
        <Button asChild variant="ghost" size="sm" className="h-7 px-2 text-xs">
          <Link to="/history">See the gaps</Link>
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <Figure size="stat">{ageText(freshness.age_days)}</Figure>
          <Link
            to="/days/$date"
            params={{ date: freshness.last_day }}
            className="text-sm text-muted-foreground underline-offset-2 hover:underline"
          >
            {formatDay(freshness.last_day)}
          </Link>
        </p>
        <p className="text-xs text-muted-foreground">
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
          <div>
            <button
              type="button"
              onClick={() => setShowRetired((v) => !v)}
              aria-expanded={showRetired}
              className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
            >
              {showRetired ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
              Retired sources ({retired.length})
            </button>
            {showRetired && (
              <ul className="mt-1 flex flex-col">
                {retired.map((s) => (
                  <SourceRow key={s.source} source={s} />
                ))}
              </ul>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function SourceRow({ source }: { source: SourceFreshness }) {
  const lag = lagText(source.lag_days);
  return (
    <li className="flex items-baseline gap-3 px-0.5 py-1 text-sm">
      <span className="min-w-0 flex-1 truncate">{source.source}</span>
      <Meta className="shrink-0">{formatDay(source.last_day)}</Meta>
      <Meta className="w-28 shrink-0 text-right">{lag ?? "last to record"}</Meta>
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

  return (
    <Card className="relative transition-colors hover:bg-accent/30">
      <CardHeader className="flex flex-row items-center gap-2 pb-2">
        <MetricIcon slug={card.metric} />
        <Link
          to="/data/$metric"
          params={{ metric: card.metric }}
          className="truncate text-sm font-medium after:absolute after:inset-0"
        >
          {metricLabel(card.metric)}
        </Link>
        <Pin className="ml-auto size-3 shrink-0 text-muted-foreground/60" aria-label="Pinned" />
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        {latest ? (
          <>
            <p className="flex items-baseline gap-1.5">
              <Figure size="panel">{formatFigure(latest.value, card.aggregation, card.unit)}</Figure>
              {unit && <Unit>{unit}</Unit>}
            </p>
            <p className="flex flex-wrap items-baseline gap-x-2 text-xs text-muted-foreground">
              {/* The date is its own link, lifted above the card's, because the Day is
                  where "what else happened then" is answered. */}
              <Link
                to="/days/$date"
                params={{ date: latest.date }}
                className="relative z-10 underline-offset-2 hover:underline"
              >
                {formatDay(latest.date)}
              </Link>
              <span>{ageText(latest.age_days)}</span>
            </p>
          </>
        ) : (
          <p className="py-2 text-sm text-muted-foreground">Never measured.</p>
        )}
        {(card.goal || counts) && (
          <p className="flex flex-wrap items-baseline gap-x-3 pt-1">
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
