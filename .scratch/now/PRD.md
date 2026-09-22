# PRD: Now, and Freshness

## Goal

Every screen in Verve is a window and a bucket, and `/` forwards to the first
Dashboard. Nothing says where the owner stands now, and nothing says that the
last datum is five weeks old or that steps stopped on the 12th. History draws the
gaps, but it has to be looked for. The Ledger makes it worse: its latest value is
taken from a 30-day series, so a Metric that stopped 35 days ago shows "—", the
same as one never measured.

This milestone adds **Now**, the screen at `/`, and **Freshness**, the age of the
data. It turns a warehouse one consults into a tool one opens, and it makes
ageing data visible rather than silent. The decisions are in ADR 0045 and the
vocabulary in CONTEXT.md (**Now**, **Freshness**, **Latest value**).

## The concept

**Freshness is the age of the last datum, never of the last Import.** A datum is
a Measurement or a State. Sessions and the `Manual` Source take no part.

**Two grains.** The Account's last datum and its age in days against the UTC
today. Each Source's last datum measured against the Account's: within 30 days it
is **active** and its lag is shown, beyond that it is **retired** and folded
away. No setting, no colour.

**One card per Pin.** Its Latest value (last day holding one, no lookback bound,
through the engine), that day's date and age, the Goal in force today as a bound,
and the Attainment over the last 7 complete days. No chart, no range: a Pin
still carries no time axis (ADR 0025).

**Nothing seeded.** No Pin: Freshness plus a line on how to pin. No data: the
import invitation.

## Surfaces

- **Now** at `/`: the Freshness block, then the Pin cards in Pin order.
- **Dashboards** move to `/d` (index) and stay at `/d/$dashboardId`.
- **Ledger**: the latest value loses its 30-day bound and gains its age.
- **Not**: a banner on every page, notifications, colours, sparklines.

## Issues

1. `01-freshness.md`: data, now, api: Freshness at two grains, and the index it needs
2. `02-latest-value-and-the-pin-cards.md`: query, now, api: the Latest value and the Pin cards on `/v1/now`
3. `03-web-now-at-root.md`: web: Now at `/`, Dashboards at `/d`
4. `04-ledger-latest-and-docs.md`: query, web: the Ledger's latest value unbounded, and the docs pass

## Out of scope

- Continuous ingestion. Nothing here changes when it arrives: the ages shrink.
- A per-(Source, Metric) freshness matrix.
- Marking a Source retired by hand.
- Any threshold, colour or alert on an age.
- Today's value as such, or a verdict against today's Goal.

## Docs

- ADR 0045 and the CONTEXT.md entries land with this PRD.
- `04` carries the README and the ROADMAP row.
