// Package archive is the Verve Archive: the file one Account's canonical data
// travels in, and the writer that streams it (ADR 0039).
//
// An Archive is a transfer, not a read. It hands over the rows as they are
// stored, with the timestamps they were stored with, which is why it is not
// bound by the day-resolution cap the read API deliberately keeps (ADR 0012):
// there is no axis here to serve. What it is bound by is the canonical model, so
// the file is readable by anything that can read JSON, and by Verve itself
// through the connector that reads it back.
//
// The format lives in one package, separate from both the CLI that writes a file
// and the handler that streams a response, because it has two callers today and
// will have more. What it reads from is the Source interface below, so the format
// is testable without a database and internal/data never learns about zip.
package archive

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// Schema is the Archive format version, written into every manifest and checked
// on the way back in. A reader that meets a version it does not know must refuse
// the file rather than read the fields it happens to recognize: the alternative
// is dropping, in silence, whatever the newer version added.
const Schema = 1

// pageSize is how many rows are read and encoded at a time. Peak memory is one
// page per family, so an Account with millions of Measurements exports on the
// same hardware that imported it. It matches connector.BatchSize, which is the
// same bound on the way in, but the two are independent constants: this one is
// not a write batch and has no transaction to keep small.
const pageSize = 5000

// Entry names. They are part of the format: a reader addresses them by name, and
// a person reads them with `unzip -p`.
const (
	ManifestName     = "manifest.json"
	MeasurementsName = "measurements.ndjson"
	StatesName       = "states.ndjson"
	SessionsName     = "sessions.ndjson"
	UnmappedName     = "unmapped.ndjson"
	ImportsName      = "imports.ndjson"
	ArtifactsDir     = "artifacts/"
)

// Manifest is what an Archive says about itself, and the first entry in the zip.
//
// It carries no email and no account id: an Archive identifies data, not a
// login. It does carry the three profile fields, because the basal estimates
// read them (ADR 0023) and a transfer that dropped them would quietly change a
// number on the other side.
type Manifest struct {
	// VerveArchive is the marker a Connector's detection looks for. It is a
	// separate field from Schema so that recognizing the file and understanding
	// its version stay two questions: an Archive from a future Verve is still
	// recognizably an Archive, and is refused as one rather than ignored.
	VerveArchive int      `json:"verve_archive"`
	Schema       int      `json:"schema"`
	VerveVersion string   `json:"verve_version"`
	ExportedAt   string   `json:"exported_at"`
	Profile      *Profile `json:"profile,omitempty"`
	Counts       Counts   `json:"counts"`
}

// Profile is the Account's static attributes: Apple's `Me` fields, stored on the
// account row. Carried, and applied by nobody on the way back in (see the
// connector): a Connector writes families, not the Account it writes for.
type Profile struct {
	DateOfBirth   string `json:"date_of_birth,omitempty"`
	BiologicalSex string `json:"biological_sex,omitempty"`
	BloodType     string `json:"blood_type,omitempty"`
}

// Counts is how many rows each entry holds, stated before the rows so a reader
// can check what it got against what it was promised. A zip can be intact and
// still be short.
type Counts struct {
	Measurements int64 `json:"measurements"`
	States       int64 `json:"states"`
	Sessions     int64 `json:"sessions"`
	SessionStats int64 `json:"session_stats"`
	Routes       int64 `json:"routes"`
	Unmapped     int64 `json:"unmapped"`
	Imports      int64 `json:"imports"`
}

// Measurement is one scalar reading as it travels.
//
// There is no id: a row id is a position in one database's tables and means
// nothing in another. There is a content key, and it has to be here:
// data.ContentKey hashes the raw value string and raw unit the *source* wrote,
// while the row holds the normalized value and original_unit, so nothing
// downstream can recompute it. A Connector that minted its own key would give
// the same reading a second identity, and importing an Archive beside a fresh
// export of the same source would store everything twice, which is the one
// failure ADR 0006 exists to prevent.
type Measurement struct {
	Metric       string  `json:"metric"`
	Value        float64 `json:"value"`
	OriginalUnit string  `json:"original_unit"`
	StartAt      string  `json:"start_at"`
	EndAt        string  `json:"end_at"`
	Source       string  `json:"source"`
	ContentKey   string  `json:"content_key"`
}

