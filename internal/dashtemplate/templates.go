package dashtemplate

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

// Seeded is the one template every new Account is created with (ADR 0018). The
// others are offered, never seeded (ADR 0047).
const Seeded = "overview"

//go:embed templates/*.json
var templateFS embed.FS

// roster is the embedded templates, decoded and validated once, seeded first and
// the rest by name so a picker scans rather than searches.
var roster = mustRoster()

// All returns every template in roster order. The Files are copies; a caller that
// edits one changes nothing for the next.
func All() []File {
	out := make([]File, len(roster))
	for i, f := range roster {
		out[i] = f.clone()
	}
	return out
}

// Get returns the template with this slug.
func Get(slug string) (File, bool) {
	for _, f := range roster {
		if f.Slug == slug {
			return f.clone(), true
		}
	}
	return File{}, false
}

// load decodes every embedded file, keyed by file name.
func load() (map[string]File, error) {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, fmt.Errorf("dashtemplate: read templates: %w", err)
	}
	files := make(map[string]File, len(entries))
	for _, e := range entries {
		r, err := templateFS.Open("templates/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("dashtemplate: open %s: %w", e.Name(), err)
		}
		f, err := Decode(r)
		r.Close()
		if err != nil {
			return nil, fmt.Errorf("dashtemplate: %s: %w", e.Name(), err)
		}
		files[e.Name()] = f
	}
	return files, nil
}

// mustRoster builds the roster, panicking on a file that does not decode or
// validate. templates_test.go makes that unreachable in a build that passed.
func mustRoster() []File {
	files, err := load()
	if err != nil {
		panic(err)
	}
	out := make([]File, 0, len(files))
	for name, f := range files {
		if err := Validate(f); err != nil {
			panic(fmt.Sprintf("dashtemplate: %s: %v", name, err))
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Slug == Seeded) != (out[j].Slug == Seeded) {
			return out[i].Slug == Seeded
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// clone copies a File deep enough that its Panels, Metrics and buckets are the
// caller's own.
func (f File) clone() File {
	c := f
	c.Panels = make([]Panel, len(f.Panels))
	for i, p := range f.Panels {
		c.Panels[i] = Panel{Metrics: append([]PanelMetric(nil), p.Metrics...), Width: p.Width}
		if p.Bucket != nil {
			b := *p.Bucket
			c.Panels[i].Bucket = &b
		}
	}
	return c
}
