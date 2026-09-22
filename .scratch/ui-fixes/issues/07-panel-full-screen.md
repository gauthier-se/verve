Status: ready-for-agent

# 07: web: a Panel in full screen

- An expand button in each Panel's header (beside the settings), aria-label
  "Show full screen", opens a Dialog near the full viewport rendering the same
  Panel: same Metrics, range, bucket, Baseline and annotations, with the chart
  given the height.
- Escape or the close button returns. Nothing is stored.
- `f` while a Panel is hovered or focused opens it (optional if it fights other
  hotkeys).
- Verify on a single-Metric, a multi-Metric and a stacked Panel, and on a phone.
