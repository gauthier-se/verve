// Package dashtemplate owns the Dashboard file: the declarative, versioned shape
// of one Dashboard's arrangement, and the rules any arrangement must satisfy,
// whether an Account builds it by hand over HTTP or Verve ships it as a Dashboard
// template (ADR 0047). One validator for both, so a template the API would refuse
// fails the build instead of reaching an Account.
package dashtemplate

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// Format is the one Dashboard file version this build reads and writes.
const Format = "verve.dashboard/1"

// MaxNameLen bounds a Dashboard name so a single field can't grow unbounded.
const MaxNameLen = 120

// maxFilePanels bounds a file's Panels. A Dashboard built by hand has no cap; a
// file is read whole into one transaction and shown whole in a picker, so it
// gets one, well past any board worth sharing.
const maxFilePanels = 12

// PanelMetric is one Metric on a Panel with the chart it is drawn as (ADR 0020).
type PanelMetric struct {
	Metric    string `json:"metric"`
	ChartType string `json:"chart_type"`
}

// Panel is one card of a Dashboard file. Bucket is nil to auto-derive from the
// span; Width is the column span, one when the file omits it.
type Panel struct {
	Metrics []PanelMetric `json:"metrics"`
	Bucket  *string       `json:"bucket,omitempty"`
	Width   int           `json:"width,omitempty"`
}

// File is a Dashboard's arrangement: its name, its time-axis tokens and its
// ordered Panels, and never its data (that is an Archive's, ADR 0039). Slug and
// Description belong to a template and are optional in the format.
type File struct {
	Format       string  `json:"format"`
	Slug         string  `json:"slug,omitempty"`
	Name         string  `json:"name"`
	Description  string  `json:"description,omitempty"`
	RangePreset  string  `json:"range_preset"`
	BaselineRule string  `json:"baseline_rule"`
	Panels       []Panel `json:"panels"`
}

// Metrics returns the distinct Metrics the file draws, in first-seen order.
func (f File) Metrics() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range f.Panels {
		for _, m := range p.Metrics {
			if !seen[m.Metric] {
				seen[m.Metric] = true
				out = append(out, m.Metric)
			}
		}
	}
	return out
}

// Decode reads one Dashboard file strictly: an unknown field is an error rather
// than something silently dropped, since a file naming a field this build does
// not know was written for a different arrangement than the one it would get.
// Decode checks the JSON only; Validate checks the arrangement.
func Decode(r io.Reader) (File, error) {
	var f File
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("dashtemplate: decode: %w", err)
	}
	for i := range f.Panels {
		if f.Panels[i].Width == 0 {
			f.Panels[i].Width = 1
		}
	}
	return f, nil
}

// Invalid maps the place of each fault to its message, the same shape as
// timeaxis.Invalid, so the API folds either into its Validator unchanged.
type Invalid map[string]string

func (v Invalid) Error() string {
	for f, m := range v {
		return f + ": " + m
	}
	return "invalid"
}

// add records message for key, keeping the first message if key repeats, as the
// API's Validator does.
func (v Invalid) add(key, message string) {
	if _, exists := v[key]; !exists {
		v[key] = message
	}
}

// check records message for key unless ok is true.
func (v Invalid) check(ok bool, key, message string) {
	if !ok {
		v.add(key, message)
	}
}

// Validate checks a decoded file against the rules a Dashboard saved over HTTP
// must satisfy, returning an Invalid keyed by the place of each fault, or nil.
func Validate(f File) error {
	v := Invalid{}
	v.check(f.Format == Format, "format", "must be "+Format)
	ValidateName(v, f.Name)
	validateTimeAxis(v, f)
	v.check(len(f.Panels) >= 1, "panels", "must hold at least one panel")
	v.check(len(f.Panels) <= maxFilePanels, "panels", fmt.Sprintf("must hold at most %d panels", maxFilePanels))
	for i, p := range f.Panels {
		validatePanel(v, fmt.Sprintf("panels[%d].", i), p)
	}
	if len(v) == 0 {
		return nil
	}
	return v
}

// ValidateName checks a Dashboard name is present and within the length cap.
func ValidateName(v Invalid, name string) {
	v.check(name != "", "name", "must be provided")
	v.check(len(name) <= MaxNameLen, "name", fmt.Sprintf("must be at most %d characters", MaxNameLen))
}

// validateTimeAxis checks the Dashboard's range and Baseline tokens through
// timeaxis, and refuses the custom ones outright: their absolute dates belong to
// the Account that chose them, not to an arrangement another Account receives.
func validateTimeAxis(v Invalid, f File) {
	v.check(f.RangePreset != "custom", "range_preset", "a Dashboard file cannot carry a custom range")
	v.check(f.BaselineRule != "custom", "baseline_rule", "a Dashboard file cannot carry a custom baseline")
	if inv, ok := timeaxis.Validate(timeaxis.Tokens{
		RangePreset: f.RangePreset, BaselineRule: f.BaselineRule,
	}).(timeaxis.Invalid); ok {
		for key, msg := range inv {
			v.add(key, msg)
		}
	}
}

// validatePanel runs one Panel of a file through the same rules a Panel saved
// over HTTP meets, then reports each fault at the Panel's place in the file.
func validatePanel(v Invalid, prefix string, p Panel) {
	pv := Invalid{}
	entries := make([]MetricInput, len(p.Metrics))
	for i, m := range p.Metrics {
		entries[i] = MetricInput{Metric: m.Metric, ChartType: &m.ChartType}
	}
	ValidatePanelMetrics(pv, "metrics", entries)
	ValidateBucket(pv, p.Bucket)
	ValidateWidth(pv, p.Width)
	for key, msg := range pv {
		v.add(prefix+key, msg)
	}
}
