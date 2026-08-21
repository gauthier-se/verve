Status: done

# 05: web: shell, dashboard, metric page, ledger, import

## What

- **Shell**: 240px sidebar with eyebrow sections, a drawn mark carrying the
  palette's accent, the host in mono, and one `TOOLS` list feeding both the
  sidebar and the narrow-screen tab bar — so a page reachable on a desktop
  cannot be unreachable on a phone. Below the breakpoint: a slim bar of account
  controls at the top, a scrolling tab bar at the bottom with a 3px active rule.
- **Dashboard**: sticky translucent header; a meta line naming the resolved
  window, the compared window and the grain (all server-resolved); auto-fit grid
  of `minmax(20rem, 1fr)`; a wider panel is a taller panel.
- **Panel card**: headline figure, then curve. A mono note beside the title says
  how the figure was arrived at — "sum · weekly buckets", "two axes", "derived ·
  ratio", "stacked · 214 nights recorded" — replacing the bare bucket name.
  Stage legend for a stacked night, sign legend for a diverging bar.
- **Metric page**: hero figure, 250px chart, server-resolved axis marks, the
  annotation chip strip, and four stat cards (Highest, Lowest, Readings,
  Coverage). Coverage is "buckets with data of buckets in the window", which is
  what makes the figures above it honest.
- **Ledger**: mono bucket keys (`2026-W34` for a week, derived from a Monday the
  server already named, by the ISO rule the server buckets with), a Readings
  column, and the table primitive restyled — header band, quieter row rules.
- **Import**: the three-step rail (which reads the real panel count and the real
  `has_data`), drop zone, progress card, report figure strip, unmapped card.

## Notes

The failure card says what actually happens: an import has no rollback, so
whatever landed is kept and re-dropping the same export picks up where it stopped
without duplicating anything (`importjob.go`).
