Status: done

# 02: web: one height per row, and plots that line up

- `PanelCard`: `h-full` with `min-h-72` (width 1) / `min-h-80` (wider),
  so a grid row takes its tallest card and every card in it matches.
- `PanelSummary`: one line; the Baseline delta truncates with the full text in
  `title` on a width-1 card at `sm` and up. On a phone see `06`.
- Verify: Overview at two and three columns, with a Baseline on, every card in
  a row the same height and every plot starting at the same y.
