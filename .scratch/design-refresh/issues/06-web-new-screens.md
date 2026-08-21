Status: done

# 06: web: /cross and /history

## What

- **`/cross`** (`cross-metric-page.tsx`): the pairwise matrix (hue for direction,
  alpha for strength, diagonal a dot, `|ρ| < 0.15` printing no number), the
  scatter of the strongest pair with the server's fitted line, and the ranked
  relationships with a proportional track and a toneless direction phrase. Lag
  and range as mono segmented controls. An empty state that explains the page is
  fed by the Pins.
- **`/history`** (`history-page.tsx`): the band with phases behind it and gaps
  shaded, a metric picker, and the event ledger — a continuous rail, a dot
  coloured by kind, a mono date column, and figure chips. The sentences live
  here, not in the API: most of them are Verve's promises about the data, and a
  promise belongs next to the evidence for it.
- Both routes registered in `router.tsx` and present in `TOOLS`, so they appear
  in the sidebar and in the narrow tab bar at once.
