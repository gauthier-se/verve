// Shared shapes mirroring the Go JSON API (the versioned contract, ADR 0005).

/** Aggregation is a Metric's Catalog rule for collapsing points into a bucket. */
export type Aggregation = "sum" | "average" | "latest" | "duration_by_state";

/** ChartType is how a Panel renders its Metric (diverging-bar is signed-only, ADR 0014). */
export type ChartType = "bar" | "line" | "area" | "band" | "stacked_bar" | "diverging_bar";

/** Bucket is a Panel's time granularity; null means auto-derive from the span. */
export type Bucket = "day" | "week" | "month";

/** RangePreset is a Dashboard's Time-range choice (custom uses from/to). */
export type RangePreset = "7d" | "30d" | "3m" | "1y" | "all" | "custom";

/** BaselineRule is how a Dashboard derives its Baseline window (ADR 0015): `none`
 *  is off, the relative rules are recomputed server-side, `custom` carries bounds. */
export type BaselineRule = "none" | "previous" | "same_period_last_year" | "custom";

/** Term is one Formula operand: a Catalog slug weighted by a coefficient. */
export interface Term {
  metric: string;
  coefficient: number;
}

/** Formula is a derived Metric's definition: (scale · Σ numerator) / (Σ denominator),
 *  an empty denominator meaning 1 (ADR 0014). */
export interface Formula {
  scale: number;
  numerator: Term[];
  denominator?: Term[];
}

/** Metric is one Catalog entry from GET /v1/metrics; a derived Metric carries a
 *  Formula and signed flag and no aggregation (ADR 0014). */
export interface Metric {
  slug: string;
  unit: string;
  aggregation?: Aggregation;
  nature: "imported" | "derived";
  signed?: boolean;
  formula?: Formula;
}

/** AuthState is GET /v1/auth/state: whether the instance still needs its first
 *  Account, so the SPA can pick the create-account vs. login screen (ADR 0017). */
export interface AuthState {
  needs_bootstrap: boolean;
}

/** Account is the logged-in identity and its `Me` profile. */
export interface Account {
  email: string;
  date_of_birth: string | null;
  biological_sex: string | null;
  blood_type: string | null;
}

/** PanelMetric is one Metric on a Panel with its own chart type (ADR 0020);
 *  the list order is the display order. */
export interface PanelMetric {
  metric: string;
  chart_type: ChartType;
}

/** Panel is one card in a Dashboard: one to four Metrics spanning at most two
 *  units — two Y axes — rendered as one combo chart (ADR 0020). */
export interface Panel {
  id: number;
  metrics: PanelMetric[];
  bucket: Bucket | null;
  width: number;
  position: number;
}

/** Dashboard is a named grid of Panels carrying the active Time range and Baseline
 *  (ADR 0015); bounds are present only for the `custom` preset/rule. */
export interface Dashboard {
  id: number;
  name: string;
  position: number;
  range_preset: RangePreset;
  range_from: string | null;
  range_to: string | null;
  baseline_rule: BaselineRule;
  baseline_from: string | null;
  baseline_to: string | null;
  /** annotations is whether this Dashboard draws the Account's Annotation markers.
   *  The notes are Account data; only their showing is a Dashboard property, because
   *  the Dashboard owns the time axis they sit on (ADR 0030). */
  annotations: boolean;
  panels: Panel[];
}

/** Point is one aggregated bucket; min/max carry the average band. A baseline point
 *  may be a dated gap (`gap: true`) where the Baseline has no data (ADR 0015). */
export interface Point {
  bucket: string;
  value: number;
  min?: number;
  max?: number;
  gap?: boolean;
  /** states is the bucket's minutes per Stage, carried only by a duration_by_state
   *  Metric (ADR 0027): what the stacked bar stacks. `value` stays the single scalar
   *  every other reader uses — for sleep, time asleep — so `awake` appears here and is
   *  never counted there. */
  states?: Record<string, number>;
}