// State is one categorical interval as it travels: a sleep stage, a stand hour.
type State struct {
	Kind       string `json:"kind"`
	StateValue string `json:"state_value"`
	StartAt    string `json:"start_at"`
	EndAt      string `json:"end_at"`
	Source     string `json:"source"`
	ContentKey string `json:"content_key"`
}

// Session is one workout as it travels, with its stats and Routes nested rather
// than flattened into sibling entries. The store writes the three as one unit
// (connector.Store's InsertWorkout), and the file has no reason to disagree with
// that seam: flattening them would need the row ids the Archive deliberately
// drops.
type Session struct {
	ActivityType  string        `json:"activity_type"`
	StartAt       string        `json:"start_at"`
	EndAt         string        `json:"end_at"`
	Duration      float64       `json:"duration"`
	TotalDistance *float64      `json:"total_distance,omitempty"`
	TotalEnergy   *float64      `json:"total_energy,omitempty"`
	Source        string        `json:"source"`
	ContentKey    string        `json:"content_key"`
	Stats         []SessionStat `json:"stats,omitempty"`
	Routes        []Route       `json:"routes,omitempty"`
}

// SessionStat is one summary figure a workout carries, in the Metric's canonical
// unit.
type SessionStat struct {
	Metric string  `json:"metric"`
	Stat   string  `json:"stat"`
	Value  float64 `json:"value"`
}

// Route is a GPS track attached to a workout. Artifact names the file under
// artifacts/ in this same zip, which is its sha256, so the reference and the
// bytes cannot drift apart.
type Route struct {
	Artifact   string `json:"artifact"`
	StartAt    string `json:"start_at"`
	EndAt      string `json:"end_at"`
	Source     string `json:"source"`
	ContentKey string `json:"content_key"`
}

// Unmapped is one record the Catalog could not read, kept verbatim (ADR 0002).
type Unmapped struct {
	SourceType string `json:"source_type"`
	Value      string `json:"value"`
	Unit       string `json:"unit"`
	StartAt    string `json:"start_at"`
	EndAt      string `json:"end_at"`
	Source     string `json:"source"`
	ContentKey string `json:"content_key"`
}

// Import is one recorded Connector run, carried as provenance and never
// replayed: reading an Archive records its own run, like every other import.
type Import struct {
	Connector     string `json:"connector"`
	SourceFile    string `json:"source_file"`
	AddedCount    int    `json:"added_count"`
	SkippedCount  int    `json:"skipped_count"`
	UnmappedCount int    `json:"unmapped_count"`
	ImportedAt    string `json:"imported_at"`
}

// Source is what Write pulls an Account out of. It is an interface so the format
// can be exercised without SQLite, and so the one implementation over the models
// (ModelSource) stays the only place that knows both halves.
type Source interface {
	Counts(ctx context.Context) (data.ExportCounts, error)
	Profile(ctx context.Context) (*Profile, error)
	Measurements(ctx context.Context, after data.MeasurementCursor, limit int) ([]data.Measurement, data.MeasurementCursor, error)
	States(ctx context.Context, after data.StateCursor, limit int) ([]data.State, data.StateCursor, error)
	Sessions(ctx context.Context, after data.SessionCursor, limit int) ([]data.ExportSession, data.SessionCursor, error)
	Unmapped(ctx context.Context, after data.UnmappedCursor, limit int) ([]data.UnmappedRecord, data.UnmappedCursor, error)
	Imports(ctx context.Context) ([]data.Import, error)
	// OpenArtifact opens a Route's stored file by name. A name with no file
	// behind it returns an error the writer turns into a warning rather than a
	// failure: the track is gone either way, and dropping the Route with it
	// would lose the fact that the workout had one.
	OpenArtifact(name string) (io.ReadCloser, error)
}

