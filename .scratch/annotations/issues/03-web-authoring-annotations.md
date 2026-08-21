Status: done

# 03: web: writing, editing and listing Annotations

## What

- **An annotation dialog**, `annotation-dialog.tsx`, modelled on
  `manual-entry-dialog.tsx`: label (required, the field that gets focus), an
  optional body, a start day, and an optional end day through the existing
  `day-range-picker.tsx`. It creates, edits and deletes, one dialog, three
  verbs, like the manual entry one, with the delete behind a confirm and only
  ever on an existing Annotation.
- **Opened from where you noticed the thing**: an "Add a note" entry on the
  Panel's menu (`panel-prefs.tsx`) and on the Metric page header, pre-filled with
  the date of the bucket under the cursor when there is one, and with today
  otherwise. Clicking an existing marker's tick opens that Annotation in the same
  dialog.
- **A third face on the Data page**: an "Annotations" section beside the Ledger's
  overview and detail, listing every Annotation of the Account in reverse
  chronological order, span, label, body, each row opening the dialog. This is
  the only view that answers "what have I written down", and it is where an
  Annotation outside the current range can be found at all.
- **Empty states** that say what an Annotation is for, in one line, rather than
  "No data": the Data page section is the one place a first-time reader meets the
  concept.
- **A contract test** in the same `internal/web/annotations_test.go`, on the one
  thing that is invisible when wrong: a write invalidates the annotations query
  and **not** the series ones. Assert the dialog's mutation names the annotations
  key and no series key. The rest of create, edit and delete is covered by issue
  01's handler tests; there is no front-end runner in CI, and standing one up for
  a dialog is not this milestone's job.

## Why here

The two entry points are not redundant. The dialog on the Panel is for the
gesture "that week, right there, was the flu", it exists because the date is
already on screen and typing it again is the friction that stops the note being
written. The Data page list is for everything after: finding a note from last
March, fixing a label, deleting one. A single entry point on the Data page would
mean every Annotation costs a navigation away from the chart that prompted it,
and the feature would go unused.

Invalidating only the annotations query on write matters: the Series are
unchanged by a note, and refetching four Panels' worth of aggregates because
someone typed "holiday" would make the cheapest write in the app the most
expensive one.

## Comments

Shipped. `web/src/components/annotation-dialog.tsx`, the three mutations in
`use-annotations.ts`, a `Textarea` primitive, the "Add a note" entry in the Panel's
settings popover and in the Metric page header, the Notes section on the Data page,
and `TestAnnotationWritesInvalidateOnlyTheNotes`.

Four deviations from the spec, each deliberate:

- **The Panel entry lives in `panel-card.tsx`, not `panel-prefs.tsx`.** The spec
  named the wrong file: `panel-prefs.tsx` is the localStorage summary-display
  preferences, while the Panel's own menu is the `PanelSettings` popover inside
  `panel-card.tsx`. The note entry sits there, above "Remove panel".
- **Two native date inputs, not `day-range-picker.tsx`.** That picker commits only
  once both ends are chosen, so it cannot express "one day, no end", which is the
  common case. Two `type="date"` fields also make the empty string the natural way
  to clear a span, which is exactly what the API takes.
- **The prefill is the last hovered bucket**, reported by the chart through a new
  `onHoverBucket` prop and held by the Panel. The cursor has left the chart by the
  time the menu is open, so the last hovered category is the honest answer; nothing
  hovered falls back to today.
- **Clicking a marker does not open its note.** A Recharts `ReferenceLine` is not a
  practical hit target, and the note is one hover away in the tooltip and one click
  away in the Data page list. It is the one bullet of this issue not built.

Verified: `npx tsc --noEmit`, `npm run build`, `make ci-go`, the contract test
checked to fail when the invalidation is widened to the Series, and a smoke test
over HTTP of exactly the bodies the dialog sends: create with `body: ""` and
`ends_on: ""` stores neither, a PATCH turns it into a span with a body, a second
PATCH with empty strings clears both, and DELETE answers 204 and empties the list.
A new handler test, `TestCreateAnnotationAcceptsEmptyOptionalFields`, pins that
contract so the dialog cannot be broken from the server side.

**Still not verified: the rendering.** No browser here and no front-end runner in
the repo, so no screen has been looked at across issues 02 and 03.
