package dashtemplate

import (
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