// Summary is what one export did, for the operator rather than for the file.
// Warnings are the Routes whose artifact was missing from disk. They are
// deliberately not in the manifest: a reader of the Archive sees the same thing
// by looking for the entry, so recording it twice would only create a way for
// the two to disagree.
type Summary struct {
	Counts    data.ExportCounts
	Artifacts int
	Warnings  []string
}

// Write streams one Account's Archive into w. Nothing is buffered beyond a page,
// and nothing is written to disk on the way.
//
// version is the build version, stamped into the manifest, and now is the export
// timestamp: both are parameters so a test can pin the only two fields that are
// not a function of the data. The data entries are byte-identical across two
// exports of the same rows, which is what makes an Archive diffable and what the
// round-trip test asserts.
func Write(ctx context.Context, w io.Writer, src Source, version string, now time.Time) (Summary, error) {
	zw := zip.NewWriter(w)
	var summary Summary

	counts, err := src.Counts(ctx)
	if err != nil {
		return summary, err
	}
	summary.Counts = counts

	profile, err := src.Profile(ctx)
	if err != nil {
		return summary, err
	}

	manifest := Manifest{
		VerveArchive: Schema,
		Schema:       Schema,
		VerveVersion: version,
		ExportedAt:   now.UTC().Format(time.RFC3339),
		Profile:      profile,
		Counts: Counts{
			Measurements: counts.Measurements,
			States:       counts.States,
			Sessions:     counts.Sessions,
			SessionStats: counts.SessionStats,
			Routes:       counts.Routes,
			Unmapped:     counts.Unmapped,
			Imports:      counts.Imports,
		},
	}
	if err := writeManifest(zw, manifest); err != nil {
		return summary, err
	}

	if err := writePages(ctx, zw, MeasurementsName, src.Measurements, measurementRow); err != nil {
		return summary, err
	}
	if err := writePages(ctx, zw, StatesName, src.States, stateRow); err != nil {
		return summary, err
	}

	// Sessions carry the artifact names, so the set of files to copy is known
	// only once they are all written. They are copied after, in sorted order, so
	// the entry order is a function of the data and not of the traversal.
	artifacts := map[string]bool{}
	err = writePages(ctx, zw, SessionsName, src.Sessions, func(s data.ExportSession) Session {
		row := sessionRow(s)
		for _, r := range row.Routes {
			artifacts[r.Artifact] = true
		}
		return row
	})
	if err != nil {
		return summary, err
	}

	if err := writePages(ctx, zw, UnmappedName, src.Unmapped, unmappedRow); err != nil {
		return summary, err
	}

	imports, err := src.Imports(ctx)
	if err != nil {
		return summary, err
	}
	if err := writeAll(zw, ImportsName, imports, importRow); err != nil {
		return summary, err
	}

	written, warnings, err := writeArtifacts(ctx, zw, src, artifacts)
	if err != nil {
		return summary, err
	}
	summary.Artifacts, summary.Warnings = written, warnings

	if err := zw.Close(); err != nil {
		return summary, fmt.Errorf("archive: close: %w", err)
	}
	return summary, nil
}

// create adds an entry with a zero modification time. The zero time is
// deliberate: a timestamp per entry would make two exports of the same data
// differ for no reason a reader could use.
func create(zw *zip.Writer, name string) (io.Writer, error) {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return nil, fmt.Errorf("archive: create %s: %w", name, err)
	}
	return w, nil
}

func writeManifest(zw *zip.Writer, m Manifest) error {
	w, err := create(zw, ManifestName)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ") // the one entry a person reads first, so it reads
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("archive: write manifest: %w", err)
	}
	return nil
}

// pager is the shape every family's paged read has: rows after a cursor, and the
// cursor to pass back.
type pager[Row, Cursor any] func(ctx context.Context, after Cursor, limit int) ([]Row, Cursor, error)

