// Package applehealth is the Apple Health export Connector: it reads an Apple
// Health `export.zip` (or a bare `export.xml`) in streaming and classifies its
// data into canonical families scoped to one Account — scalar Records into
// Measurements (mapped to a Catalog Metric and normalized to its canonical
// unit), sleep/stand category Records into States, and Workouts into Sessions
// with their GPX Routes copied out as file artifacts (ADR 0004). Records whose
// type it cannot map go to the Unmapped bin, kept and never discarded (ADR 0002).
//
// The parse is a streaming xml.Decoder token loop (ADR 0006): the export is
// ~750 MB, so it must run in constant memory. High-volume families (Measurements,
// States) flush to storage in bounded batches; nothing accumulates the whole
// file. Workouts are few, so each is written on its closing tag — the point at
// which its Session id is known and its routes can be attached.
package applehealth

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/units"
)

// batchSize is how many rows accumulate before a flush. It bounds memory and
// keeps each write transaction (and the WAL) small during a large import.
const batchSize = 5000

// appleTimeLayout is Apple Health's date format, e.g. "2024-11-25 21:13:22 +0200".
const appleTimeLayout = "2006-01-02 15:04:05 -0700"

// Store is the subset of the data layer the Connector writes through. Rows are
// deduplicated per account by content key; batch inserts return a mask of which
// rows were newly added, and the single-row session/route inserts return whether
// the row was new (see internal/data).
type Store interface {
	InsertBatch(ctx context.Context, ms []data.Measurement) ([]bool, error)
	InsertUnmappedBatch(ctx context.Context, us []data.UnmappedRecord) ([]bool, error)
	InsertStateBatch(ctx context.Context, ss []data.State) ([]bool, error)
	InsertSession(ctx context.Context, s *data.Session) (bool, error)
	InsertRoute(ctx context.Context, r *data.Route) (bool, error)
	RecordImport(ctx context.Context, imp *data.Import) error
}

// Tally is one bucket's added/skipped counts within a Report — used per Metric,
// per State kind, and per activity type.
type Tally struct {
	Added   int
	Skipped int
}

// Report is the outcome of one import, suitable for a readable CLI summary.
type Report struct {
	SourceFile    string
	Added         int
	Skipped       int
	Unmapped      int // newly kept in the Unmapped bin
	PerMetric     map[string]Tally
	UnmappedTypes map[string]int // raw source type → count newly kept

	// Non-scalar families.
	StatesAdded     int
	StatesSkipped   int
	SessionsAdded   int
	SessionsSkipped int
	RoutesAdded     int
	RoutesSkipped   int
	PerState        map[string]Tally // State kind → tally
	PerActivity     map[string]Tally // Session activity type → tally
}

// Import reads the Apple Health export at path and writes it to store, scoped to
// accountID; GPX route artifacts are copied into artifactsDir (ADR 0004). A
// ".zip" path is opened as an archive and its export.xml entry streamed, with
// routes resolved to entries in the same archive; any other path is streamed
// directly as XML, with routes resolved as files beside it.
func Import(ctx context.Context, store Store, accountID int64, path, artifactsDir string) (Report, error) {
	sourceFile := filepath.Base(path)

	if strings.EqualFold(filepath.Ext(path), ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return Report{}, fmt.Errorf("applehealth: open zip %s: %w", path, err)
		}
		defer zr.Close()

		entry := findExportXML(&zr.Reader)
		if entry == nil {
			return Report{}, fmt.Errorf("applehealth: no export.xml in %s", path)
		}
		rc, err := entry.Open()
		if err != nil {
			return Report{}, fmt.Errorf("applehealth: open %s in zip: %w", entry.Name, err)
		}
		defer rc.Close()
		return importStream(ctx, store, accountID, sourceFile, rc, artifactsDir, zipRouteOpener{&zr.Reader})
	}

	f, err := os.Open(path)
	if err != nil {
		return Report{}, fmt.Errorf("applehealth: open %s: %w", path, err)
	}
	defer f.Close()
	return importStream(ctx, store, accountID, sourceFile, f, artifactsDir, dirRouteOpener{filepath.Dir(path)})
}

// findExportXML returns the archive entry for the export's XML (Apple nests it
// under apple_health_export/export.xml), or nil if absent.
func findExportXML(zr *zip.Reader) *zip.File {
	for _, f := range zr.File {
		if filepath.Base(f.Name) == "export.xml" {
			return f
		}
	}
	return nil
}