/** ImportReport is the compact outcome of a finished web import (ADR 0016). */
export interface ImportReport {
  source_file: string;
  added: number;
  skipped: number;
  unmapped: number;
}

/** ImportJob is one web import in flight or settled: its lifecycle status, the
 *  active phase, a single 0–100 percent, and the report or failure message (ADR 0016). */
export interface ImportJob {
  status: "pending" | "running" | "done" | "failed";
  phase: "upload" | "import";
  percent: number;
  report?: ImportReport;
  error?: string;
}

/** ImportStatus is GET /v1/imports: the Account's current job (or null) plus
 *  whether it has any data yet, which drives the dashboard's empty-state CTA. */
export interface ImportStatus {
  job: ImportJob | null;
  has_data: boolean;
}

/** LedgerValue is a Metric's most recent daily value with the day it fell on. */
export interface LedgerValue {
  value: number;
  date: string;
}

/** LedgerRow is one Metric's line in the Ledger overview (GET /v1/ledger, ADR 0021):
 *  its latest value and window figures folded server-side, plus a week-over-week
 *  delta. An absent figure is a gap ("—"). For a `sum` Metric `week`/`month` are daily
 *  averages (the window fold ÷ its day count), so steps/calories read per-day. */
export interface LedgerRow {
  metric: string;
  unit: string;
  aggregation: Aggregation | "";
  latest?: LedgerValue;
  week?: number;
  month?: number;
  delta_abs?: number;
  delta_pct?: number;
}

/** Series is the result of GET /v1/series: metadata, ordered buckets, and the
 *  Panel summary — the whole window folded into one value server-side (ADR 0019). */
export interface Series {
  metric: string;
  unit: string;
  aggregation: Aggregation | "";
  bucket: Bucket;
  source: string;
  points: Point[];
  /** summary is the Panel summary: the Metric aggregated over the whole window as a
   *  single bucket (ADR 0019). Absent is a gap ("—") — no data, or a derived Metric
   *  missing a required operand. Never re-derived client-side. */
  summary?: Point;
  /** days is the whole-day span of the window — the honest denominator for a per-day
   *  average of a `sum` summary (summary.value ÷ days), including a Baseline window of
   *  a different length. Server-provided so the client divides, never re-aggregates. */
  days?: number;
  /** mean is the window average of a `latest` Metric's readings (e.g. mean body mass),
   *  shown instead of the last reading when summaries render as period averages — the
   *  better trend view. Server-computed; absent for non-latest Metrics or an empty window. */
  mean?: number;
  /** nights is the count of Nights holding data in the window, carried only by a
   *  duration_by_state Metric (ADR 0027). It is the denominator for a per-night figure,
   *  as `days` is for a per-day one: a 30-day window over 21 recorded nights divides by
   *  21, never by 30. */
  nights?: number;
}

/** ManualMeasurement is one Manual entry: a Measurement the Account typed rather than
 *  a Connector imported (ADR 0022). Unlike a Series point it carries an `id`, because
 *  it is the only kind of Measurement Verve will delete and deleting needs an address.
 *  `value` is the canonical stored value — for a `%` Metric that is a fraction (0.27,
 *  not 27); rescaling for display is the client's job. */
export interface ManualMeasurement {
  id: number;
  metric: string;
  value: number;
  unit: string;
  measured_at: string;
  source: string;
}

// --- The Plan (ADR 0023) ---

/** BasalEquation is one published resting-expenditure equation. */
export type BasalEquation = "katch_mcardle" | "cunningham" | "mifflin_st_jeor" | "harris_benedict";

/** EstimateInput is a datum an equation needs; it names what is missing when one cannot run. */
export type EstimateInput = "lean_mass" | "mass" | "height" | "age" | "sex";

