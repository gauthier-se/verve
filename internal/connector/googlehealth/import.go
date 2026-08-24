// Package googlehealth is the Google Health Connector: it reads the "Google Health"
// section of a Google Takeout archive and writes it into the canonical families.
//
// The export is a zip of CSV files, one directory of time series plus a pile of
// account settings. Every series file carries a header, and a README beside it
// documents each column and its unit, which is what makes this Connector's mapping
// a table (ADR 0009) rather than a guess. It writes Measurements and nothing else,
// because the export holds nothing else: no sleep stages, no workouts, no routes.
//
// A Google Health account is usually filled by syncing another platform into it, so
// most rows arrive attributed to an upstream app ("Apple Health Health Kit",
// "Zepp Life Health Kit"). Those Source names are stored verbatim: a row Google
// mirrored and a row Apple recorded are different provenance claims, and rewriting
// one into the other would make them indistinguishable in storage forever.
package googlehealth

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/units"
)

// connectorName is this Connector's stable identifier, recorded with every Import.
const connectorName = "googlehealth"

const (
	// healthDir is the archive segment identifying the part of a Takeout we read. A
	// Takeout can bundle a dozen other Google services, and its root folder is
	// Google's to name, so the segment is what identifies our part of it.
	healthDir = "/Google Health/"
	// activityDir holds the time series: one directory, fourteen families in the
	// reference export, all of the form timestamp,<value>,data source.
	activityDir = "Physical Activity_GoogleData/"
	// legacyDir holds the same quantities in the older Fitbit JSON shape: US units,
	// MM/DD/YY timestamps with no zone, and no source attribution. Same data, worse,
	// so it is kept in the Unmapped bin and graphed by nothing.
	legacyDir = "Global Export Data/"
)

// legacySource is the Source recorded for the legacy JSON series, which carry no
// attribution of their own. It names the aggregator, which is all that is known.
const legacySource = "Google Health"

// dateShard strips the date a series file is named after: heart_rate_2026-08-20.csv
// and the 65 files beside it are one family. The timestamp column is authoritative,
// so the filename's date is redundant by construction.
var dateShard = regexp.MustCompile(`[-_ ]\d{4}-\d{2}-\d{2}$`)

// legacyTimeLayouts are the shapes the legacy JSON dates in. They carry no zone, so
// they are read as UTC, a guess, and a free one: the bin is never graphed, since
// no read path serves raw rows (ADR 0012).
var legacyTimeLayouts = []string{"01/02/06 15:04:05", "01/02/06"}

// Connector is the Google Health Takeout Connector. It is a value type with no
// state; the registry holds one.
type Connector struct{}

// Name implements connector.Connector.
func (Connector) Name() string { return connectorName }

// Label implements connector.Connector.
func (Connector) Label() string { return "Google Health" }

// Accepts recognizes a Takeout by the directory segment it exports health data
// under, never by the extension: both supported exports are .zip.
func (Connector) Accepts(p string) bool {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if isHealthEntry(f.Name) {
			return true
		}
	}
	return false
}

// Import implements connector.Connector.
func (Connector) Import(ctx context.Context, store connector.Store, accountID int64, p string, opts connector.Options) (connector.Report, error) {
	return Import(ctx, store, accountID, p, opts)
}

// normalizeName makes an archive entry's path comparable. Every entry in the
// reference export spells its directory "Google Health" with a NON-BREAKING
// space, so a comparison against a hand-typed "Google Health" matches nothing at
// all and the whole archive reads as empty. This is the same class of trap
// internal/catalog/priority.go documents for Apple's device names, from a different
// vendor: a name that looks right on screen and is not the bytes it appears to be.
// Normalizing costs one pass per entry name and removes the entire class.
func normalizeName(name string) string {
	return strings.ReplaceAll(name, " ", " ")
}

// isHealthEntry reports whether an archive entry sits under the Google Health
// section. The leading separator is part of the match so a top-level directory
// merely *named* like it cannot pass.
func isHealthEntry(name string) bool {
	return strings.Contains("/"+normalizeName(name), healthDir)
}

// relative returns an entry's path below the Google Health section, e.g.
// "Physical Activity_GoogleData/steps_2026-08-01.csv".
func relative(name string) string {
	_, rest, _ := strings.Cut("/"+normalizeName(name), healthDir)
	return rest
}

// family is a series file's identity: its basename with the date shard and the
// extension removed.
func family(rel string) string {
	base := path.Base(rel)
	base = strings.TrimSuffix(base, path.Ext(base))
	return dateShard.ReplaceAllString(base, "")
}

