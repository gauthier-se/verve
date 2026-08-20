# Verve

Verve is a self-hosted, open-source health data warehouse. It ingests health
data from external sources (Apple Health first), stores it in a canonical,
source-independent model, and visualizes it as metric graphs. The canonical
data outlives and does not depend on any single source.

## Language

### Data families

The canonical model recognizes distinct **families** of health data rather than
one uniform sample type. Each family has its own shape and storage.

**Measurement**:
A scalar value of a given metric at (or over) a point in time — e.g. a heart
rate reading, a step count, a body-mass entry. The dominant family by volume. Nutrition is *not* a separate
family: each nutrient (`dietary_energy`, `dietary_protein`…) is a Measurement
with `sum` aggregation.
_Avoid_: Sample, DataPoint, Reading (when the scalar meaning is intended).

**Meal**:
An optional grouping of the nutrient Measurements logged together (Apple's `Food`
correlation). Preserved on import as a link between Measurements, but not
surfaced in v1 — only needed to itemize "what did I eat".
_Avoid_: Food, Entry, Correlation.

**Recording** (deferred):
A high-frequency waveform such as an ECG (512 Hz). Fits none of the four
families; not modeled in v1. The waveform files are kept (referenced), and a
`Recording` family will be introduced when an ECG viewer is built.
_Avoid_: Waveform, Signal, Trace.

**State**:
A categorical state that holds over an interval — e.g. a sleep stage
(`InBed`, `AsleepREM`) or a stand hour. Defined by (state value, start, end).
The sleep States are read through the `sleep` **Metric** (see **Night** and
**Stage** under Sleep); the stand States are stored and deliberately unread,
since `apple_stand_time` already answers that question as a Measurement.
_Avoid_: Interval, Category, Phase.

**Event**:
A point-in-time marker with no duration and no scalar value — e.g. a low
heart-rate event, a fall. Defined by (kind, timestamp).
_Avoid_: Alert, Flag, Incident.

**Session**:
A rich, bounded activity aggregating multiple sub-measurements — e.g. a
workout with duration, distance, energy, an optional route, and summary
statistics. Sessions may reference their own child data (e.g. a GPS route).
Unlike a **State**, a Session is read as an **entity** and not as a **Metric**:
it has identity, so it gets a list, a detail and a map rather than a bucket, and
it appears on no Panel (ADR 0028). The interface says **Workouts** where the
domain says Session, because that is the word its owner uses and Session is kept
for the day a bounded activity is not a workout; the API follows the domain
(`/v1/sessions`).
_Avoid_: Activity (that is what a Session *was*, see below), Workout (Workout is
one *kind* of Session), Event.

**Activity**:
What a **Session** was: `running`, `cycling`, `traditional_strength_training`,
derived as a neutral slug from the source type without a table, so the set is
open by construction and holds every value a Connector emits (ADR 0011). Display
is what closes: a curated table maps a known Activity to a label, an icon, a
group (`cardio`, `strength`, `water`, `winter`, `other`) and whether it reads in
pace or in speed, and an unknown one falls back to its own prettified slug
(ADR 0002). The group is a server-side filter and not a decoration, which is why
the table is Catalog data rather than a web asset.
_Avoid_: Sport, Type (too generic), Workout type, Discipline.

**Session stat**:
One summary figure a **Session** carries: an aggregate of a canonical **Metric**
over the whole workout, keyed (Session, Metric, aggregate) with the aggregate
one of `sum`, `average`, `min`, `max`. An average heart rate and a maximum heart
rate are different answers, so all four are kept rather than collapsed to one.
Values are in the Metric's canonical unit, so a Session stat and a Measurement of
the same quantity are directly comparable. The distance and energy sums are also
promoted to columns on the Session, deliberately duplicated so the list sorts
and displays without a join per row (ADR 0028).
_Avoid_: Statistic (unqualified, it reads as a computed figure Verve derives),
Summary (that is the Panel term), Total (only one of the four aggregates).