/** EstimateBasis is the evidence behind an Expenditure estimate, best first: `observed`
 *  back-computes from intake against the body-mass trend, `recorded` is what the devices
 *  claim, `predicted` is an equation times an activity factor. */
export type EstimateBasis = "observed" | "recorded" | "predicted";

/** BodyCompositionTrust is the Account's declared judgement of its lean-mass data. */
export type BodyCompositionTrust = "measured" | "estimated" | "unknown";

/** BasalEstimate is one equation's result. `kcal` absent means it cannot run, and
 *  `missing` names the inputs that would unlock it — never a value from a guessed input. */
export interface BasalEstimate {
  equation: BasalEquation;
  name: string;
  kcal?: number;
  missing?: EstimateInput[];
}

/** Expenditure is the Expenditure estimate with the evidence that produced it. The detail
 *  fields are set only for the basis in force, so the page can show the arithmetic. */
export interface Expenditure {
  kcal: number;
  basis: EstimateBasis;
  window_days: number;
  mean_intake_kcal?: number;
  mass_slope_kg_per_day?: number;
  intake_days?: number;
  mass_days?: number;
  activity_factor?: number;
  basal_kcal?: number;
}

/** MeasuredRate is the Account's measured speed of body-mass change, in the same units a
 *  Target rate uses so the two are directly comparable. */
export interface MeasuredRate {
  pct_per_week: number;
  kg_per_week: number;
  window_days: number;
  mass_days: number;
}

/** Phase is a bounded stretch pursuing one Target rate. `ended_at` absent means open. */
export interface Phase {
  id: number;
  rate_pct_per_week: number;
  started_at: string;
  ended_at?: string;
}

/** Targets is what a Target rate implies. Protein is a floor with evidence behind it;
 *  `conventional_split` marks fat and carbohydrate as a stated convention, because past a
 *  hormonal fat floor the split has no demonstrated effect at equal calories and protein. */
export interface Targets {
  kcal: number;
  kg_per_week: number;
  protein_g: number;
  fat_g: number;
  carb_g: number;
  protein_g_per_kg_lean: number;
  protein_from_body_mass?: boolean;
  conventional_split: boolean;
}

/** Adherence compares intent to outcome over the open Phase's own window. It deliberately
 *  carries no lean-mass figure: on a bioimpedance scale a cut mechanically renders
 *  lean-mass "loss" whether or not any muscle was lost. */
export interface Adherence {
  window_days: number;
  thin?: boolean;
  target_rate_pct_per_week: number;
  actual_rate_pct_per_week?: number;
  target_kcal: number;
  actual_kcal?: number;
  target_protein_g: number;
  actual_protein_g?: number;
}

/** Guardrail is one advisory warning. Verve warns and never blocks — it does not know the
 *  Account's medical context (the same rule as the uncoloured Baseline delta, ADR 0015). */
export interface Guardrail {
  code: string;
  message: string;
}

/** Plan is the whole Plan page in one payload; every figure is derived server-side. */
export interface Plan {
  basal: BasalEstimate[];
  preselected_equation?: BasalEquation;
  expenditure?: Expenditure;
  actual_rate?: MeasuredRate;
  phase?: Phase;
  rate_pct_per_week: number;
  targets?: Targets;
  adherence?: Adherence;
  guardrails: Guardrail[];
  insufficient?: boolean;
}

/** Profile is the Account data that is not a Measurement. `derived_trust` is a suggestion
 *  to show when nothing is declared, not a stored choice. */
export interface Profile {
  date_of_birth?: string;
  biological_sex?: "male" | "female";
  body_composition_trust?: BodyCompositionTrust;
  derived_trust: BodyCompositionTrust;
}

