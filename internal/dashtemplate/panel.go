package dashtemplate

import (
	"encoding/json"
	"fmt"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// A Panel carries one to four Metrics spanning at most two canonical units, two
// Y axes, so every curve keeps its true scale (ADR 0020). The metric cap also
// bounds a /v1/series request, which serves at most one Panel's worth.
const (
	MaxPanelMetrics = 4
	MaxPanelUnits   = 2
)

// UnknownMetricMsg is the single client-facing message for a slug outside the
// Catalog, here because a Panel is where most of them are refused.
const UnknownMetricMsg = "unknown metric — see GET /v1/metrics"

// chartTypes is the closed set a Panel may take.
var chartTypes = map[string]bool{
	"bar": true, "line": true, "area": true, "band": true, "stacked_bar": true, "diverging_bar": true,
}

// MetricInput is one entry of a Panel's metrics list as a client sends it; a nil
// chart type means "default from the Metric's aggregation rule".
type MetricInput struct {
	Metric    string  `json:"metric"`
	ChartType *string `json:"chart_type"`
}

// ValidatePanelMetrics resolves a Panel's metric list against the Catalog
// (ADR 0020): 1 to 4 entries spanning at most two canonical units, each chart
// type compatible with its Metric and defaulted from its aggregation rule when
// omitted. field is the key errors attach to; chart-type faults attach to
// "chart_type".
func ValidatePanelMetrics(v Invalid, field string, entries []MetricInput) []PanelMetric {
	if len(entries) == 0 {
		v.add(field, "must be provided")
		return nil
	}
	v.check(len(entries) <= MaxPanelMetrics, field,
		fmt.Sprintf("a panel carries at most %d metrics", MaxPanelMetrics))

	units := make(map[string]bool)
	metrics := make([]PanelMetric, 0, len(entries))
	for _, e := range entries {
		if e.Metric == "" {
			v.add(field, "must be provided")
			continue
		}
		m, known := catalog.Lookup(e.Metric)
		if !known {
			v.add(field, UnknownMetricMsg)
			continue
		}
		units[m.Unit] = true
		chartType := DefaultChartType(m)
		if e.ChartType != nil {
			chartType = *e.ChartType
		}
		ValidateChartType(v, chartType, m)
		metrics = append(metrics, PanelMetric{Metric: e.Metric, ChartType: chartType})
	}
	v.check(len(units) <= MaxPanelUnits, field,
		fmt.Sprintf("a panel spans at most %d units — metrics sharing a unit share an axis", MaxPanelUnits))
	return metrics
}

// DefaultChartType is the chart a Metric gets when a Panel specifies none: signed
// derived → diverging bar (ADR 0014); else by aggregation: sum→bar, average→band,
// duration_by_state→stacked bar, latest (and unsigned derived)→line.
func DefaultChartType(m catalog.Metric) string {
	if m.Signed {
		return "diverging_bar"
	}
	switch m.Aggregation {
	case catalog.Sum:
		return "bar"
	case catalog.Average:
		return "band"
	case catalog.DurationByState, catalog.SumByState:
		return "stacked_bar"
	default: // Latest, and unsigned derived Metrics
		return "line"
	}
}

// ValidateChartType checks a chart type is known and compatible: band→average,
// stacked_bar→duration_by_state, diverging_bar→signed; bar/line/area suit any Metric.
func ValidateChartType(v Invalid, chartType string, m catalog.Metric) {
	if !chartTypes[chartType] {
		v.add("chart_type", "must be one of bar, line, area, band, stacked_bar, diverging_bar")
		return
	}
	switch chartType {
	case "band":
		v.check(m.Aggregation == catalog.Average, "chart_type", "the band variant is only for average metrics")
	case "stacked_bar":
		v.check(m.Aggregation.ByState(), "chart_type", "the stacked-bar variant is only for metrics with a breakdown")
	case "diverging_bar":
		v.check(m.Signed, "chart_type", "the diverging-bar variant is only for signed metrics")
	}
}

// ParseBucketOverride resolves a present bucket field on a panel update: the
// literal null clears the override (auto-derive), a JSON string is validated as
// day/week/month, and anything else is a validation error.
func ParseBucketOverride(raw json.RawMessage, v Invalid) *string {
	if string(raw) == "null" {
		return nil
	}
	var b string
	if err := json.Unmarshal(raw, &b); err != nil {
		v.add("bucket", "must be a string (day, week, month) or null")
		return nil
	}
	return ValidateBucket(v, &b)
}

// ValidateBucket resolves an optional bucket override: nil (or explicit null)
// means auto-derive; a value must be a known bucket (timeaxis.ParseBucket, the
// single bucket vocabulary shared with the read path).
func ValidateBucket(v Invalid, raw *string) *string {
	if raw == nil {
		return nil
	}
	if _, err := timeaxis.ParseBucket(*raw); err != nil {
		v.add("bucket", "must be day, week, or month, or omitted to auto-derive")
		return nil
	}
	return raw
}

// ValidateWidth checks a Panel's column span is 1, 2, or 3.
func ValidateWidth(v Invalid, width int) {
	v.check(width >= 1 && width <= 3, "width", "must be between 1 and 3")
}
