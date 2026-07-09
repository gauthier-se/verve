// Shared shapes mirroring the Go JSON API (the versioned contract, ADR 0005).

/** Aggregation is a Metric's Catalog rule for collapsing points into a bucket. */
export type Aggregation = "sum" | "average" | "latest" | "duration_by_state";

/** ChartType is how a Panel renders its Metric. The diverging-bar variant is the
 *  signed-Metric default: bars from a zero baseline, colored by sign (ADR 0014). */
export type ChartType = "bar" | "line" | "area" | "band" | "stacked_bar" | "diverging_bar";

/** Bucket is a Panel's time granularity; null means auto-derive from the span. */
export type Bucket = "day" | "week" | "month";

/** RangePreset is a Dashboard's Time-range choice (custom uses from/to). */
export type RangePreset = "7d" | "30d" | "3m" | "1y" | "all" | "custom";

/** BaselineRule is how a Dashboard derives its comparison Baseline window from
 *  the current Time range (ADR 0015). `none` is comparison off; the two relative
 *  rules are recomputed server-side; `custom` carries absolute from/to bounds. */
export type BaselineRule = "none" | "previous" | "same_period_last_year" | "custom";

/** Term is one Formula operand: a Catalog slug weighted by a coefficient. */
export interface Term {
  metric: string;
  coefficient: number;
}

/** Formula is a derived Metric's definition: a ratio of two weighted sums times a
 *  constant scale — (scale · Σ numerator) / (Σ denominator), an absent/empty
 *  denominator meaning 1 (ADR 0014). Present only on derived Metrics. */
export interface Formula {
  scale: number;
  numerator: Term[];
  denominator?: Term[];
}

/** Metric is one Catalog entry from GET /v1/metrics. An imported Metric carries an
 *  aggregation rule; a derived Metric instead carries a Formula and a signed flag
 *  and reports no aggregation (ADR 0014). */
export interface Metric {
  slug: string;
  unit: string;
  aggregation?: Aggregation;
  nature: "imported" | "derived";
  signed?: boolean;
  formula?: Formula;
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

/** Dashboard is a named grid of Panels carrying the active Time range and, when
 *  period comparison is on, a Baseline (ADR 0015). The baseline bounds mirror the
 *  range bounds: present only for the `custom` rule. */
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

/** Point is one aggregated bucket; min/max carry the band for average Metrics. A
 *  baseline point may instead be a dated gap (`gap: true`, no value) where the
 *  Baseline window has no data at that ordinal position (ADR 0015). */
export interface Point {
  bucket: string;
  value: number;
  min?: number;
  max?: number;
  gap?: boolean;
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
