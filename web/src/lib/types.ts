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

/** Panel is one card in a Dashboard. */
export interface Panel {
  id: number;
  metric: string;
  chart_type: ChartType;
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

/** Series is the result of GET /v1/series: metadata plus ordered buckets. */
export interface Series {
  metric: string;
  unit: string;
  aggregation: Aggregation | "";
  bucket: Bucket;
  source: string;
  points: Point[];
}