// importStream is the streaming core: it tokenizes r, routing each top-level
// Record to a Measurement, a State, or the Unmapped bin, and each Workout
// subtree to a Session with its routes, flushing the high-volume families in
// bounded batches and finally recording the Import run. It is separated from
// Import so tests can feed an in-memory reader and a fake route opener.
func importStream(ctx context.Context, store Store, accountID int64, sourceFile string, r io.Reader, artifactsDir string, opener routeOpener) (Report, error) {
	report := Report{
		SourceFile:    sourceFile,
		PerMetric:     make(map[string]Tally),
		UnmappedTypes: make(map[string]int),
		PerState:      make(map[string]Tally),
		PerActivity:   make(map[string]Tally),
	}

	dec := xml.NewDecoder(r)
	dec.Strict = false // Apple's export carries a DTD and locale text; be lenient.

	measurements := make([]data.Measurement, 0, batchSize)
	unmapped := make([]data.UnmappedRecord, 0, batchSize)
	states := make([]data.State, 0, batchSize)

	// stack tracks element nesting so we process only top-level Records (direct
	// children of HealthData). Records nested in a Correlation or Workout are
	// duplicated at top level per Apple's own note, so skipping them here avoids
	// re-processing the same reading. wb is non-nil while inside a <Workout>.
	var stack []string
	var wb *workoutBuilder

	flushMeasurements := func() error {
		if len(measurements) == 0 {
			return nil
		}
		mask, err := store.InsertBatch(ctx, measurements)
		if err != nil {
			return err
		}
		for i, added := range mask {
			c := report.PerMetric[measurements[i].Metric]
			if added {
				c.Added++
				report.Added++
			} else {
				c.Skipped++
				report.Skipped++
			}
			report.PerMetric[measurements[i].Metric] = c
		}
		measurements = measurements[:0]
		return nil
	}

	flushUnmapped := func() error {
		if len(unmapped) == 0 {
			return nil
		}
		mask, err := store.InsertUnmappedBatch(ctx, unmapped)
		if err != nil {
			return err
		}
		for i, added := range mask {
			if added {
				report.Unmapped++
				report.UnmappedTypes[unmapped[i].SourceType]++
			}
		}
		unmapped = unmapped[:0]
		return nil
	}

	flushStates := func() error {
		if len(states) == 0 {
			return nil
		}
		mask, err := store.InsertStateBatch(ctx, states)
		if err != nil {
			return err
		}
		for i, added := range mask {
			c := report.PerState[states[i].Kind]
			if added {
				c.Added++
				report.StatesAdded++
			} else {
				c.Skipped++
				report.StatesSkipped++
			}
			report.PerState[states[i].Kind] = c
		}
		states = states[:0]
		return nil
	}

	// finishWorkout writes one Session on its closing tag, then copies and
	// records each of its GPX routes. The Session id (new or pre-existing) is
	// needed to attach the routes, so this runs per workout rather than batched.
	finishWorkout := func() error {
		sess := wb.session(accountID)
		inserted, err := store.InsertSession(ctx, &sess)
		if err != nil {
			return err
		}
		c := report.PerActivity[sess.ActivityType]
		if inserted {
			c.Added++
			report.SessionsAdded++
		} else {
			c.Skipped++
			report.SessionsSkipped++
		}
		report.PerActivity[sess.ActivityType] = c

		for _, ref := range wb.routes {
			key, artifact, err := copyRouteArtifact(opener, ref.path, artifactsDir)
			if err != nil {
				return err
			}
			route := data.Route{
				AccountID:  accountID,
				SessionID:  sess.ID,
				Artifact:   artifact,
				StartAt:    ref.start,
				EndAt:      ref.end,
				Source:     ref.source,
				ContentKey: key,
			}
			rInserted, err := store.InsertRoute(ctx, &route)
			if err != nil {
				return err
			}
			if rInserted {
				report.RoutesAdded++
			} else {
				report.RoutesSkipped++
			}
		}
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Report{}, fmt.Errorf("applehealth: parse %s: %w", sourceFile, err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, t.Name.Local)

			switch {
			case t.Name.Local == "Workout" && parent == "HealthData":
				wb = newWorkoutBuilder(t.Attr)
			case wb != nil && t.Name.Local == "WorkoutStatistics":
				wb.addStatistic(t.Attr)
			case wb != nil && t.Name.Local == "WorkoutRoute":
				wb.startRoute(t.Attr)
			case wb != nil && t.Name.Local == "FileReference":
				wb.addFileRef(t.Attr)
			case t.Name.Local == "Record" && parent == "HealthData":
				attrs := parseAttrs(t.Attr)
				if kind, ok := stateKind(attrs.typ); ok {
					states = append(states, buildState(accountID, kind, attrs))
					if len(states) >= batchSize {
						if err := flushStates(); err != nil {
							return Report{}, err
						}
					}
					continue
				}
				m, u, isMeasurement := classifyRecord(accountID, attrs)
				if isMeasurement {
					measurements = append(measurements, m)
					if len(measurements) >= batchSize {
						if err := flushMeasurements(); err != nil {
							return Report{}, err
						}
					}
				} else {
					unmapped = append(unmapped, u)
					if len(unmapped) >= batchSize {
						if err := flushUnmapped(); err != nil {
							return Report{}, err
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "Workout" && wb != nil {
				if err := finishWorkout(); err != nil {
					return Report{}, err
				}
				wb = nil
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if err := flushMeasurements(); err != nil {
		return Report{}, err
	}
	if err := flushUnmapped(); err != nil {
		return Report{}, err
	}
	if err := flushStates(); err != nil {
		return Report{}, err
	}

	imp := &data.Import{
		AccountID:     accountID,
		SourceFile:    sourceFile,
		AddedCount:    report.Added,
		SkippedCount:  report.Skipped,
		UnmappedCount: report.Unmapped,
	}
	if err := store.RecordImport(ctx, imp); err != nil {
		return Report{}, fmt.Errorf("applehealth: record import: %w", err)
	}
	return report, nil
}

// recordAttrs are the Record attributes the Connector reads.
type recordAttrs struct {
	typ, unit, value, source, start, end string
}

// buildState turns a sleep/stand category Record into a State: its Apple
// category value becomes a neutral phase slug, times are normalized, and the
// dedup key covers (kind, state_value, source, start, end).
func buildState(accountID int64, kind string, r recordAttrs) data.State {
	start := normalizeTime(r.start)
	end := normalizeTime(r.end)
	value := normalizeStateValue(r.value)
	return data.State{
		AccountID:  accountID,
		Kind:       kind,
		StateValue: value,
		StartAt:    start,
		EndAt:      end,
		Source:     r.source,
		ContentKey: stateContentKey(kind, value, r.source, start, end),
	}
}

// classifyRecord turns one Record's parsed attributes into either a Measurement
// (when its type maps to a Catalog Metric and its value normalizes cleanly) or
// an Unmapped record (otherwise). The returned bool selects which of the two is
// populated. Nothing is ever dropped: an unmappable type, an unparseable value,
// or a unit that will not convert all fall back to the Unmapped bin.
func classifyRecord(accountID int64, r recordAttrs) (data.Measurement, data.UnmappedRecord, bool) {
	start := normalizeTime(r.start)
	end := normalizeTime(r.end)

	unmapped := data.UnmappedRecord{
		AccountID:  accountID,
		SourceType: r.typ,
		Value:      r.value,
		Unit:       r.unit,
		StartAt:    start,
		EndAt:      end,
		Source:     r.source,
		ContentKey: contentKey(r.typ, r.source, start, end, r.value, r.unit),
	}

	slug, ok := typeToMetric[r.typ]
	if !ok {
		return data.Measurement{}, unmapped, false
	}
	metric, ok := catalog.Lookup(slug)
	if !ok {
		return data.Measurement{}, unmapped, false
	}
	raw, err := strconv.ParseFloat(r.value, 64)
	if err != nil {
		return data.Measurement{}, unmapped, false
	}
	value, err := units.Convert(raw, r.unit, metric.Unit)
	if err != nil {
		return data.Measurement{}, unmapped, false
	}

	return data.Measurement{
		AccountID:    accountID,
		Metric:       slug,
		Value:        value,
		OriginalUnit: r.unit,
		StartAt:      start,
		EndAt:        end,
		Source:       r.source,
		ContentKey:   contentKey(slug, r.source, start, end, r.value, r.unit),
	}, data.UnmappedRecord{}, true
}

func parseAttrs(attrs []xml.Attr) recordAttrs {
	var r recordAttrs
	for _, a := range attrs {
		switch a.Name.Local {
		case "type":
			r.typ = a.Value
		case "unit":
			r.unit = a.Value
		case "value":
			r.value = a.Value
		case "sourceName":
			r.source = a.Value
		case "startDate":
			r.start = a.Value
		case "endDate":
			r.end = a.Value
		}
	}
	return r
}

// normalizeTime parses an Apple timestamp and re-renders it as RFC 3339 in UTC,
// so stored times sort and bucket cleanly regardless of the originating offset.
// An unparseable value is kept verbatim rather than dropped.
func normalizeTime(s string) string {
	t, err := time.Parse(appleTimeLayout, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}
