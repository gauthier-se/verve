Status: done

# 02 — web: link Panel titles and legend entries to the Metric page

## What

- `panel-card.tsx`: a single-Metric Panel's title becomes a `Link` to
  `/data/$metric` (the multi-Metric joined title stays plain text — it names
  several Metrics, no single target).
- `panel-summary.tsx` (`PanelLegend`): each entry's `metricLabel` becomes a
  `Link` to its own `/data/$metric` — a multi-Metric Panel's legend already
  lists one Metric per entry, so each has an unambiguous target.
- No drag conflict: `dashboard-grid.tsx`'s sortable listeners are wired only
  onto `DragHandle`, never the title, so a title link does not fight
  reordering.

## Why here

Follow-up to `01-web-metric-page`: the Metric page's only entry point was the
Ledger. Every place a Metric's name is already shown on a Dashboard is a
natural link to its own page too.

## Comments
