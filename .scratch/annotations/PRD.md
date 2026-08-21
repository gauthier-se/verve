# PRD: Annotations

## Goal

Let a curve be read against what was happening. Verve shows what the data says
and refuses to interpret it (ADR 0011): it will not colour a drop good or bad,
because it cannot know. But the owner knows. A week of flat steps was a week in
bed with the flu; a mass plateau started when the program changed; the resting
heart rate that climbed for ten days was a trip with bad sleep. None of that is
in Apple Health, and today there is nowhere in Verve to put it, so every reading
of a chart re-does the same act of memory.

An **Annotation** is that memory, dated and on the time axis. It is the fourth
differentiator promised alongside period comparison, cross-metric Panels and
derived Metrics, and the only one still unbuilt.

## The concept

**An Annotation belongs to the Account; showing it belongs to the Dashboard.**
That split is the design. The note "flu, 12-19 March" is a fact about the year,
not about the Training dashboard, so it is written once and appears wherever
that fortnight is on screen: every Panel of every Dashboard, the Metric page,
the same instant read against steps, sleep and mass without being entered three
times. Whether a given Dashboard *renders* its markers is a property of that
Dashboard's time axis, persisted beside `baseline_rule`, because the Dashboard
owns the time axis and Panels own the metric axis (ADR 0015).

**An Annotation is not a Measurement.** It carries no value, no unit, no Metric,
no aggregation rule, and it must never enter the Catalog. It is dated context
about a period, and the Metric it is read against is chosen by whatever Panel
happens to be on screen, which is exactly the property a Measurement does not
have. It lives in its own table and its own endpoint, and it never touches
`/v1/series`.

**An Annotation is a day or a span of days.** Verve's whole read path is
day-granular by construction (ADR 0012), so an Annotation gets a `starts_on` and
an optional `ends_on`, both `YYYY-MM-DD`, and no clock. A single day renders as a
marker, a span as a band. There is no third shape.

**The server folds an Annotation onto the bucket grid.** A Panel's X axis is
categorical, keyed on the bucket start date the server emitted
(`panel-chart.tsx:107`). A marker therefore has to name a bucket that exists in
that grid, not a date: on a month bucket, "12 March" is the marker on
`2026-03-01`. Resolving that is `internal/timeaxis`'s job, not the client's, for
the same reason the Baseline's ordinal alignment is computed server-side (ADR
0015): one module owns bucket boundaries, and both sides are pinned to it by
test. The endpoint takes the same time-axis tokens a Panel does and returns each
Annotation already carrying its `bucket` and, for a span, its `end_bucket`.

_Avoid_: Event (implies something Verve detected rather than something you
wrote), Note (loses the dating, which is the whole point), Marker and Band (the
rendering, never the concept), Tag (a label on an object, not a point in time),
Log (implies an append-only stream).

## What this milestone does

- **An Annotation is Account data.** New table `annotations (id, account_id,
  label, body, starts_on, ends_on)`, migration `0012`. `label` is short and
  required, it is what a tooltip shows; `body` is optional prose. `ends_on` is
  null for a single day and otherwise `>= starts_on`, enforced by a `CHECK`.
- **Five endpoints**, the plain CRUD shape the Dashboard handlers already use:
  `GET /v1/annotations` (a window, with the time-axis tokens, folded to buckets),
  `POST`, `PATCH /v1/annotations/{id}`, `DELETE /v1/annotations/{id}`. Every one
  scoped by `account_id`, so one Account can never read or edit another's.
- **A Dashboard-wide toggle**, `annotations` (boolean, default on), persisted on
  the Dashboard beside `baseline_rule` and forwarded like every other token. Off
  is one click, for the Dashboard where the markers are noise.
- **Markers and bands on every Panel.** A single-day Annotation is a
  `ReferenceLine` at its bucket; a span is a `ReferenceArea` from `bucket` to
  `end_bucket`. Both are recessed, the same visual weight class as the Baseline
  overlay, drawn behind the marks, never competing with the data. Several
  Annotations landing in one bucket collapse into a single marker carrying a
  count.
- **A label on hover**, in the existing chart tooltip rather than beside it: the
  bucket's values first, then the Annotations it holds. One tooltip per bucket,
  not two competing hover targets.
- **Authoring from where you noticed.** A "Add a note" action on the Panel's
  menu and on the Metric page opens a dialog pre-filled with the hovered
  bucket's date, because the moment you want to annotate a day is the moment
  you are looking at it. The same dialog edits and deletes.
- **A list on the Data page**, a third face beside the Ledger's overview and
  detail: every Annotation of the Account in reverse chronological order, with
  its span and its body, editable and deletable there. It is the answer to
  "what did I write down", which no chart can give.
- **The Metric page renders them too**, on the same terms as a Panel, with the
  toggle local to the page since a Metric page has no persisted time axis
  (ADR 0025 keeps Pins, and therefore Metric pages, free of stored axis state).

## What this milestone does NOT do

- **No categories, no colours, no icons.** A closed set of kinds (illness,
  travel, program…) is a taxonomy of *your* life, and Verve has no basis for
  deciding its members. A free label already says "flu" better than a colour
  does. This is deferrable in one direction only, so it stays deferred.
- **No derived or automatic Annotations.** A workout is not an Annotation
  (ADR 0028 names the temptation explicitly), an import is not an Annotation, a
  Phase boundary is not an Annotation. Everything here is typed by the owner,
  the same line the Manual source draws for Measurements (ADR 0022).
- **No sub-day Annotation.** "The run at 18:30" needs an intra-day axis the API
  deliberately does not serve (ADR 0012), and it would arrive with the hypnogram
  or not at all.
- **No attaching an Annotation to a Metric.** An Annotation scoped to
  `resting_heart_rate` would be invisible on the Panel where its effect actually
  shows, which is the failure the account-wide scope exists to avoid. If a note
  is about one Metric, its label says so.
- **No overlap rules.** Two Annotations may cover the same day. They are notes,
  not a state machine.
- **No annotation of a Baseline window.** The Baseline is a second window drawn
  on the current window's ordinal axis; a marker there would sit at a bucket
  whose date is not the date under it. Markers describe the current range only.
- **No sharing, no export.** Verve is single-Account per person by construction
  (ADR 0007), and the Ledger's copy-out already covers getting data elsewhere.

## Docs

- **A new ADR**: an Annotation is Account-scoped dated context, folded to the
  bucket grid server-side, and never a Measurement. Record the rejected
  alternatives: per-Dashboard Annotations, Annotation-as-Catalog-entry (a
  `label` family with no unit), client-side bucket resolution, a closed kind
  taxonomy, sub-day timestamps, and Metric-scoped notes.
- **A CONTEXT.md entry** for **Annotation** with its `_Avoid_` list, under the
  Dashboards section, next to Baseline and Time axis, where it belongs, since
  it is a property of the time axis and not of the data families.

## Issues

1. `01-annotation-model-and-api`, data and api: the migration, the store, the
   four endpoints, the bucket folding in `timeaxis`, the Dashboard toggle
   column, the ADR and the CONTEXT.md entry.
2. `02-web-annotations-on-the-time-axis`, web: markers, bands and the collapsed
   multi-marker on Panels and the Metric page, the tooltip section, the
   Dashboard toggle in the time-axis control.
3. `03-web-authoring-annotations`, web: the create/edit/delete dialog from a
   Panel and a Metric page, and the Annotations list on the Data page.
