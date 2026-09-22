# PRD: Dashboard templates

## Goal

A new Account gets the Overview (ADR 0018), and after that everyone starts again
from zero. Someone who wants to follow their sleep, a cut or an endurance block
has to know which of a hundred Catalog Metrics answer that question, which chart
suits each, and which of them can share a Panel. A curated board carries that
knowledge, and the first hour of use is where its absence costs the most.

This milestone adds three **Dashboard templates** (Sleep, Cut, Endurance),
offered when creating a Dashboard, and the **Dashboard file** format they are
written in, which is also the format a shared Dashboard will use (ROADMAP,
"Sharing a Dashboard as JSON"). The decisions are recorded in ADR 0047 and the
vocabulary in CONTEXT.md (**Dashboard template**, **Dashboard file**).

## The concept

**A template is a JSON file, embedded.** One file per template under
`internal/dashtemplate/templates/`, in the Dashboard file format
(`"format": "verve.dashboard/1"`). A contribution is a file and a passing test,
never Go.

**One validator.** The Panel rules (Catalog membership, chart type against
aggregation, four Metrics, two units, bucket, width) and the Dashboard tokens
(range preset, baseline rule) move out of `internal/api` into
`internal/dashtemplate`. The handlers and the templates' test call the same
code, so a template that HTTP would refuse fails the build.

**Only the Overview is seeded.** It becomes `overview.json`, seeded through the
same loader. Its content does not change.

**Instantiating copies.** An ordinary Dashboard, no link back, no propagation.

**Arrangement only.** No Goal, no Annotation, no Pin.

**Offered whatever the Account holds**, with the count of its Metrics that have
data ("4 of 5 have data"). Nothing is filtered out.

## The roster

Every chart type below must pass the shared validator; `04` of the issues is
where the exact files land, and the test is the arbiter.

**Sleep**: range `30d`, no Baseline.
"How long, how deep, and how the body recovered overnight."

| Panel | Metrics (chart) | Width |
| --- | --- | --- |
| 1 | `sleep` (stacked_bar) | 2 |
| 2 | `heart_rate_variability_sdnn` (line) | 1 |
| 3 | `resting_heart_rate` (line) | 1 |
| 4 | `respiratory_rate` (line) | 1 |
| 5 | `apple_sleeping_wrist_temperature` (line) | 1 |

**Cut**: range `3m`, no Baseline.
"Energy in against energy out, and what the scale and the protein say about it."

| Panel | Metrics (chart) | Width |
| --- | --- | --- |
| 1 | `calorie_balance` (diverging_bar) | 2 |
| 2 | `dietary_energy` (bar) + `total_energy_expenditure` (line) | 1 |
| 3 | `body_mass` (line) + `body_fat_percentage` (line) | 1 |
| 4 | `protein_per_kg` (line) | 1 |

**Endurance**: range `3m`, no Baseline.
"Training volume by week, and the fitness and recovery figures that move with it."

| Panel | Metrics (chart) | Bucket | Width |
| --- | --- | --- | --- |
| 1 | `training_time` (stacked_bar) | week | 2 |
| 2 | `training_distance` (stacked_bar) | week | 1 |
| 3 | `vo2_max` (line) | auto | 1 |
| 4 | `resting_heart_rate` (line) + `heart_rate_variability_sdnn` (line) | auto | 1 |
| 5 | `running_speed` (line) | auto | 1 |

**Overview**: unchanged from ADR 0018, `30d`, five width-1 Panels.

## Surfaces

- **"New dashboard" dialog**: a choice between an empty grid and one of the
  templates, each with its name, description, Metric count and coverage. Picking
  a template fills the name field with the template's name, still editable.
- **Nowhere else.** No gallery page, no banner, no onboarding step.

## Issues

1. `01-the-dashboard-file-and-its-validator.md`: dashtemplate, api: the Dashboard
   file format and the one validator
2. `02-overview-from-a-file.md`: dashtemplate, data: the Overview seeded from a
   file
3. `03-templates-over-http.md`: dashtemplate, query, api: listing and
   instantiating templates
4. `04-the-roster.md`: dashtemplate: Sleep, Cut and Endurance
5. `05-web-new-dashboard-from-a-template.md`: web: starting a Dashboard from a
   template, and the docs pass

## Out of scope

- Exporting a Dashboard to a file, importing one from a file.
- Seeding anything but the Overview.
- Any link between a template and its copies.
- Goals, Annotations or Pins in a template.
- Translation of template names and descriptions.
- A template per Account, or one an Account saves for reuse.

## Docs

- ADR 0047 and the CONTEXT.md entries land with this PRD; ADR 0018 carries an
  "Extended by" note.
- `05` carries `CONTRIBUTING.md` ("Contributing a Dashboard template"), the
  README feature list and the ROADMAP row.