/** Annotation is a dated note the Account wrote on the time axis (ADR 0030): an
 *  illness, a trip, a change of program. It carries no value and no Metric, and it
 *  belongs to the Account rather than to a Dashboard, so the same note is read
 *  against every curve. `starts_on`/`ends_on` are the real dates the tooltip shows;
 *  `bucket`/`end_bucket` are those dates folded onto the resolved bucket grid by the
 *  server, which is where a marker is drawn. Nothing here is derived client-side:
 *  snapping a date to a bucket is `internal/timeaxis`' job, and a second
 *  implementation of it would disagree silently. */
export interface Annotation {
  id: number;
  label: string;
  body: string | null;
  starts_on: string;
  ends_on: string | null;
  /** bucket is the grid position of the first day on screen. Absent when the request
   *  carried no time axis (the Data page's full list). */
  bucket?: string;
  /** end_bucket is set only when the span covers more than one bucket, so its very
   *  presence is the "band, not marker" test. A fortnight at the month bucket is one
   *  bucket wide and has no band to draw. */
  end_bucket?: string;
}

/** Pin is a Catalog Metric the Account keeps in the sidebar (ADR 0025). It holds
 *  the slug and its place in the list, and deliberately nothing else: a Pin that
 *  carried a Time range would be a one-Panel Dashboard under another name. */
export interface Pin {
  metric: string;
  position: number;
}

/** Activity is what a Session was, as the server hands it over: the slug the
 *  Connector stored plus everything a screen needs to show it. The label, the
 *  group and the reading come from the server's curated table (ADR 0028) rather
 *  than being restated here, so an activity added there needs no client release. */
export interface Activity {
  slug: string;
  label: string;
  group: "cardio" | "strength" | "water" | "winter" | "other";
  reading: "pace" | "speed" | "none";
}

/** Session is one workout. `distance` and `energy` are the Session's own totals,
 *  which is what the device measured: they are the figures a screen shows, never
 *  anything derived from the simplified route geometry (ADR 0028). */
export interface Session {
  id: number;
  activity: Activity;
  start_at: string;
  end_at: string;
  duration: number; // seconds
  distance?: number; // km
  energy?: number; // kcal
  source: string;
  has_route: boolean;
}

/** SessionTotals describes the whole filter and names the period it covers, so a
 *  header can say what it is totalling. A total without its domain reads as a
 *  truth and is not one. */
export interface SessionTotals {
  count: number;
  duration: number;
  distance?: number;
  energy?: number;
  from: string;
  to: string;
}

export interface SessionPage {
  sessions: Session[];
  totals: SessionTotals;
  next_cursor?: string;
}

/** SessionStat is one summary figure of a workout: an aggregate of a canonical
 *  Metric over the whole session. An average and a maximum are two answers, so
 *  both arrive and both are shown. */
export interface SessionStat {
  metric: string;
  stat: "sum" | "average" | "min" | "max";
  value: number;
  unit: string;
}

export interface RouteRef {
  id: number;
  start_at: string;
  end_at: string;
  source: string;
}

export interface SessionDetail {
  session: Session;
  stats: SessionStat[];
  routes: RouteRef[];
}

/** RouteSample is one point of a route profile: a distance along the trace with
 *  what was true there. `speed` is km/h; a pace reading is rendered from it. */
export interface RouteSample {
  km: number;
  ele?: number;
  speed?: number;
}

/** RouteGeometry is one Route drawn. `length_km` is the geometry's own length and
 *  belongs to the profile axis only: what a screen reports as the workout's
 *  distance is the Session's own total (ADR 0028). */
export interface RouteGeometry {
  id: number;
  start_at: string;
  end_at: string;
  source: string;
  points: [number, number][];
  profiles: {
    samples: RouteSample[];
    length_km: number;
    ascent_m: number;
    descent_m: number;
    min_ele_m?: number;
    max_ele_m?: number;
  };
}

/** MapConfig is the basemap an instance has been configured with. Absent means
 *  none, which is the default: the workout map then draws its trace on a blank
 *  ground and the browser makes no outbound request (ADR 0028). */
export interface MapConfig {
  tiles: string;
  attribution: string;
}
