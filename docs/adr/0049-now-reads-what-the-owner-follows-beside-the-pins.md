# Now reads what the owner follows, beside the Pins

_Amends ADR 0045, which stays in force for everything else: Now is still one
reading with no window, Freshness is still measured against the Account, and an
age is still never coloured._

## Context

ADR 0045 gave Now the Account's Freshness and one card per Pin. In use the page is
mostly empty: an owner pins three or four Metrics, and everything else they care
about (last night, the run on Sunday, the Goals they set) is one or two screens
away. Pinning a Panel was considered and refused for the reason ADR 0025 and ADR
0045 already give: a Panel has a window, and a window on Now makes it a Dashboard.

The question is what else answers "where do I stand" without a window.

## Decision

**Now carries five more readings, each one value or one entity, none a range.**

- **Outside your usual.** The Metrics the owner follows whose Latest value sits
  outside their Usual (ADR 0046), the furthest out first, at most six. *Followed*
  means on a Panel of one of the Account's Dashboards or under a Goal, minus the
  Pins, whose cards already say where they sit. The owner's own choice is the
  filter: a mineral intake a nutrition app reports and nobody charts is not
  raised. A value more than 7 days behind the Account's last datum is not raised
  either, measured against the Account as a Source's lag is: a Metric that
  stopped is History's question. Distance is counted in widths of the band, so a
  step count and a heart rate rank on one scale. The row says above or below and
  nothing else: a position, never a verdict.
- **The latest Night.** The sleep Metric's Latest Night, read as the entity
  (ADR 0041), with its age. Because it is the entity and not a date referring to
  it, Now may draw its stages as a strip; the clock and the legend stay on the
  Night's own page, which the card opens.
- **The latest workout.** The Session that began last, as the workout list shows
  it, aged from the day it began with none of the Night's shift (ADR 0040).
- **Goals.** Every Goal in force today, pinned or not, with the seven complete
  days before today counted against it, exactly as a Pin card counts (ADR 0044).
  A count with its denominator; the proportion bar beside it is drawn in a muted
  tone, never a status colour.
- **An export reminder.** When the Account's last datum is 14 days old or more,
  one line says so and links to Import. It is a reminder and not a warning: the
  threshold says when to ask, not when the data is bad, and it is not coloured.

**The unusual Metrics are their own call, `GET /v1/now/unusual`.** They cost a
Usual per followed Metric, the slow half of the page, so the Pins, the Night, the
workout and the Goals render without waiting and the section appears when it has
something to say. Its query key sits under Now's, so every write that stales Now
stales it.

## Considered Options

- **The readings above (chosen).**
- **Pin a Panel.** A Panel has a window; a window on Now is a Dashboard (ADR
  0025). Rejected; a Dashboard is one click away.
- **Unusual over every Metric with data.** Complete, and on a real Account it is
  a list of micronutrients from a food diary and a four-second read. Rejected in
  favour of the Metrics the owner follows.
- **A hard-coded list of Metrics worth raising.** Verve does not know which
  Metrics matter to this owner; their Panels and Goals do. Rejected.
- **Unusual for the pinned Metrics too.** Their cards already print the Usual.
  Rejected as a duplicate.
- **The Night's full hypnogram on Now.** The axis, its hours and its legend are
  the Night page's. Now draws the strip and links there.

## Consequences

- `GET /v1/now` gains `last_night`, `last_workout` and `goals`; `goals` is always
  present, the other two absent for an Account that never recorded one.
- `GET /v1/now/unusual` is new, answering `{"unusual": [...]}`, never null.
- A Metric appears on Now without being pinned. This is a Pin's job in ADR 0045;
  the Unusual list is not a Pin, carries no Goal and no count, and exists only
  while its value stays outside the band.
- The Unusual read is bounded by the number of followed Metrics, not by the
  Catalog. A 28-day day-grain Series is the engine's cost per Metric.
