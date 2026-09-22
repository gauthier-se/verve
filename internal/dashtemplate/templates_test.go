package dashtemplate

import (
	"fmt"
	"testing"
)

// TestEveryTemplateIsAValidFile is the test a contributed template must pass: it
// decodes, it satisfies the rules a Dashboard saved over HTTP meets, its slug is
// unique and names its file (ADR 0047).
func TestEveryTemplateIsAValidFile(t *testing.T) {
	files, err := load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no template is embedded")
	}
	seen := map[string]bool{}
	for name, f := range files {
		if err := Validate(f); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if f.Slug+".json" != name {
			t.Errorf("%s: slug %q does not name its file", name, f.Slug)
		}
		if seen[f.Slug] {
			t.Errorf("%s: slug %q is taken twice", name, f.Slug)
		}
		seen[f.Slug] = true
	}
}

func TestTheSeededTemplateIsTheOverviewAndComesFirst(t *testing.T) {
	all := All()
	if len(all) == 0 || all[0].Slug != Seeded {
		t.Fatalf("All()[0] = %+v, want the %q template first", all, Seeded)
	}
	f, ok := Get(Seeded)
	if !ok || f.Name != "Overview" {
		t.Fatalf("Get(%q) = %+v, %v", Seeded, f, ok)
	}
	if _, ok := Get("no-such-template"); ok {
		t.Error("Get found a template that does not exist")
	}
}

// TestNoTemplateRepeatsItself holds what the validator does not: a Metric named
// twice on one Panel, or two identical Panels, is a template mistake even when
// every rule is met.
func TestNoTemplateRepeatsItself(t *testing.T) {
	for _, f := range All() {
		panels := map[string]bool{}
		for i, p := range f.Panels {
			metrics := map[string]bool{}
			bucket := "auto"
			if p.Bucket != nil {
				bucket = *p.Bucket
			}
			key := fmt.Sprint(p.Metrics, bucket)
			for _, m := range p.Metrics {
				if metrics[m.Metric] {
					t.Errorf("%s: panel %d names %s twice", f.Slug, i, m.Metric)
				}
				metrics[m.Metric] = true
			}
			if panels[key] {
				t.Errorf("%s: panel %d repeats an earlier one", f.Slug, i)
			}
			panels[key] = true
		}
	}
}

// TestAWidePanelOnlyLeads holds the one arrangement that leaves no hole on the
// auto-fit grid, whatever its column count, one to three (web/src/components/dashboard-grid.tsx):
// at most one Panel wider than a column, and it comes first, so every row after it
// is filled by single-column Panels and only the last row can be short.
func TestAWidePanelOnlyLeads(t *testing.T) {
	for _, f := range All() {
		for i, p := range f.Panels {
			if p.Width > 1 && i > 0 {
				t.Errorf("%s: panel %d is %d columns wide; only the first Panel may be", f.Slug, i, p.Width)
			}
		}
	}
}