// writePages drains a family into one NDJSON entry, a page at a time, converting
// each stored row into its travelling shape.
func writePages[Row, Wire, Cursor any](ctx context.Context, zw *zip.Writer, name string, next pager[Row, Cursor], wire func(Row) Wire) error {
	w, err := create(zw, name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)

	var cursor Cursor
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, after, err := next(ctx, cursor, pageSize)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if err := enc.Encode(wire(row)); err != nil {
				return fmt.Errorf("archive: write %s: %w", name, err)
			}
		}
		cursor = after
	}
}

// writeAll drains an already-read slice into an NDJSON entry, for the one family
// small enough not to page.
func writeAll[Row, Wire any](zw *zip.Writer, name string, rows []Row, wire func(Row) Wire) error {
	w, err := create(zw, name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for _, row := range rows {
		if err := enc.Encode(wire(row)); err != nil {
			return fmt.Errorf("archive: write %s: %w", name, err)
		}
	}
	return nil
}

// writeArtifacts copies each referenced GPX in once, in name order. The name is
// the file's content hash, so two Routes over the same track share one entry.
func writeArtifacts(ctx context.Context, zw *zip.Writer, src Source, refs map[string]bool) (int, []string, error) {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)

	written := 0
	var warnings []string
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return written, warnings, err
		}
		f, err := src.OpenArtifact(name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("route file missing: %s", name))
			continue
		}
		w, cerr := create(zw, path.Join("artifacts", name))
		if cerr != nil {
			f.Close()
			return written, warnings, cerr
		}
		_, err = io.Copy(w, f)
		f.Close()
		if err != nil {
			return written, warnings, fmt.Errorf("archive: copy artifact %s: %w", name, err)
		}
		written++
	}
	return written, warnings, nil
}

func measurementRow(m data.Measurement) Measurement {
	return Measurement{
		Metric:       m.Metric,
		Value:        m.Value,
		OriginalUnit: m.OriginalUnit,
		StartAt:      m.StartAt,
		EndAt:        m.EndAt,
		Source:       m.Source,
		ContentKey:   m.ContentKey,
	}
}

func stateRow(s data.State) State {
	return State{
		Kind:       s.Kind,
		StateValue: s.StateValue,
		StartAt:    s.StartAt,
		EndAt:      s.EndAt,
		Source:     s.Source,
		ContentKey: s.ContentKey,
	}
}

func sessionRow(s data.ExportSession) Session {
	row := Session{
		ActivityType:  s.Session.ActivityType,
		StartAt:       s.Session.StartAt,
		EndAt:         s.Session.EndAt,
		Duration:      s.Session.Duration,
		TotalDistance: s.Session.TotalDistance,
		TotalEnergy:   s.Session.TotalEnergy,
		Source:        s.Session.Source,
		ContentKey:    s.Session.ContentKey,
	}
	for _, st := range s.Stats {
		row.Stats = append(row.Stats, SessionStat{Metric: st.Metric, Stat: st.Stat, Value: st.Value})
	}
	for _, r := range s.Routes {
		row.Routes = append(row.Routes, Route{
			Artifact:   r.Artifact,
			StartAt:    r.StartAt,
			EndAt:      r.EndAt,
			Source:     r.Source,
			ContentKey: r.ContentKey,
		})
	}
	return row
}

func unmappedRow(u data.UnmappedRecord) Unmapped {
	return Unmapped{
		SourceType: u.SourceType,
		Value:      u.Value,
		Unit:       u.Unit,
		StartAt:    u.StartAt,
		EndAt:      u.EndAt,
		Source:     u.Source,
		ContentKey: u.ContentKey,
	}
}

func importRow(i data.Import) Import {
	return Import{
		Connector:     i.Connector,
		SourceFile:    i.SourceFile,
		AddedCount:    i.AddedCount,
		SkippedCount:  i.SkippedCount,
		UnmappedCount: i.UnmappedCount,
		ImportedAt:    i.ImportedAt,
	}
}
