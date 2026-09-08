// Package applehealth is the Apple Health export Connector: it streams an
// export.zip (or bare export.xml) and classifies its data into canonical families
// scoped to one Account — Records into Measurements or the Unmapped bin (ADR 0002),
// category Records into States, Workouts into Sessions with GPX Routes as artifacts
// (ADR 0004). Streaming xml.Decoder in constant memory over a ~750 MB export (ADR
// 0006); high-volume families flush in bounded batches, Workouts write on close.
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
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/units"
)

// appleTimeLayout is Apple Health's date format, e.g. "2024-11-25 21:13:22 +0200".
const appleTimeLayout = "2006-01-02 15:04:05 -0700"

// Import reads the Apple Health export at path and writes it to store, scoped to
// accountID. A ".zip" path is opened as an archive and its export.xml entry
// streamed, with routes resolved to entries in the same archive; any other path is
// streamed directly as XML, with routes resolved as files beside it.
func Import(ctx context.Context, store connector.Store, accountID int64, path string, opts connector.Options) (connector.Report, error) {
	sourceFile := filepath.Base(path)

	if strings.EqualFold(filepath.Ext(path), ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return connector.Report{}, fmt.Errorf("applehealth: open zip %s: %w", path, err)
		}
		defer zr.Close()

		entry := findExportXML(&zr.Reader)
		if entry == nil {
			return connector.Report{}, fmt.Errorf("applehealth: no export.xml in %s", path)
		}
		rc, err := entry.Open()
		if err != nil {
			return connector.Report{}, fmt.Errorf("applehealth: open %s in zip: %w", entry.Name, err)
		}
		defer rc.Close()

		r := connector.NewProgressCounter(int64(entry.UncompressedSize64), opts.Progress).Wrap(rc)
		return importStream(ctx, store, accountID, sourceFile, r, opts, zipRouteOpener{&zr.Reader})
	}

	f, err := os.Open(path)
	if err != nil {
		return connector.Report{}, fmt.Errorf("applehealth: open %s: %w", path, err)
	}
	defer f.Close()
	return importStream(ctx, store, accountID, sourceFile, f, opts, dirRouteOpener{filepath.Dir(path)})
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

// importStream is the streaming core: it tokenizes r, routing each top-level Record
// to a Measurement, State, or the Unmapped bin and each Workout to a Session with
// its routes, flushing in bounded batches. Split from Import so tests can feed an
// in-memory reader and a fake route opener.
func importStream(ctx context.Context, store connector.Store, accountID int64, sourceFile string, r io.Reader, opts connector.Options, opener routeOpener) (connector.Report, error) {
	sink := connector.NewSink(store, accountID, connectorName, sourceFile, opts)

	dec := xml.NewDecoder(r)
	dec.Strict = false // Apple's export carries a DTD and locale text; be lenient.

	// stack tracks nesting so we process only top-level Records (children of
	// HealthData); nested ones are duplicated at top level per Apple's note. wb is
	// non-nil while inside a <Workout>.
	var stack []string
	var wb *workoutBuilder

	// finishWorkout writes one Session on its closing tag, then copies and records
	// each of its GPX routes. The Session id is needed to attach a route, so this
	// runs per workout rather than batched.
	finishWorkout := func() error {
		sess := wb.session(accountID)
		if err := sink.Session(ctx, &sess, wb.stats); err != nil {
			return err
		}

		for _, ref := range wb.routes {
			key, artifact, err := copyRouteArtifact(opener, ref.path, opts.ArtifactsDir)
			if err != nil {
				return err
			}
			if err := sink.Route(ctx, &data.Route{
				SessionID:  sess.ID,
				Artifact:   artifact,
				StartAt:    ref.start,
				EndAt:      ref.end,
				Source:     ref.source,
				ContentKey: key,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return connector.Report{}, err
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return connector.Report{}, fmt.Errorf("applehealth: parse %s: %w", sourceFile, err)
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
					if err := sink.State(ctx, buildState(accountID, kind, attrs)); err != nil {
						return connector.Report{}, err
					}
					continue
				}
				m, u, isMeasurement := classifyRecord(accountID, attrs)
				if isMeasurement {
					if err := sink.Measurement(ctx, m); err != nil {
						return connector.Report{}, err
					}
				} else {
					if err := sink.Unmapped(ctx, u); err != nil {
						return connector.Report{}, err
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "Workout" && wb != nil {
				if err := finishWorkout(); err != nil {
					return connector.Report{}, err
				}
				wb = nil
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	return sink.Close(ctx)
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
		ContentKey: data.StateContentKey(kind, value, r.source, start, end),
	}
}

// classifyRecord turns a Record into a Measurement (type maps to a Catalog Metric
// and value normalizes) or an Unmapped record otherwise; the bool selects which.
// Nothing is dropped — bad type/value/unit all fall back to the Unmapped bin.
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
		ContentKey: data.ContentKey(r.typ, r.source, start, end, r.value, r.unit),
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
		ContentKey:   data.ContentKey(slug, r.source, start, end, r.value, r.unit),
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
