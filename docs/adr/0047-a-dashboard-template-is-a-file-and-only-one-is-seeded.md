# A Dashboard template is a file, and only one of them is seeded

_Extends ADR 0018, which stays in force: a new Account is still seeded with one
Dashboard, "Overview", and never with several._

## Context

The seeded Overview (ADR 0018) keeps a new Account off an empty screen. After
that, everyone starts again from zero: a person who wants to follow their sleep,
a cut or an endurance block has to know which of a hundred Catalog Metrics
answer that question, which chart suits each, and which ones share an axis. That
knowledge is exactly what a curated board carries, and it is the first hour of
use that pays for its absence.

The template itself is Go today: a slice of `{metric, chartType}` in
`internal/data/provision.go`. That was the right size for one board. It is the
wrong shape for several, for three reasons:

- **The rules a template must satisfy live in the HTTP layer.** The Panel caps
  (four Metrics, two units, ADR 0020) and the chart-type compatibility checks
  are in `internal/api/dashboardhandlers.go`. The seeded Panels bypass them, and
  nothing but review checks that a template would have passed them.
- **A contribution has to be data, not code.** The ROADMAP already names "Sharing
  a Dashboard as JSON" as the other half of an Archive (ADR 0039), and the same
  shape as a contributed palette (ADR 0026): declarative data a pull request can
  carry. A curated board is that file, written by Verve instead of by an Account.
- **Two formats would drift.** If templates are Go and a shared Dashboard is
  JSON, the day a Panel gains a field, one of them forgets it.

## Decision

**A Dashboard template is a JSON file embedded in the binary**, one per template,
under `internal/dashtemplate/templates/`. Its shape is the **Dashboard file**
format, versioned from the start (`"format": "verve.dashboard/1"`): a name, a
description, the time-axis tokens a Dashboard stores (`range_preset`,
`baseline_rule`), and an ordered list of Panels, each with its Metrics and chart
types, its optional bucket override and its width. It is the format a future
Dashboard export will write and its import will read, so that milestone adds a
route and no new shape.

**One validator, shared.** The Panel rules move out of the handlers into
`internal/dashtemplate` (Catalog membership, chart type against aggregation, the
four-Metric and two-unit caps, bucket, width, range and baseline tokens). The
handlers call it for a Panel an Account builds by hand, and a test calls it on
every embedded template. **A template that would be refused over HTTP fails the
build**, the same way `palette_test.go` holds a palette to its tokens.

**Only the Overview is seeded.** The argument of ADR 0018 against several themed
boards at creation still holds. The Overview becomes `overview.json`, and account
creation seeds it through the same loader, so there is one mechanism and not a
special case. Every other template is **offered**: the "New dashboard" dialog
lets the Account start from a template or from an empty grid.

**Instantiating copies.** A Dashboard created from a template is an ordinary
Dashboard, with no link back to the file. Editing it, renaming it or deleting it
touches nothing else, and a later change to the template does not reach a copy
already made. Instantiating twice makes two Dashboards.

**A template carries arrangement, never a judgement.** It declares Metrics,
charts, a range and a bucket. It never declares a Goal (ADR 0044: the direction
and the bound are the owner's to declare, and a shipped "7 h of sleep" would be
Verve deciding what is good), never an Annotation (those are the Account's own
dated facts, ADR 0030), and never a Pin (ADR 0025).

**A template is offered whatever the Account holds.** The listing says how many
of a template's Metrics have data for this Account ("4 of 5 have data"), and
filters nothing out: an empty Panel fills after the next import, as the Overview's
always have (ADR 0018), and hiding a board because a Watch has not been imported
yet would hide the reason to import it.

**The first roster is three**, each an answer to a question people already ask:
Sleep, Cut, Endurance. The Overview makes four files.

## Considered Options

- **Templates as JSON files, one validator, Overview seeded from the same loader
  (chosen).**
- **Keep templates in Go, add three more slices.** Smallest change, and it leaves
  the validation where only a request reaches it, so a wrong template is found by
  whoever opens it. Contributing a board would mean writing Go. Rejected.
- **Seed all four at account creation.** The first screen of a new Account would
  show four boards over data it does not have yet. ADR 0018 rejected exactly this,
  and nothing about it has changed. Rejected.
- **A template stays linked to its copies, and updates propagate.** It turns a
  convenience into a sync problem: which of the owner's edits wins over which of
  Verve's. A copy is what the owner asked for. Rejected.
- **Ship Goals with the templates** ("7 h of sleep", "10 000 steps"). It is the
  valence Verve refuses everywhere else, arriving by the side door. Rejected.
- **Hide the templates whose Metrics the Account has no data for.** It reads well
  after an import and badly before one, which is when a template matters most.
  Rejected in favour of stating the coverage.
- **Build Dashboard export and import in the same milestone.** The format is
  decided here, so they would cost little. But they carry their own questions (a
  file naming a Metric this build's Catalog lacks, a newer `format` version,
  what a name collision means) and they are not what the first hour needs.
  Deferred.

## Consequences

- `internal/dashtemplate` owns the Dashboard file format, its decoding, its
  validation and the embedded roster. `internal/api` loses its Panel validation
  to it, and `internal/data/provision.go` loses its `defaultPanels` slice.
- Two new routes: `GET /v1/dashboard-templates` and a `template` field on
  `POST /v1/dashboards`, which instantiates server-side in one transaction.
- Adding a template is a JSON file plus passing the test. `CONTRIBUTING.md`
  gains a section beside the one for palettes.
- The template names and descriptions are English, like the rest of the
  interface. Translating them is the same question as translating the interface.
- A template names Metrics by slug. A Metric renamed or removed from the Catalog
  breaks the template's test in the same change, which is the point of testing it.
- The picker is a list in a dialog. Past about eight templates, it needs a
  different control, and that is the number at which to reopen this.
