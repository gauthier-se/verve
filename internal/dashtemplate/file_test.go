package dashtemplate

import (
	"strings"
	"testing"
)

// validFile is a Dashboard file every refusal case below is one edit away from.
const validFile = `{
  "format": "verve.dashboard/1",
  "slug": "sleep",
  "name": "Sleep",
  "description": "How long, how deep, and how the body recovered overnight.",
  "range_preset": "30d",
  "baseline_rule": "none",
  "panels": [
    { "metrics": [{ "metric": "sleep", "chart_type": "stacked_bar" }], "width": 2 },
    { "metrics": [{ "metric": "resting_heart_rate", "chart_type": "line" },
                  { "metric": "heart_rate_variability_sdnn", "chart_type": "line" }],
      "bucket": "week" }
  ]
}`

func TestAValidFileDecodesAndValidatesClean(t *testing.T) {
	f, err := Decode(strings.NewReader(validFile))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := Validate(f); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if f.Name != "Sleep" || f.Slug != "sleep" || f.RangePreset != "30d" || f.BaselineRule != "none" {
		t.Errorf("header = %+v", f)
	}
	if len(f.Panels) != 2 {
		t.Fatalf("panels = %d, want 2", len(f.Panels))
	}
	if f.Panels[0].Width != 2 || f.Panels[0].Bucket != nil {
		t.Errorf("panel 0 = %+v, want width 2 and no bucket", f.Panels[0])
	}
	// width is absent from the second Panel and defaults to one column.
	if f.Panels[1].Width != 1 || f.Panels[1].Bucket == nil || *f.Panels[1].Bucket != "week" {
		t.Errorf("panel 1 = %+v, want width 1 and bucket week", f.Panels[1])
	}
	want := []PanelMetric{{"resting_heart_rate", "line"}, {"heart_rate_variability_sdnn", "line"}}
	if got := f.Panels[1].Metrics; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("panel 1 metrics = %+v, want %+v", got, want)
	}
}

func TestDecodeRefusesAnUnknownField(t *testing.T) {
	withGoal := strings.Replace(validFile, `"width": 2 }`, `"width": 2, "goal": 420 }`, 1)
	if _, err := Decode(strings.NewReader(withGoal)); err == nil {
		t.Fatal("Decode accepted a Panel carrying an unknown field")
	}
}

// TestValidateRefuses holds one fault per case, each the valid file with one edit,
// and checks the fault is reported at its place in the file.
func TestValidateRefuses(t *testing.T) {
	cases := []struct {
		name, old, new, field string
	}{
		{"an unknown format", `"verve.dashboard/1"`, `"verve.dashboard/2"`, "format"},
		{"an unknown Metric", `"metric": "sleep"`, `"metric": "sleepiness"`, "panels[0].metrics"},
		{"a missing chart type", `"chart_type": "stacked_bar"`, `"chart_type": ""`, "panels[0].chart_type"},
		{"a band on a sum Metric", `{ "metric": "sleep", "chart_type": "stacked_bar" }`,
			`{ "metric": "steps", "chart_type": "band" }`, "panels[0].chart_type"},
		{"a stacked bar without a breakdown", `{ "metric": "resting_heart_rate", "chart_type": "line" }`,
			`{ "metric": "resting_heart_rate", "chart_type": "stacked_bar" }`, "panels[1].chart_type"},
		{"a diverging bar on an unsigned Metric", `{ "metric": "resting_heart_rate", "chart_type": "line" }`,
			`{ "metric": "resting_heart_rate", "chart_type": "diverging_bar" }`, "panels[1].chart_type"},
		{"five Metrics on a Panel", `{ "metric": "resting_heart_rate", "chart_type": "line" },`,
			strings.Repeat(`{ "metric": "resting_heart_rate", "chart_type": "line" },`, 4), "panels[1].metrics"},
		{"three units on a Panel", `{ "metric": "resting_heart_rate", "chart_type": "line" },`,
			`{ "metric": "resting_heart_rate", "chart_type": "line" }, { "metric": "steps", "chart_type": "bar" },`,
			"panels[1].metrics"},
		{"an unknown bucket", `"bucket": "week"`, `"bucket": "fortnight"`, "panels[1].bucket"},
		{"an empty name", `"name": "Sleep"`, `"name": ""`, "name"},
		{"a name past the cap", `"name": "Sleep"`, `"name": "` + strings.Repeat("z", 121) + `"`, "name"},
		{"no Panel", validFile[strings.Index(validFile, `"panels"`):], `"panels": [] }`, "panels"},
		{"thirteen Panels", `"panels": [`,
			`"panels": [` + strings.Repeat(`{ "metrics": [{ "metric": "steps", "chart_type": "bar" }] },`, 12), "panels"},
		{"an unknown range", `"range_preset": "30d"`, `"range_preset": "2w"`, "range_preset"},
		{"a custom range", `"range_preset": "30d"`, `"range_preset": "custom"`, "range_preset"},
		{"a custom baseline", `"baseline_rule": "none"`, `"baseline_rule": "custom"`, "baseline_rule"},
		{"a width of four", `"width": 2`, `"width": 4`, "panels[0].width"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edited := strings.Replace(validFile, tc.old, tc.new, 1)
			if edited == validFile {
				t.Fatalf("the edit %q did not apply", tc.old)
			}
			f, err := Decode(strings.NewReader(edited))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			err = Validate(f)
			inv, ok := err.(Invalid)
			if !ok {
				t.Fatalf("Validate = %v, want an Invalid", err)
			}
			if _, has := inv[tc.field]; !has {
				t.Errorf("Validate = %v, want a fault on %q", inv, tc.field)
			}
		})
	}
}
