package api

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/history"
	"github.com/gauthier-se/verve/internal/query"
)

// The JSON API is a contract written twice: once as struct tags in Go, once as
// interfaces in web/src/lib/types.ts. Nothing compiled the two against each
// other, and the file holding the second copy is the most-changed file in the
// repository, which is exactly the shape of a contract kept in step by hand.
//
// Drift here does not fail loudly. A renamed field arrives as undefined and
// renders as an empty cell or a blank chart; a field the client still declares is
// one it reads and never receives. Both look like a bug in a component.
//
// This reads types.ts rather than generating it, because 170 of its 640 lines are
// prose: what a field means, which ADR decided it, why a bound is nullable. A
// generator would either lose that or push it into Go comments where it does not
// belong, and what drifts is a field set, not a paragraph. So the prose stays
// hand-written and the field set is pinned.
const typesTSPath = "../../web/src/lib/types.ts"

// contract pairs a Go payload type with the TypeScript interface that must
// describe it. Curated rather than exhaustive: it covers what crosses the wire on
// the endpoints the SPA reads, which is where drift costs something. Adding a
// payload type is one line.
var contract = []struct {
	name string // the exported TypeScript interface
	got  any    // a zero value of the Go type serialized into it
}{
	{"Dashboard", dashboardView{}},
	{"Panel", panelView{}},
	{"PanelMetric", panelMetricView{}},
	{"Metric", metricView{}},
	{"Formula", formulaView{}},
	{"Term", termView{}},
	{"Account", accountView{}},
	{"Profile", profileView{}},
	{"Annotation", annotationView{}},
	{"Exclusion", exclusionView{}},
	{"Pin", pinView{}},
	{"MapConfig", mapView{}},
	{"Window", windowView{}},
	{"TimeAxis", timeAxisView{}},
	{"Session", sessionView{}},
	{"SessionTotals", sessionTotalsView{}},
	{"SessionStat", statView{}},
	{"RouteRef", routeRefView{}},
	{"ImportJob", importJobView{}},
	{"ImportReport", reportView{}},
	{"BasalEstimate", basalView{}},
	{"Phase", phaseView{}},
	{"Plan", planView{}},
	{"CoVary", coVaryView{}},
	{"SkippedMetric", coVarySkippedView{}},

	// Types the read modules own and the API serves unwrapped: they carry their
	// own json tags and go straight onto the wire (ADR 0037).
	{"Point", query.Point{}},
	{"Series", query.Series{}},
	{"LedgerRow", query.LedgerRow{}},
	{"LedgerValue", query.LedgerValue{}},
	{"HistorySpan", query.Span{}},
	{"HistoryPhase", history.Phase{}},
	{"HistoryFigure", history.Figure{}},
	{"HistoryEvent", history.Event{}},
	{"History", history.Read{}},
	{"Pair", query.Pair{}},
	{"Scatter", query.Scatter{}},
	{"ScatterDot", query.ScatterDot{}},
	{"ScatterFit", query.ScatterFit{}},
}

// tsField is one declared field of a TypeScript interface.
type tsField struct {
	optional bool // declared `name?: T`
	nullable bool // the type includes `| null`
}

var (
	// An interface body: everything between `export interface Name {` and the
	// closing brace at column zero.
	tsInterfaceRe = regexp.MustCompile(`(?ms)^export interface (\w+) \{\n(.*?)^\}`)
	// One field line: an optional `?`, the type up to the semicolon, and the
	// trailing line comment several fields carry ("duration: number; // seconds").
	tsFieldRe = regexp.MustCompile(`^\s*(\w+)(\??):\s*(.+?);\s*(?://.*)?$`)
	// A doc comment or a blank line inside an interface body.
	tsNoiseRe = regexp.MustCompile(`^\s*(/\*|\*|//|$)`)
)

// parseTypes reads the declared interfaces out of types.ts.
func parseTypes(t *testing.T) map[string]map[string]tsField {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(typesTSPath))
	if err != nil {
		t.Fatalf("read %s: %v", typesTSPath, err)
	}

	out := map[string]map[string]tsField{}
	for _, m := range tsInterfaceRe.FindAllStringSubmatch(string(raw), -1) {
		name, body := m[1], m[2]
		fields := map[string]tsField{}
		for _, line := range strings.Split(body, "\n") {
			if tsNoiseRe.MatchString(line) {
				continue
			}
			f := tsFieldRe.FindStringSubmatch(line)
			if f == nil {
				continue // an index signature or a shape this pin does not model
			}
			fields[f[1]] = tsField{
				optional: f[2] == "?",
				nullable: strings.Contains(f[3], "null"),
			}
		}
		out[name] = fields
	}
	if len(out) == 0 {
		t.Fatalf("%s parsed to no interfaces: has the file moved or been reformatted?", typesTSPath)
	}
	return out
}

// goFields walks a struct's json tags, following embedded structs the way
// encoding/json does, and reports each wire field and whether it is omitempty.
func goFields(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("json")
		name, opts, _ := strings.Cut(tag, ",")

		// An embedded struct with no tag is promoted: its fields are the parent's.
		if f.Anonymous && (!ok || name == "") {
			inner := f.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				for k, v := range goFields(t, inner) {
					out[k] = v
				}
				continue
			}
		}

		if !ok || name == "-" {
			continue // never serialized, so it is not part of the contract
		}
		if name == "" {
			name = f.Name
		}
		out[name] = strings.Contains(opts, "omitempty")
	}
	return out
}

// TestWireContractMatchesTypeScript is the pin: every field Go serializes is
// declared in TypeScript, and every field TypeScript declares is one Go sends.
func TestWireContractMatchesTypeScript(t *testing.T) {
	declared := parseTypes(t)

	for _, c := range contract {
		t.Run(c.name, func(t *testing.T) {
			ts, ok := declared[c.name]
			if !ok {
				t.Fatalf("types.ts declares no interface %s", c.name)
			}
			goSide := goFields(t, reflect.TypeOf(c.got))

			for field := range goSide {
				if _, ok := ts[field]; !ok {
					t.Errorf("Go sends %q; %s does not declare it, so the client reads undefined", field, c.name)
				}
			}
			for field := range ts {
				if _, ok := goSide[field]; !ok {
					t.Errorf("%s declares %q; Go never sends it, so the client reads a key that is never set", c.name, field)
				}
			}
		})
	}
}

// TestOmitemptyFieldsAreOptional: a field Go may leave out has to be reachable as
// absent on the other side. Declared required, a client reads it as always
// present and finds out otherwise on the first payload that omits it.
func TestOmitemptyFieldsAreOptional(t *testing.T) {
	declared := parseTypes(t)

	for _, c := range contract {
		t.Run(c.name, func(t *testing.T) {
			ts, ok := declared[c.name]
			if !ok {
				t.Skip("covered by TestWireContractMatchesTypeScript")
			}
			for field, omitempty := range goFields(t, reflect.TypeOf(c.got)) {
				f, ok := ts[field]
				if !ok {
					continue // reported by the test above
				}
				if omitempty && !f.optional && !f.nullable {
					t.Errorf("%s.%s is omitempty in Go but required in TypeScript", c.name, field)
				}
			}
		})
	}
}
