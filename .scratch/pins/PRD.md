# PRD: Pins

## Goal

Let an Account keep the Metrics it actually cares about one click away, in the
sidebar, beside its Dashboards. Verve already gives every Metric a full page
(`/data/$metric`), but reaching it always costs a detour through the Data page.
Someone who checks their body mass every morning should find it where they find
everything else they look at daily.

## The concept

A **Pin** is a Metric of the Catalog the Account keeps in the sidebar. It is a
shortcut to that Metric's page, and deliberately nothing more: it carries no
Time range, no Baseline, no composition. That line is the whole design. A Pin
that carried a persisted Time range would be a one-Panel Dashboard under another
name, and would immediately owe an answer to "why has my Pin no Baseline, no
bucket override, no second Metric" (ADR 0015, ADR 0020).

_Avoid_: Favorite (implies a judgement Verve does not make), Bookmark (a browser
concept), Shortcut (too generic), Watchlist (implies alerting Verve does not do).

## What this milestone does

- **A Pin is server data, per Account.** New table `pins (id, account_id,
  metric, position)` with `UNIQUE (account_id, metric)`, migration `0010`. A Pin
  states "this matters to me" about my data, which is the same reason a
  Dashboard is server-side and the Appearance is not (ADR 0024).
- **Three endpoints**: `GET /v1/pins`, `POST /v1/pins`, `DELETE
  /v1/pins/{metric}`. The metric slug is the natural key, and it is what the
  client's toggle already holds. `POST` validates the slug against the Catalog
  and is idempotent.
- **A "Pinned" section in the sidebar**, below the Dashboards list and above the
  fixed Data / Plan / Import block. It renders only when non-empty, so it
  explains itself the day it appears. Each entry is `MetricIcon` + label, linking
  to `/data/$metric`, with the same active styling the Dashboard entries use.
- **Pin and unpin from the Metric page**: a `Pin` / `PinOff` toggle in the
  header, next to the back button. Unpin also from the sidebar entry on hover,
  because going back to a page you no longer visit in order to leave it is
  absurd.
- **Insertion order.** `position` is written at insert and never exposed. It is
  in the schema from day one so drag-reordering costs no migration later.

## What this milestone does NOT do

- **No reordering.** Nothing in this sidebar reorders today: `position` is
  read-only in the Dashboard API (`internal/api/dashboardhandlers.go:84`), and
  only Panels have an order endpoint. Pins being draggable while the Dashboards
  directly above them are not would be the odd thing, not the reverse.
- **No limit on the number of Pins.** The sidebar list already scrolls.
- **No seeded Pins.** ADR 0018 seeds a Dashboard because an Account with none
  faces an empty app with no next step; an Account with no Pin faces its
  Overview and its full nav. Seeding Pins would fill the sidebar with a choice
  the owner did not make, and the first reflex would be to delete them.
- **No pin control on Ledger rows or Panel legends.** Those rows are already
  entirely clickable to navigate; a hover button would put two competing targets
  on one line. The gesture is "pin this page", so the control belongs to the
  page.
- **No state for a Metric with no data.** Knowing whether a Metric has data
  takes a query the sidebar does not make and must not make: greying an entry
  would cost one round-trip per Pin on every shell render, to prevent a click
  that already explains itself. A Metric no longer in the Catalog is filtered at
  render against the already-loaded `useMetricMap`; the row stays in the
  database and comes back if the Metric does.
- **No change to the Metric page's Time range.** Its preset stays local state at
  `3m`. Persisting the last-used preset in localStorage, globally across
  Metrics, is a worthwhile follow-up but it is a display preference and belongs
  in its own PR, not mixed into a server-content feature. Per-Metric persistence
  is explicitly rejected: two Metrics opened in a row would show different
  windows with nothing explaining why.

## Docs

- **A new ADR**, whose decision is the demarcation line above: a Pin is a
  shortcut to one Metric, it carries no time axis, and that is what keeps it
  from drifting into a degenerate Dashboard. Record the rejected alternatives:
  localStorage instead of the server, Metric + frozen range, Metric + editable
  range, seeded Pins.
- **A CONTEXT.md entry** for **Pin** with its `_Avoid_` list, under the
  Dashboards section.

## Issues

1. `01-pin-model-and-api`, data and api: the migration, the store, the three
   endpoints, the ADR and the CONTEXT.md entry.
2. `02-web-pins-sidebar`, web: the sidebar section, the Metric page toggle, the
   hover unpin.