// Import reads the Google Health section of the Takeout at p and writes it to
// store, scoped to accountID.
func Import(ctx context.Context, store connector.Store, accountID int64, p string, opts connector.Options) (connector.Report, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return connector.Report{}, fmt.Errorf("googlehealth: open zip %s: %w", p, err)
	}
	defer zr.Close()
	return importArchive(ctx, store, accountID, path.Base(p), &zr.Reader, opts)
}

// importer carries the state one import accumulates: the report, the pending
// batches, and the progress counters. It exists so the per-entry readers stay small
// and so a batch flush is one method rather than the same twelve lines per family.
type importer struct {
	store     connector.Store
	accountID int64
	opts      connector.Options
	report    connector.Report

	measurements []data.Measurement
	unmapped     []data.UnmappedRecord

	readBytes  int64
	totalBytes int64
}

// importArchive walks the Google Health section in a stable order and routes each
// entry: a mapped series to Measurements, an unmapped one to the bin, and anything
// that is not a health record to the ignored tally.
func importArchive(ctx context.Context, store connector.Store, accountID int64, sourceFile string, zr *zip.Reader, opts connector.Options) (connector.Report, error) {
	imp := &importer{
		store:     store,
		accountID: accountID,
		opts:      opts,
		report: connector.Report{
			Connector:     connectorName,
			SourceFile:    sourceFile,
			PerMetric:     make(map[string]connector.Tally),
			UnmappedTypes: make(map[string]int),
			PerState:      make(map[string]connector.Tally),
			PerActivity:   make(map[string]connector.Tally),
			Ignored:       make(map[string]int),
		},
		measurements: make([]data.Measurement, 0, connector.BatchSize),
		unmapped:     make([]data.UnmappedRecord, 0, connector.BatchSize),
	}

	// Entries are read in name order so the report is the same for the same archive,
	// whatever order the zip happens to store them in.
	var entries []*zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isHealthEntry(f.Name) {
			continue
		}
		entries = append(entries, f)
		if imp.reads(relative(f.Name)) {
			imp.totalBytes += int64(f.UncompressedSize64)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	for _, f := range entries {
		if err := ctx.Err(); err != nil {
			return connector.Report{}, err
		}
		if err := imp.entry(ctx, f); err != nil {
			return connector.Report{}, err
		}
	}

	if err := imp.flushMeasurements(ctx); err != nil {
		return connector.Report{}, err
	}
	if err := imp.flushUnmapped(ctx); err != nil {
		return connector.Report{}, err
	}

	if err := store.RecordImport(ctx, &data.Import{
		AccountID:     accountID,
		Connector:     connectorName,
		SourceFile:    sourceFile,
		AddedCount:    imp.report.Added,
		SkippedCount:  imp.report.Skipped,
		UnmappedCount: imp.report.Unmapped,
	}); err != nil {
		return connector.Report{}, fmt.Errorf("googlehealth: record import: %w", err)
	}
	return imp.report, nil
}

// reads reports whether an entry holds data this Connector reads at all, which is
// what the progress total is measured against.
func (imp *importer) reads(rel string) bool {
	switch {
	case strings.HasPrefix(rel, activityDir):
		return strings.EqualFold(path.Ext(rel), ".csv")
	case strings.HasPrefix(rel, legacyDir):
		return strings.EqualFold(path.Ext(rel), ".json")
	default:
		return false
	}
}

// entry routes one archive entry. Everything outside the two data directories is
// account material, settings, subscriptions, READMEs, an avatar, and is counted
// under its directory rather than kept: see Report.Ignored.
func (imp *importer) entry(ctx context.Context, f *zip.File) error {
	rel := relative(f.Name)
	if !imp.reads(rel) {
		imp.report.Ignored[path.Dir(rel)]++
		return nil
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("googlehealth: open %s: %w", f.Name, err)
	}
	defer rc.Close()

	var r io.Reader = rc
	if imp.opts.Progress != nil {
		r = &countingReader{r: rc, imp: imp}
	}

	if strings.HasPrefix(rel, legacyDir) {
		return imp.legacy(ctx, rel, r)
	}
	return imp.series(ctx, rel, r)
}

// series reads one Physical Activity file: a header, then rows of
// timestamp,<value…>,data source. A mapped family becomes Measurements; an unmapped
// one is kept whole in the bin, so a family Google adds tomorrow is not lost today
// (ADR 0002).
func (imp *importer) series(ctx context.Context, rel string, r io.Reader) error {
	cr := csv.NewReader(r)
	// Rows carry trailing empty fields in some families, and one embeds a quoted
	// JSON object with commas in it; the reader handles the quoting, and a variable
	// field count is not an error worth failing an import over.
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil // an empty file, of which the reference export has many
	}
	if err != nil {
		return fmt.Errorf("googlehealth: read %s: %w", rel, err)
	}

	cols := index(header)
	tsCol, ok := cols["timestamp"]
	if !ok {
		// A file with no timestamp column is not a series: the "no data" placeholders
		// Google writes for features never used land here.
		imp.report.Ignored[path.Dir(rel)]++
		return nil
	}
	srcCol, hasSource := cols["data source"]

	fam := family(rel)
	m, mapped := familyToMetric[fam]
	valCol := -1
	if mapped {
		// Columns are found by name and never by position. vo2_max.csv is the reason:
		// its columns are value, error and method, and a Connector reading "column 1"
		// imports an error rate as a fitness score the day Google adds a column.
		valCol, ok = cols[strings.ToLower(m.Column)]
		if !ok {
			return fmt.Errorf("googlehealth: %s has no %q column (found %s)", rel, m.Column, strings.Join(header, ", "))
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("googlehealth: read %s: %w", rel, err)
		}
		if len(row) <= tsCol {
			continue
		}

		at := normalizeTime(row[tsCol])
		source := ""
		if hasSource && srcCol < len(row) {
			source = strings.TrimSpace(row[srcCol])
		}
		if source == "" {
			source = legacySource
		}

		if !mapped {
			imp.addUnmapped(rowJSON(header, row), "", at, source, fam)
		} else if valCol < len(row) {
			imp.addMeasurement(m, strings.TrimSpace(row[valCol]), at, source, fam)
		}

		// Bounded batches, as every Connector writes: the reference export's energy
		// series alone is a hundred thousand rows, and a flush per file would hold
		// them all before the first write.
		if len(imp.measurements) >= connector.BatchSize {
			if err := imp.flushMeasurements(ctx); err != nil {
				return err
			}
		}
		if len(imp.unmapped) >= connector.BatchSize {
			if err := imp.flushUnmapped(ctx); err != nil {
				return err
			}
		}
	}
}

