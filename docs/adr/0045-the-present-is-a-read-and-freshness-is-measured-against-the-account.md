# The present is a read, and freshness is measured against the Account

## Context

Every screen in Verve is a window and a bucket. `/` forwards to the first
Dashboard, which draws a range. No screen says where the owner stands now, and
nothing says that the last datum is five weeks old or that steps stopped on the
12th. History draws the gaps (ADR 0032), but it has to be looked for, and a
stale warehouse looks exactly like a fresh one everywhere else.

It is worse than invisible in one place. The Ledger's latest value is taken from
a 30-day series (ADR 0021), so a Metric that stopped 35 days ago shows "—",
which is also what a Metric that was never measured shows. The one column that
should carry the staleness erases it.

The data to answer "how fresh is this" already exists. `Engine.Sources` reads
each Source's last datum (`MAX(start_at)`) for History, which only uses it to
date an arrival.

Three questions have an obvious answer that is wrong:

- **Fresh relative to what?** The obvious answer is now. But an iPhone replaced
  in 2019 has a last datum six years old and is not late, it is retired. A rule
  measured against now flags every device the owner ever replaced, forever, and a
  warning that is always on is read as decoration within a week.
- **Last import or last datum?** The obvious answer is the last Import, because
  it is recorded (`imports`). But re-importing an export taken from a phone that
  stopped syncing adds nothing new and moves the import date to today. The import
  date says when a file was read, not when the body was last measured.
- **What is "today's value"?** A Connector reads a file the owner exports (ADR
  0035), so without continuous ingestion today almost never holds data. A home
  screen of today's values is a screen of gaps. And a Goal never judges today
  (ADR 0044), so "today against the target" is the one comparison Verve has
  already refused.

## Decision

**Now is the screen at `/`.** It answers "where do I stand" in one call,
`GET /v1/now`, served by its own read module, `internal/now`, because it spans
the Pins, the Goals, the read engine and the Sources, which no single module owns
(ADR 0037). The Dashboard index moves to `/d`.

**Freshness is the age of the last datum, never of the last Import.** A datum is
a Measurement or a State; its day is the one the read engine would bucket it in.
Sessions are excluded, as they are from `Engine.Sources`: a workout's provenance
is read on the workout. The `Manual` Source is excluded too, because a typed
value says nothing about whether a device is still sending.

**Freshness has two grains, the Account and the Source.**

- The **Account's freshness** is its last datum's day and its age in days against
  the server's UTC today, the day Goals and windows already use. This is the fact
  "your data stops five weeks ago".
- Each **Source's freshness** is measured against the **Account's last datum, not
  against now**. A Source whose last datum is within 30 days of the Account's is
  **active**, and its lag behind the Account is shown. Anything older is
  **retired** and folded away, listed but not raised. The threshold is a
  constant, not a setting.

The two grains separate the two failures. "Nothing arrived for five weeks" is the
Account's age; "the Watch stopped while the phone kept going" is a Source's lag.
Neither needs the other's threshold.

**The Metric grain is on the Pins, not in the banner.** Now lists each Pin with
its **latest value**: the last day that holds one, however far back, read through
the engine so Source election and the Manual overlay apply as everywhere else.
The card prints the value, its date and its age ("7 912 · 12 Sept · 10 days
ago"). A card whose date is not today is the normal case, and its age is the
signal.

**A Pin's card carries the bound, and counts over a fixed week.** The Goal in
force today is printed beside the value as a bound, never as a verdict, with the
Attainment over the last 7 complete days ("5 of 6 measured days"). Seven days is
a constant, as the Ledger's windows are (ADR 0021): Now is one reading, and a Pin
still carries no time axis (ADR 0025).

**Nothing is seeded.** An Account with no Pin sees its freshness and a line
saying how to pin a Metric. An Account with no data at all sees the import
invitation, which is the only next step it has.

**Verve does not colour freshness.** An age is printed as an age. There is no
green, amber or red, and no threshold beyond the one that tells active from
retired, because how old is too old depends on the Metric and on the owner's
habits, and Verve knows neither.

## Considered Options

- **Account and Source, measured against the Account (chosen).**
- **Every Source against now.** Simplest, and it flags every replaced device for
  the rest of the Account's life. Rejected.
- **A per-(Source, Metric) matrix.** The most precise: the Watch still sends
  heart rate but no longer steps. It is also a grid most owners would scroll past,
  and the Pin cards already carry the Metric grain for the Metrics the owner said
  matter. Rejected as a banner; the question can be reopened on the Metric page.
- **A "retire this Source" action.** Exact, and it adds stored state and a
  settings surface for something the data already says. Rejected.
- **The last Import's date.** Recorded and cheap, and it answers when a file was
  read rather than when the body was measured. Rejected.
- **Today's value on each card.** Almost always a gap under file-based ingestion,
  and the one day a Goal never judges. Rejected in favour of the latest value and
  its age.
- **A banner in the shell above every page.** More visible, and it puts a
  permanent strip on every screen for a fact that changes once per import.
  Rejected; the screen that opens first carries it.
- **A sparkline per Pin.** Tempting, and a sparkline has a window, which makes
  the card a one-Panel Dashboard (ADR 0025). Rejected.
- **The Overview template's Metrics when no Pin exists.** A seeded Pin under
  another name, which ADR 0025 already refused. Rejected.

## Consequences

- `/` stops forwarding to the first Dashboard. The "Dashboards" tab and the
  Dashboard index live at `/d`; a Dashboard stays at `/d/$dashboardId`.
- ADR 0017's first Account now lands on Now rather than on its seeded Dashboard.
  With no data yet, Now shows the import invitation, which is the first step it
  has anyway.
- The Ledger's latest value is read the same way as a Pin's, with no lookback
  bound, so a Metric that stopped 35 days ago shows its last value and its date
  instead of "—". Its week and month columns are unchanged: those are windows.
- Reading each Source's last datum by `GROUP BY source` scans every row of the
  Account, which is acceptable for History and not for the screen that opens
  first. An index on `(account_id, source, start_at)` on both families lets
  SQLite answer each `MAX` from the index. It costs one more index on the largest
  table, paid at import.
- Continuous ingestion is not part of this. When a Connector ever reads
  continuously, nothing here changes: today's card starts to hold today's value,
  and the ages shrink.
