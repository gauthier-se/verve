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
_Avoid_: Interval, Category, Phase.

**Event**:
A point-in-time marker with no duration and no scalar value — e.g. a low
heart-rate event, a fall. Defined by (kind, timestamp).
_Avoid_: Alert, Flag, Incident.

**Session**:
A rich, bounded activity aggregating multiple sub-measurements — e.g. a
workout with duration, distance, energy, an optional route, and summary
statistics. Sessions may reference their own child data (e.g. a GPS route).
_Avoid_: Activity, Workout (Workout is one *kind* of Session), Event.

### Dashboards

**Dashboard**:
A named, user-arranged grid of Panels — e.g. "Training", "Sleep", "Nutrition".
Users create several and switch between them. Carries the active Time range.
_Avoid_: View (too vague — informal synonym at most), Page, Board.

**Dashboard template**:
The curated default content — one Dashboard named "Aperçu" with a fixed set of
Panels over universal Metrics (mass, active energy, steps, resting heart rate,
exercise time) — **seeded at Account creation** so no Account ever faces an empty
app. Defined in Verve, not user input, like the closed Catalog; the seeded
Dashboard is thereafter an ordinary Dashboard the owner can edit or delete
(ADR 0018).
_Avoid_: Preset, Starter, Default view.

**Panel**:
A single card in a Dashboard: one or more Metrics × a chart type × an
aggregation × a time bucket — e.g. "Steps — daily — sum — bars".
_Avoid_: Widget, Card, Chart, Tile, View.

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