// addMeasurement converts one row into a Measurement, or into the Unmapped bin when
// its value is not a number the Catalog's unit can hold, the same fallback the
// Apple Connector makes, for the same reason: a row Verve cannot read is still a row
// the Account owns.
func (imp *importer) addMeasurement(m csvMetric, raw, at, source, fam string) {
	metric, ok := catalog.Lookup(m.Metric)
	if !ok {
		imp.addUnmapped(raw, m.Unit, at, source, fam)
		return
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		imp.addUnmapped(raw, m.Unit, at, source, fam)
		return
	}
	value, err := units.Convert(parsed, m.Unit, metric.Unit)
	if err != nil {
		imp.addUnmapped(raw, m.Unit, at, source, fam)
		return
	}

	// An Exclusion is checked once the slug is known and before the row is queued:
	// what it names is never written, and is counted rather than swallowed (ADR 0033).
	if imp.opts.Exclusions.Excludes(m.Metric, at) {
		t := imp.report.PerMetric[m.Metric]
		t.Excluded++
		imp.report.PerMetric[m.Metric] = t
		imp.report.Excluded++
		return
	}

	imp.measurements = append(imp.measurements, data.Measurement{
		AccountID:    imp.accountID,
		Metric:       m.Metric,
		Value:        value,
		OriginalUnit: m.Unit,
		StartAt:      at,
		EndAt:        at,
		Source:       source,
		ContentKey:   data.ContentKey(m.Metric, source, at, at, canonical(parsed), m.Unit),
	})
}