**Route**:
A GPS track attached to a **Session**, stored as its GPX file under
`artifacts/` and referenced by content hash (ADR 0004). A Session may carry
several, and they stay separate: concatenating them draws a line between the end
of one segment and the start of the next, which is ground that was never
covered. A Route is served as its own **resource** and not as a **Series**: a
simplified polyline with its elevation and pace profiles, derived from the
artefact at read time, so the day-resolution cap on the series contract (ADR
0012) is untouched. Its geometric length is the profile axis and never a figure
on screen, because the Session's own `total_distance` is what the device measured
(ADR 0028).
_Avoid_: Track, Trace (fine in prose, not as the term), GPX (that is the file
format), Path.

### Dashboards

**Dashboard**:
A named, user-arranged grid of Panels — e.g. "Training", "Sleep", "Nutrition".
Users create several and switch between them. Carries the active Time range.
_Avoid_: View (too vague — informal synonym at most), Page, Board.

**Dashboard template**:
The curated default content — one Dashboard named "Overview" with a fixed set of
Panels over universal Metrics (mass, active energy, steps, resting heart rate,
exercise time) — **seeded at Account creation** so no Account ever faces an empty
app. Defined in Verve, not user input, like the closed Catalog; the seeded
Dashboard is thereafter an ordinary Dashboard the owner can edit or delete
(ADR 0018).
_Avoid_: Preset, Starter, Default view.

**Panel**:
A single card in a Dashboard: one to four Metrics, each rendered with its own
chart type (defaulted by its aggregation rule), over the Dashboard's time axis —
e.g. "Steps — daily — sum — bars", or a combo "dietary energy (bars) vs body
mass (line)". Metrics sharing a unit share a Y axis and a Panel spans at most
**two units** — hence at most two axes — so every curve keeps its true magnitude
(never normalized). A multi-Metric Panel does not render the Baseline —
co-variation between Metrics and comparison between periods are different
questions, not superposed (the same exclusion mechanic as the `all` range) — and
shows one Panel summary per Series in its legend rather than a single headline
figure (ADR 0020).
_Avoid_: Widget, Card, Chart, Tile, View, Overlay (the rendering, never a
concept).

