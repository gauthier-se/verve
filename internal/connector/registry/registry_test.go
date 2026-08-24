package registry

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/connector"
)

// writeZip writes a .zip holding one entry per name, with trivial contents, and
// returns its path.
func writeZip(t *testing.T, name string, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for entry, body := range entries {
		w, err := zw.Create(entry)
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

// Every export Verve reads is a .zip, so detection is by content: an archive is
// routed by what is inside it and never by its name (ADR 0009).
func TestForRoutesByContent(t *testing.T) {
	apple := writeZip(t, "export.zip", map[string]string{
		"apple_health_export/export.xml": `<?xml version="1.0"?><!DOCTYPE HealthData><HealthData/>`,
	})
	google := writeZip(t, "export.zip", map[string]string{
		"Takeout/Google Health/Physical Activity_GoogleData/steps_2026-08-01.csv": "timestamp,steps,data source\n",
	})

	for _, tc := range []struct {
		name, path, want string
	}{
		{"apple archive", apple, "applehealth"},
		{"google takeout", google, "googlehealth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := For(tc.path)
			if err != nil {
				t.Fatalf("For: %v", err)
			}
			if c.Name() != tc.want {
				t.Errorf("connector = %q, want %q", c.Name(), tc.want)
			}
		})
	}
}

// A bare export.xml is still an Apple export: the Connector predates the archive
// and the CLI has always taken either.
func TestForAcceptsBareExportXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.xml")
	body := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<!DOCTYPE HealthData [<!ELEMENT HealthData (Record*)>]>` + "\n<HealthData locale=\"fr_FR\"></HealthData>"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := For(path)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if c.Name() != "applehealth" {
		t.Errorf("connector = %q, want applehealth", c.Name())
	}
}

// An archive nobody recognizes, a missing file and a directory all reach the same
// sentinel rather than a panic or a path-shaped message.
func TestForRefusesWhatItDoesNotKnow(t *testing.T) {
	stranger := writeZip(t, "holiday.zip", map[string]string{"photos/beach.jpg": "not health data"})
	dir := t.TempDir()
	missing := filepath.Join(dir, "nothing-here.zip")

	for _, tc := range []struct{ name, path string }{
		{"unknown archive", stranger},
		{"missing file", missing},
		{"directory", dir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := For(tc.path); !errors.Is(err, connector.ErrUnknownExport) {
				t.Errorf("err = %v, want ErrUnknownExport", err)
			}
		})
	}
}

// Two Connectors must never both accept one file: the first match wins, so an
// overlap would make routing depend on registration order.
func TestDetectorsAreDisjoint(t *testing.T) {
	apple := writeZip(t, "export.zip", map[string]string{
		"apple_health_export/export.xml": `<HealthData/>`,
	})
	google := writeZip(t, "takeout.zip", map[string]string{
		"Takeout/Google Health/Physical Activity_GoogleData/steps.csv": "timestamp,steps,data source\n",
	})

	for _, path := range []string{apple, google} {
		accepted := []string{}
		for _, c := range All() {
			if c.Accepts(path) {
				accepted = append(accepted, c.Name())
			}
		}
		if len(accepted) != 1 {
			t.Errorf("%s accepted by %s, want exactly one", filepath.Base(path), strings.Join(accepted, "+"))
		}
	}
}