// canonical renders a parsed value as the shortest string that reads back to the
// same float. It is what goes into the Content key, in place of the raw cell.
//
// The Apple Connector hashes the raw string so the key is byte-stable and free of
// float-formatting ambiguity (ADR 0006), and that reasoning holds only where one
// source writes one number one way. Google does not: it publishes resting heart
// rate under two family names, from the same app at the same instant, written "51"
// in one and "51.0" in the other. Hashing the cell keeps both, and an Account ends
// up with two identical readings a day for a Metric it never took twice. Hashing a
// canonical rendering is just as byte-stable, it is derived, not transcribed, and
// it makes the two agree, which is what ADR 0006 wanted from the key in the first
// place.
func canonical(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// addUnmapped queues one row for the Unmapped bin under a source type naming this
// Connector and the family it came from, so the bin stays readable once two
// Connectors write into it.
func (imp *importer) addUnmapped(value, unit, at, source, fam string) {
	typ := "google_health/" + fam
	imp.unmapped = append(imp.unmapped, data.UnmappedRecord{
		AccountID:  imp.accountID,
		SourceType: typ,
		Value:      value,
		Unit:       unit,
		StartAt:    at,
		EndAt:      at,
		Source:     source,
		ContentKey: data.ContentKey(typ, source, at, at, value, unit),
	})
}

// legacy reads one Global Export Data file: the older Fitbit JSON of quantities the
// CSV series already carry, in US units and with no attribution. Every element is
// kept whole in the bin, nothing is mapped from here, because a weight in pounds
// under a dateless timestamp is not evidence a curve should be drawn from.
func (imp *importer) legacy(ctx context.Context, rel string, r io.Reader) error {
	var elements []map[string]any
	if err := json.NewDecoder(r).Decode(&elements); err != nil {
		// Google ships several shapes here and adds to them; an unreadable one is one
		// ignored file, not a failed import.
		imp.report.Ignored[path.Dir(rel)]++
		return nil
	}

	fam := "global_export/" + family(rel)
	for _, el := range elements {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := json.Marshal(el)
		if err != nil {
			continue
		}
		imp.addUnmapped(string(raw), "", legacyTime(el), legacySource, fam)
		if len(imp.unmapped) >= connector.BatchSize {
			if err := imp.flushUnmapped(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// legacyTime reads an element's instant from whichever of the legacy shapes it
// uses: a "dateTime", or a "date" with an optional "time" beside it.
func legacyTime(el map[string]any) string {
	raw, _ := el["dateTime"].(string)
	if raw == "" {
		day, _ := el["date"].(string)
		clock, _ := el["time"].(string)
		raw = strings.TrimSpace(day + " " + clock)
	}
	for _, layout := range legacyTimeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return raw
}

// flushMeasurements writes the pending Measurements and tallies what was new.
func (imp *importer) flushMeasurements(ctx context.Context) error {
	if len(imp.measurements) == 0 {
		return nil
	}
	added, err := imp.store.InsertBatch(ctx, imp.measurements)
	if err != nil {
		return fmt.Errorf("googlehealth: insert measurements: %w", err)
	}
	for i, isNew := range added {
		t := imp.report.PerMetric[imp.measurements[i].Metric]
		if isNew {
			t.Added++
			imp.report.Added++
		} else {
			t.Skipped++
			imp.report.Skipped++
		}
		imp.report.PerMetric[imp.measurements[i].Metric] = t
	}
	imp.measurements = imp.measurements[:0]
	return nil
}

// flushUnmapped writes the pending bin rows and tallies what was new.
func (imp *importer) flushUnmapped(ctx context.Context) error {
	if len(imp.unmapped) == 0 {
		return nil
	}
	added, err := imp.store.InsertUnmappedBatch(ctx, imp.unmapped)
	if err != nil {
		return fmt.Errorf("googlehealth: insert unmapped: %w", err)
	}
	for i, isNew := range added {
		if isNew {
			imp.report.Unmapped++
			imp.report.UnmappedTypes[imp.unmapped[i].SourceType]++
		}
	}
	imp.unmapped = imp.unmapped[:0]
	return nil
}

// index maps a header's cells to their positions, lowercased and trimmed so a
// change of case in the export does not silently unmap a column.
func index(header []string) map[string]int {
	cols := make(map[string]int, len(header))
	for i, h := range header {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return cols
}

// rowJSON renders a row as a JSON object keyed by its header, which is what the bin
// keeps for a family with no Metric: every column, readable, in one text field.
func rowJSON(header, row []string) string {
	obj := make(map[string]string, len(row))
	for i, cell := range row {
		key := strconv.Itoa(i)
		if i < len(header) {
			key = strings.TrimSpace(header[i])
		}
		obj[key] = cell
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return strings.Join(row, ",")
	}
	return string(raw)
}

// normalizeTime renders a Google timestamp as RFC 3339 UTC, byte for byte what the
// Apple Connector produces, so rows from the two sort, bucket and compare
// identically. Google writes microseconds and this drops them: the finest grain any
// read path serves is the day (ADR 0012), and two samples inside one second either
// differ in value, in which case both are kept, or are identical, in which case the
// Content key was going to collapse them anyway. An unparseable value is kept
// verbatim rather than dropped, as the Apple Connector does.
func normalizeTime(s string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return strings.TrimSpace(s)
	}
	return t.UTC().Format(time.RFC3339)
}

// countingReader reports progress in bytes consumed against the summed size of the
// entries this import will read, so the web import's second phase means the same
// thing here as it does for a streamed XML export.
type countingReader struct {
	r   io.Reader
	imp *importer
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.imp.readBytes += int64(n)
	c.imp.opts.Progress(c.imp.readBytes, c.imp.totalBytes)
	return n, err
}
