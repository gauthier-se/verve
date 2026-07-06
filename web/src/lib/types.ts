// Shared shapes mirroring the Go JSON API (the versioned contract, ADR 0005).

/** Aggregation is a Metric's Catalog rule for collapsing points into a bucket. */
export type Aggregation = "sum" | "average" | "latest" | "duration_by_state";

/** ChartType is how a Panel renders its Metric. */
export type ChartType = "bar" | "line" | "area" | "band" | "stacked_bar";

/** Bucket is a Panel's time granularity; null means auto-derive from the span. */
export type Bucket = "day" | "week" | "month";

/** RangePreset is a Dashboard's Time-range choice (custom uses from/to). */
export type RangePreset = "7d" | "30d" | "3m" | "1y" | "all" | "custom";

/** Metric is one Catalog entry from GET /v1/metrics. */
export interface Metric {
  slug: string;
  unit: string;
  aggregation: Aggregation;
  nature: "imported" | "derived";
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

/** Dashboard is a named grid of Panels carrying the active Time range. */
export interface Dashboard {
  id: number;
  name: string;
  position: number;
  range_preset: RangePreset;
  range_from: string | null;
  range_to: string | null;
  panels: Panel[];
}

/** Point is one aggregated bucket; min/max carry the band for average Metrics. */
export interface Point {
  bucket: string;
  value: number;
  min?: number;
  max?: number;
}

/** Series is the result of GET /v1/series: metadata plus ordered buckets. */
export interface Series {
  metric: string;
  unit: string;
  aggregation: Aggregation;
  bucket: Bucket;
  source: string;
  points: Point[];
}
