# A Pin is a shortcut to a Metric, not a one-Panel Dashboard

## Context

Every Metric has had its own full page since the Metric page shipped: a big
figure, its trend, its highs and lows, its history. Reaching it costs a detour
through the Data page every time, so the Metric someone checks every morning is
the one the app makes hardest to reach. The sidebar already lists the Account's
Dashboards, and it is where a person looks for the things they look at daily.

The obvious feature is "keep this page in the sidebar". The non-obvious part is
where it stops. A shortcut that also remembers a time range is a Dashboard with
one Panel wearing a different name, and it inherits, immediately, every question
that has already been answered for Dashboards: why has it no Baseline (ADR
0015), no bucket override, no second Metric (ADR 0020)? Each answer is
defensible in isolation and indefensible as a set, because the honest answer is
"because it is a Dashboard we did not want to admit was one".

The second question is where a Pin lives. Verve already has both postures: a
Dashboard is server data, per Account; the Appearance is `localStorage`, per
device (ADR 0024).

## Decision

**A Pin is a Catalog Metric the Account keeps in the sidebar, and it carries no
time axis.** No Time range, no Baseline, no bucket, no second Metric. Opening a
Pin opens that Metric's page on the page's own defaults. That single exclusion
is the concept: it is what makes a Pin cheap to build, cheap to explain, and
unable to drift into a degenerate Dashboard.

**A Pin is server data, per Account**: table `pins (account_id, metric,
position)`, three endpoints. A Pin asserts "this matters to me" about my data,
which is the same kind of claim a Dashboard makes and a different kind from "this
device paints in Nord".

**Both writes are idempotent.** `POST /v1/pins` answers 200 for an
already-pinned Metric and `DELETE /v1/pins/{metric}` answers 204 for an absent
one. A toggle asks for a state; a caller that gets the state it asked for has not
made an error. The uniqueness is carried by a `UNIQUE (account_id, metric)`
index rather than a read-then-write, so a double click cannot race into two rows.

**A Metric is a slug, validated at the API boundary**, not a foreign key: the
Catalog is compiled into the binary, not a table (ADR 0002). A Pin whose Metric
leaves the Catalog is filtered out **at render**, against the Catalog the client
already holds; the row stays in the database and the Pin returns if the Metric
does.

**Nothing is seeded.** A new Account gets zero Pins and no "Pinned" section
until it pins something.

## Considered Options

- **A shortcut with no time axis (chosen).** One concept, one exclusion, no
  overlap with Dashboards.
- **A Pin freezing the range chosen when it was pinned.** Attractive for about a
  day, until the frozen window is stale and there is no way to see that it is,
  because the page looks exactly like a fresh one. Rejected.
- **A Pin with its own editable, persisted range.** This is the honest version of
  the above and it is a one-Panel Dashboard. If that is what is wanted, the
  answer is a Dashboard, which already exists and already works. Rejected.
- **`localStorage`, like the Appearance.** Free: no migration, no endpoint. But
  ADR 0024 chose `localStorage` for reasons specific to appearance,
  cosmetic, per-device, and needed before first paint to avoid a flash, and none
  of the three hold for a Pin. A Pin that does not follow the Account is a Pin
  that vanishes on the second device. Rejected.
- **Seeded Pins for a new Account.** ADR 0018 seeds a Dashboard because an
  Account with none faces an empty app and no next step. An Account with no Pin
  faces its Overview and its full navigation: nothing is broken. Seeded Pins
  would fill the sidebar with a choice the owner did not make, and the first
  reflex would be to delete them. Rejected.
- **A cap on the number of Pins.** The list already scrolls, and a cap has to be
  explained at the moment it is hit, which is the worst moment. Rejected.
- **Greying a Pin whose Metric has no data.** It reads well and it costs a query
  per Pin on every render of the shell, to prevent a click onto a page that
  already says, in words, that there is no data. Rejected.

## Consequences

- The sidebar gains a "Pinned" section that renders only when non-empty, so it
  introduces itself the day it first appears rather than sitting empty from
  day one.
- `position` is written at insert and never exposed. Nothing in this sidebar
  reorders today: `position` is read-only in the Dashboard API and only Panels
  have an order endpoint. Pins being draggable while the Dashboards directly
  above them are not would be the odd thing. The column exists so that adding it
  later is a handler change, not a migration.
- Pinning is offered in exactly one place, the Metric page's header, because the
  gesture is "pin this page". Unpinning gets a second entry point on the sidebar
  row itself, because that is where you notice you no longer want it.
- The Ledger rows and Panel legends deliberately gain no pin control: both are
  already entirely clickable to navigate, and a hover button would put two
  competing targets on one line.
- If a Pin should ever need a range, this ADR is the thing to reopen, and the
  question to answer first is why the answer is not simply a Dashboard.