**Panel summary**:
The headline figure a Panel shows above its curve so **magnitude** — not just the
shape of the trend — is legible at a glance, since the curve alone never reveals a
total or a mean. It is the Metric folded over the whole Time range, defined as **a
single bucket spanning the range**: one rule, no special case — an `average` is a
true count-weighted mean (never a biased mean of daily means), a `latest` Metric's
summary is simply its last value, and a derived Metric aggregates each operand over
the window *then* applies its Formula once (so a ratio is the period's real ratio).
An all-empty window is a **gap** ("—"), never a zero, following the same gap rule as
any bucket (ADR 0014). Rendered large, with the most recent bucket's value beside it
small; in **period comparison** it also carries a neutral **delta** against the
Baseline's own summary — direction and magnitude only, **never colored good/bad**,
because Verve does not know which direction is good for a given Metric. Computed
server-side and carried on the Series, never re-derived client-side (ADR 0019).
Universal on every Panel, not a per-Panel option.
_Avoid_: KPI, Stat, Big number, Headline, Total (only one aggregation's shape).

**Time range**:
The window a Dashboard shows (last 7 days, month, year, custom), applied to all
its Panels. A Panel may override its own bucket (day/week/month) but not the
range.
_Avoid_: Period, Window, Span.

**Baseline**:
The second, earlier Time range a Dashboard compares against when **period
comparison** is on — overlaid on every Panel as a recessed reference line. Like
the Time range, the Baseline is a Dashboard-wide property of the time axis and is
persisted with the Dashboard. It is defined by a **Baseline rule**, not by a
metric; a Dashboard with no Baseline (rule `none`) shows a single window as
before. Comparison is disabled when the Time range is `all` (nothing precedes
"all"). See ADR 0015.
_Avoid_: Comparison range (that's the feature), Reference (too vague), Previous
(only one of the rules), Overlay (that's the rendering).

**Baseline rule**:
How a Dashboard derives its **Baseline** window from the current Time range —
`previous` (shift back by the range's own length), `same_period_last_year` (shift
back one year), or `custom` (absolute frozen `from`/`to` dates). The relative
rules are *recomputed* from the current range, never stored as dates; only
`custom` is absolute (the same shape the Time range's `custom` preset uses). For
a `1y` range the two relative rules coincide.
_Avoid_: Comparison mode, Offset, Shift (that's the mechanic, not the choice).

**Ordinal alignment**:
How a Baseline series is laid over the current series on one chart: by **position
within the period** (bucket 1 vs bucket 1, "day 1 of each window"), not by
calendar date — the dates differ by construction. The overlay is **truncated to
the shorter** of the two windows, dropping orphan baseline buckets (a leap day, a
longer custom span) that have no counterpart. Each baseline bucket still carries
its own real date for the tooltip. Computed server-side so both windows are
provably consistent (see ADR 0015).
_Avoid_: Index alignment (jargon), Zip, Overlay (the visual result, not the rule).

**Time axis**:
The Dashboard-wide temporal frame its Panels render against: a **Time range**, its
optional **Baseline**, and the effective **bucket**, all resolved server-side from
the Dashboard's stored tokens (`range_preset`, `baseline_rule`, a Panel's bucket
override…) at read time. The Dashboard owns the time axis; Panels own the metric
axis (ADR 0015). Resolution — preset→window, rule→window, span→bucket — lives in
one module (`internal/timeaxis`), so the client forwards tokens instead of
computing dates.
_Avoid_: Timeframe (reads as a synonym of Time range), Time window (that's one
resolved bound pair, not the axis).

**Ledger**:
The tabular read-view of the same data the Panels graph — the numbers *behind* the
curves, so a value can be read exactly, sorted, and copied out. It is **not** the raw
Measurement rows (Verve never serves those — ADR 0012): a Ledger shows the same
server-aggregated **Series** the graphs use. Two faces on one page: an **overview**
(one row per Metric that has data, with its latest value and folded window figures) and
a **detail** table (one Metric's Points as chronological rows at a chosen bucket). It
reuses `GET /v1/series` and adds one small overview endpoint (`/v1/ledger`); every
figure is still folded server-side (ADR 0021). Surfaced as the "Data" page.
_Avoid_: Raw data (it is aggregated, not raw), Table (the rendering, not the concept),
Grid (that's the Panel layout), Spreadsheet.

**Pin**:
A Catalog **Metric** the Account keeps in the sidebar, beside its Dashboards, as a
shortcut to that Metric's page. A Pin carries **no time axis**, no Time range, no
Baseline, no bucket, no second Metric, and that exclusion *is* the concept: a Pin
that remembered a range would be a one-Panel **Dashboard** under another name, and
would owe an answer to every question already settled for Dashboards (ADR 0025).
Server data, per Account, like a Dashboard and unlike the **Appearance**, because a
Pin asserts "this matters to me" about the data rather than about this device.
Pinning and unpinning are idempotent, nothing is seeded, and a Pin whose Metric has
left the Catalog is hidden at render rather than deleted.
_Avoid_: Favorite (implies a judgement Verve does not make), Bookmark (a browser
concept), Shortcut (too generic), Watchlist (implies alerting Verve does not do).

### Sleep

**Night**:
The bucket a sleep interval falls in, and the reason sleep is not read at day
grain: a **Measurement** is an instant, a sleep **State** is a span that crosses
midnight by construction, so a calendar day splits every night in two and calls
neither half a night. A Night is the **noon-anchored day keyed on the morning it
wakes into** — `date(start_at, '+12 hours')` — so a night fragmented into dozens
of rows still lands whole in one bucket, labelled with the day the rest of the
Dashboard is talking about. A Night belongs to a **Time range** whole or not at
all, and a week or month bucket is folded from the Night labels rather than from
the intervals, so a Night is never in a different week than the day it is named
after. It is also the grain at which sleep's evidence is resolved — which
**Stage** rows and which **Source** count — for the same reason the **Manual
overlay**'s grain is the day: it is the grain at which the evidence actually
changes (ADR 0027).
_Avoid_: Sleep day (reads as a day spent sleeping), Session (that's the workout
family), Bedtime, Sleep period.

**Stage**:
One phase within a **Night** — `asleep_deep`, `asleep_core`, `asleep_rem`,
`asleep` (unspecified), `awake`, `in_bed` — carried per bucket as minutes and
rendered as the stacked bar's segments. The `sleep` Metric's value is **time
asleep**, the sum of the `asleep*` Stages: `awake` is always reported so the
stack shows the interruptions and never counted as sleep. `in_bed` is not a
Stage of the same nature but the container the others sit inside — an iPhone
records it over the very minutes a Watch records Stages in — so it counts only
in a Night that has no Stages at all, which is exactly an iPhone-only Account's
whole sleep history (ADR 0027).
_Avoid_: Phase (that's the Planning term), Sleep state (State is the family),
Level, Depth.

### Appearance

**Appearance**:
How Verve is painted, as a pair of **orthogonal** choices: a **Mode** and a
**Palette**. Orthogonal is the whole point — every Palette ships both a light and a
dark variant, so picking one never decides the other, and the Account is never asked
to choose between "Nord" and "dark". A display choice, held per device, that touches
no stored data (ADR 0024).
_Avoid_: Theme (it names the pair *and* each half, which is exactly the ambiguity
this term exists to remove), Skin, Look.

**Mode**:
How light or dark the surfaces are: `light`, `dark`, or `system` — the last
following the operating system and changing with it, mid-session, without asking.
_Avoid_: Dark mode (that is one value, not the axis), Color scheme, Appearance
(that is the pair).

**Palette**:
The named color set a Mode is painted in: Verve, Catppuccin, Dracula, GitHub,
Gruvbox, Nord, Rosé Pine, Solarized, Tokyo Night.
A Palette owns the chrome (surfaces, text, borders, the accent) **and** the
categorical chart ramp, so a Panel's curves belong to the same world as the page
around them. It deliberately does **not** own the colors that carry meaning rather
than style — the destructive action, and the diverging surplus/deficit pair of a
signed Metric — which are identical in every Palette (ADR 0024). Like the Catalog,
the set is **closed**: Verve defines the Palettes, the Account picks one; there is no
custom color, because a hand-picked hue cannot be checked for contrast or for
color-vision separation across four series. All but Verve are named after themes
people already use elsewhere, because the criterion is **recognition**, not variety
(ADR 0026); a named Palette is a *verified adaptation* of that theme, never a
certified reproduction, since Verve requires a light and a dark variant of every
Palette and holds both to AA contrast and to chart separation, which the upstream
themes were not designed against.
_Avoid_: Theme, Color scheme, Skin, Preset (that is the Time range's word).

### Planning

**Estimate**:
A quantity Verve *infers* about the Account rather than one a Source *recorded* —
the load-bearing distinction of this section. A Metric answers "what was measured";
an Estimate answers "what is true", which the measurement may approximate poorly.
Estimates are computed on read, never stored as Measurements, and never enter the
Catalog: they are not observations and must never be graphable beside one.
_Avoid_: Calculation, Prediction (only one basis is predictive), Derived value
(a derived Metric is a different, Catalog-level thing).

**Basal estimate**:
The Account's resting energy expenditure as produced by a **basal equation** —
what the body spends at rest, inferred from body composition or anthropometry.
Deliberately **not** `basal_energy`, the imported Metric holding Apple's own
figure: the two routinely disagree by hundreds of kcal, and conflating them would
make the disagreement invisible.
_Avoid_: BMR (acronym; fine in prose, never as the canonical term), Basal energy
(that's the Metric), Resting metabolism, Metabolism.

**Basal equation**:
One of the published formulas producing a **Basal estimate** — Katch-McArdle and
Cunningham (from lean mass), Mifflin-St Jeor and Harris-Benedict (from mass,
height, age, sex). The Account chooses; Verve preselects the most accurate the
available data supports and greys out those whose inputs are missing. An equation
is data, not a branch in code.
_Avoid_: Formula (that's the derived-Metric language — a different thing),
Method, Model.

**Expenditure estimate**:
The Account's total daily energy expenditure — the figure a calorie target is
built on. Deliberately **not** the `total_energy_expenditure` derived Metric
(`active_energy + basal_energy`), which is what Apple *recorded* and which can
overstate the truth by a third. Every Expenditure estimate carries the
**Estimate basis** that produced it, because a figure whose provenance is unknown
cannot be trusted or argued with.
_Avoid_: TDEE (acronym; and it reads as a synonym of the Metric it must not be
confused with), Total energy expenditure (that *is* the Metric), Maintenance
(that's the target at rate zero, not the estimate).

**Estimate basis**:
Which evidence produced an **Expenditure estimate**, best first: `observed`
(back-computed from logged intake against the body-mass trend — what the body
actually did), `recorded` (the mean of `total_energy_expenditure` — what the
devices claim), or `predicted` (**Basal estimate** times a chosen activity
factor — what an equation guesses). Verve picks the best basis the data supports
and always names it; the basis is part of the answer, not an implementation
detail.
_Avoid_: Source (that's the origin of a Measurement — a different concept),
Method, Confidence, Tier.

**Phase**:
A bounded stretch of time over which the Account pursues one **target rate** —
a cut, a bulk, a maintenance stretch. Phases are kept as a history, never
overwritten, so each is judged against the window it actually ran over and the
question "was I doing what I meant to be doing?" stays answerable about the past.
A new Phase closes the current one; at most one is open.
_Avoid_: Goal (a goal has no end), Plan (that's the page), Cycle, Period (that's
the time-axis vocabulary), Program.

**Target rate**:
The speed of body-mass change a **Phase** aims at, expressed as a percentage of
body mass per week — signed, so a bulk is positive and a cut negative. A rate,
not a calorie figure, because the same deficit is trivial at one body size and
dangerous at another; the calorie target is *derived* from it against the
**Expenditure estimate**. Named zones (aggressive cut, moderate cut, maintenance,
lean bulk) label regions of the scale; they are vocabulary, not discrete options.
_Avoid_: Deficit / Surplus (only one sign each, and both name the calorie figure),
Goal, Pace, Intensity.

### Cross-cutting

**Metric**:
A canonical, source-independent kind of measurable thing — e.g. `heart_rate`,
`steps`, `body_mass`. Each Metric has a stable neutral slug, one canonical unit,
and an **aggregation rule** — one of `sum` (steps, calories), `average` with
min/max band (heart rate, speed), `latest` (body mass), or `duration_by_state`
(sleep). The rule, not the user, decides how points collapse into a time bucket. The set of Metrics is the **Catalog**: closed (defined
in Verve, not free-form strings) but extensible (new entries added deliberately).
Neutral names, never Apple identifiers (`heart_rate`, not
`HKQuantityTypeIdentifierHeartRate`).
A Metric has one of two natures: **imported** (produced by a Connector, carries
its own aggregation rule) or **derived** (defined by a **Formula** over other
Metrics and computed on read, e.g.
`calorie_balance = dietary_energy − (active_energy + basal_energy)`). A derived
Metric has **no aggregation rule of its own**: at the requested bucket each
operand is aggregated by *its own* rule and the Formula is then applied per
bucket (see **Formula** and ADR 0014). Only imported Metrics existed in
v1; derived Metrics are the first differentiator after the v1 core.
_Avoid_: Type, Kind, Signal, Indicator.

**Catalog**:
The closed, curated set of canonical Metrics that Verve understands, each with
its canonical unit and aggregation rule. A Connector must map incoming data to a
Catalog Metric.
_Avoid_: Registry, Dictionary, Schema.

**Formula**:
The declarative definition of a derived Metric: a **ratio of two weighted sums**
of other Metrics, times an optional constant — `(k · Σ aᵢ·numᵢ) / (Σ bⱼ·dénᵢ)`.
A missing denominator is 1 (a plain weighted sum, e.g. `calorie_balance`). Every
operand is **required**: if any operand — or the whole denominator — has no data
in a bucket, that bucket is a **gap** (no value), never a zero. A Formula is
data, not code, so a Connector-style compiled definition today can back a
user-defined editor later (ADR 0014). Deliberately not a general expression: no
nesting, no operator precedence.
_Avoid_: Expression, Equation, Rule (the aggregation rule is a different thing).

**Unmapped bin**:
Where a Connector puts incoming data whose type it cannot map to a Catalog
Metric. Kept and inspectable, never discarded — so no source data is lost even
when the Catalog does not yet cover it.
_Avoid_: Raw table, Dead letter, Reject pile.

**Source**:
The origin that produced a piece of data — e.g. "Apple Watch", "Yazio",
"Nike Run Club". Apple Health is itself only an aggregator of upstream
sources, never the canonical owner. Every family carries its Source.
_Avoid_: Provider, Device, Origin (Device is narrower — the physical hardware).

**Manual entry**:
A Measurement the Account typed rather than a Connector imported, carrying the
reserved Source `Manual`. It is an ordinary Measurement in the store — same table,
same content key — but it does **not** compete in Source priority: a human corrects
individual days, whereas a device produces a continuous stream, so ranking one
against the other whole-range would let a single typed value blank out a year of
readings. Instead a Manual entry **overlays**: see **Manual overlay**. Manual
entries are the **only** Measurements Verve will delete — a human typo must be
undoable, whereas deleting an imported row would not stick (its content key would
vanish with it and the next Import would restore it).
_Avoid_: Manual measurement (verbose), Custom data, User data.

**Manual overlay**:
How a **Manual entry** displaces imported data: on any **day** where the Account has
typed a value for a Metric, that day's Manual rows replace the winning Source's rows
for that day, and every other day is untouched. The day is the grain because that is
the grain at which a person corrects a record. The overlay applies before any
aggregation, so bucketing, summaries and Formula operands all see one already-resolved
row set and need no special case — and it is skipped entirely for a Metric with no
Manual rows, which is every Metric on an Account that has never typed one.
_Avoid_: Override (suggests the device row is hidden or lost — it is neither),
Source priority (that ranks whole Sources over a range; this is a different, finer
mechanic), Merge.

**Import**:
A single run of a Connector over a source file (e.g. one Apple Health export
`.zip`), recorded with its time, file, and counts of rows added vs skipped.
Imports are idempotent: re-importing a full Apple snapshot adds only new data.
_Avoid_: Sync, Ingest (the act), Load.

**Import job**:
A single web import *in flight*: the background run of the Connector over an
uploaded export, tracked by a status (`pending → running → done | failed`) and a
two-phase progress percentage (upload, then decode). Held in an in-memory
registry, one per Account at a time; on success it carries the same report as a
CLI import. Distinct from the **Import** — the persisted record of a *finished*
run with its counts. A crash loses the job, not data: re-upload is idempotent
(ADR 0016).
_Avoid_: Task, Upload (that's one phase), Import (the finished record).

**Content key**:
The deduplication identity of a Measurement, derived by hashing
`(metric, source, startDate, endDate, value, unit)` — because Apple records
carry no stable ID. `creationDate` is deliberately excluded (it shifts between
exports). A row whose content key already exists is skipped on re-import.
_Avoid_: Fingerprint, Dedup key, Hash.

**Source priority**:
A per-Metric ordering of Sources used to resolve overlap at read time. Verve
keeps every Measurement from every Source (non-destructive); when a graph needs
one series, it picks values from the highest-priority Source that has data —
e.g. Watch over iPhone for `steps`, to avoid double-counting. Distinct from
*merging* Sources (combining complementary coverage), which is a future
refinement.
_Avoid_: Deduplication (too narrow — it's resolution, not row removal), Ranking.

**Account**:
A person who logs into Verve and owns their own data. Every piece of data
(Measurement, Dashboard, Import, Annotation…) belongs to exactly one Account;
Accounts never see each other's data. Verve is multi-user from v1 — health data
is intimate and isolation is strict. An Account also carries the static profile
attributes from Apple's `Me` (date of birth, biological sex, blood type…), used
to normalize some Metrics (e.g. age-based heart-rate zones).
_Avoid_: User, Owner (use "owns"/"owner" only as the relationship), Profile,
Tenant.

**Bootstrap**:
The creation of the *first* Account on a fresh instance, done from the web (email
+ password) with no shell. Web signup is open **only while zero Accounts exist**;
once the first Account is created it **closes** — enforced server-side — and
further Accounts are created via the CLI. The first Account is auto-logged-in and
lands on its seeded Dashboard (ADR 0017).
_Avoid_: Signup, Registration (web signup is closed after this), Onboarding (the
broader flow), Setup.

**Connector**:
A component that reads data from an external system and maps it into the
canonical families — e.g. the Apple Health export importer. Compiled into the
binary and registered in a registry; the community contributes new ones by PR.
Its **mapping** (source type → Catalog Metric + unit conversion) is declarative
data, so a Connector's code is mostly "how to read the source", not "what maps
to what".
_Avoid_: Importer, Adapter, Plugin, Integration.
