Status: done

# 02: web: the Pinned section and the Metric page toggle

## What

- **`web/src/hooks/use-pins.ts`**, following `use-dashboards.ts`: `usePins()`
  query plus `useAddPin()` / `useRemovePin()` mutations, both invalidating the
  pins query on success.
- **`AppShell`** (`web/src/components/app-shell.tsx`): a **Pinned** section
  between the Dashboards `<nav>` and the `border-t` block holding Data / Plan /
  Import.
  - Rendered **only when the list is non-empty**: no empty state, no header
    floating over nothing.
  - Same header treatment as "Dashboards" (`text-xs font-medium uppercase
    tracking-wide text-muted-foreground`), no create button beside it.
  - Each entry: `MetricIcon` + `metricLabel`, `Link` to `/data/$metric`, the
    same active styling the Data / Plan / Import entries use, keyed off the
    route param rather than the pathname.
  - **Filter against the Catalog**: entries whose slug is absent from
    `useMetricMap()` are not rendered. Nothing is deleted server-side; a Metric
    returning to the Catalog brings its Pin back with it.
  - **Hover unpin**: a small `X` (or `PinOff`) button revealed on hover on the
    right of the entry, calling `useRemovePin()`. It must not sit inside the
    `Link` (nested interactive elements): put both in a flex row, the `Link`
    taking the remaining width.
- **`MetricPage`** (`web/src/components/metric-page.tsx`): a toggle in the
  header next to the back button, `Pin` when unpinned and `PinOff` when pinned,
  with an `aria-label` and an `aria-pressed` reflecting the state. It reads
  `usePins()` and calls the matching mutation.
- No change to the Ledger rows or to the Panel legend links.

## Why here

The sidebar already owns the shell's navigation and already reads
`useDashboards()`; a Pins section is one more query and one more list in the
same place. The Catalog filter belongs on the client because the client already
holds the Catalog (`useMetricMap`), so no extra request and no server join buys
the same guarantee.

The toggle lives on the Metric page because the gesture is "pin this page", and
that is also what keeps the affordance to exactly one place. Unpinning is the
exception: it gets a second entry point on the sidebar row, because the row is
where you notice you no longer want it.
