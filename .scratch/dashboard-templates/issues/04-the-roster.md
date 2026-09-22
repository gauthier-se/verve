Status: done
Blocked by: 02

# 04: dashtemplate: Sleep, Cut and Endurance

## What

- **`sleep.json`, `cut.json`, `endurance.json`** under
  `internal/dashtemplate/templates/`, with the Panels, charts, buckets, widths
  and descriptions in the PRD's roster tables.
- Where the validator refuses a choice in the PRD, the validator wins: change
  the chart type to the nearest one it accepts and note the change in this
  file's Comments, rather than loosening a rule.
- Look at each one on the reference Account (`sample_data/`), in light and dark,
  and at a phone width: a width-2 Panel next to a width-1 one must not leave a
  hole in the grid at three columns. Reorder if it does.

## Tests

- `templates_test.go` from `02` covers them with no new code.
- One assertion per template that it names no Metric twice on one Panel and has
  no Panel identical to another.

## Not

- No Goals, Annotations or Pins in any file (ADR 0047).

## Comments

- The three files landed with `03`, which needed them for its listing tests.
  Every chart type in the PRD passed the validator unchanged.
- `TestNoTemplateRepeatsItself` is in `templates_test.go`.
- Left: the look on the reference Account, in both Modes and at phone width,
  which needs the picker from `05` to reach the boards the way an Account will.
- Looked at on a real Account in the preview: Sleep and Endurance read as
  designed. Cut left a hole at two columns (width 2, 1, 2, 1). The grid is an
  auto-fit of 1 to 5 columns, and the only arrangement with no hole at every
  count is one leading wide Panel followed by single-column ones, so that is now
  a rule (`TestAWidePanelOnlyLeads`, and CONTRIBUTING). Cut's energy Panel went to
  width 1 and moved up; the PRD table is updated.
