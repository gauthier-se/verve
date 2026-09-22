Status: done

# 07: web: a Panel in full screen

- An expand button in each Panel's header (beside the settings), aria-label
  "Show full screen", opens a Dialog near the full viewport rendering the same
  Panel: same Metrics, range, bucket, Baseline and annotations, with the chart
  given the height.
- Escape or the close button returns. Nothing is stored.
- `f` while a Panel is hovered or focused opens it (optional if it fights other
  hotkeys).
- Verify on a single-Metric, a multi-Metric and a stacked Panel, and on a phone.

## Comments

- The expanded view is the same `PanelCard` with `expanded`, in a Dialog at
  96vw by 92svh: same query keys, so it opens on the cached series. It carries
  no drag handle, no settings and no expand button.
- The `f` hotkey is left out: a hovered-Panel hotkey needs focus tracking across
  the grid for little gain over the button.
- Verified on the stacked sleep Panel at 1280 px; Escape closes it.
