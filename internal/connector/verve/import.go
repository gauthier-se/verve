package verve

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/archive"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/data"
)

// maxLine bounds one NDJSON line. A Measurement is a couple of hundred bytes and
// the largest row is a workout with its stats and Routes nested, so a megabyte is
// far past anything the writer produces: reaching it means the file is not what
// it claims to be, and saying so beats growing a buffer until memory runs out.
const maxLine = 1 << 20

// contentKeyShape is what a content key must look like: the hex sha256 every
// data.ContentKey call produces. The shape is checked and the provenance is not,
// because it cannot be: the key hashes the raw value string the *source* wrote,
// which no stored row keeps (ADR 0039). A forged key is bounded by
// UNIQUE (account_id, content_key): it can collide with a row in the importing
// Account and with nothing else.
var contentKeyShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Import reads a Verve Archive and writes it through the canonical families.
//
// Nothing is written before the manifest is understood and the first line
// parses. A malformed line stops the run naming the entry and the line number,
// which is the opposite of what the Unmapped bin does and deliberately so: the
// bin exists for data a *source* produced that the Catalog cannot map, which is
// expected. A bad line in an Archive means the file is damaged or hand-edited,
// and the useful answer is the line, not a partial import nobody will notice.
func Import(ctx context.Context, store connector.Store, accountID int64, p string, opts connector.Options) (connector.Report, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return connector.Report{}, fmt.Errorf("verve archive: open %s: %w", filepath.Base(p), err)
	}
	defer zr.Close()

	manifest, err := readManifest(&zr.Reader)
	if err != nil {
		return connector.Report{}, fmt.Errorf("verve archive: %s has no readable %s: %w",
			filepath.Base(p), archive.ManifestName, err)
	}
	if manifest.Schema > archive.Schema {
		return connector.Report{}, fmt.Errorf(
			"verve archive: %s was written by a newer Verve (archive schema %d, this build reads %d): upgrade Verve and import it again",
			filepath.Base(p), manifest.Schema, archive.Schema)
	}

	imp := &importer{
		zr:        &zr.Reader,
		file:      filepath.Base(p),
		sink:      connector.NewSink(store, accountID, connectorName, sourceName(p), opts),
		accountID: accountID,
		artifacts: opts.ArtifactsDir,
		progress:  connector.NewProgressCounter(readableBytes(&zr.Reader), opts.Progress),
	}

	if err := imp.run(ctx, manifest); err != nil {
		return connector.Report{}, err
	}
	return imp.sink.Close(ctx)
}

// importer carries the per-run state the entry readers share.
type importer struct {
	zr        *zip.Reader
	file      string
	sink      *connector.Sink
	accountID int64
	artifacts string
	progress  *connector.ProgressCounter

	// read counts what each entry held, checked against the manifest at the end.
	read archive.Counts
}

// run reads every entry in a fixed order and then checks what it read against
// what the manifest promised.
func (i *importer) run(ctx context.Context, manifest archive.Manifest) error {
	if err := i.measurements(ctx); err != nil {
		return err
	}
	if err := i.states(ctx); err != nil {
		return err
	}
	if err := i.sessions(ctx); err != nil {
		return err
	}
	if err := i.unmapped(ctx); err != nil {
		return err
	}
	return i.checkCounts(manifest)
}

// checkCounts compares what was read with what the manifest declared. A zip can
// be intact and still be short: the central directory only proves the entries
// are whole, not that they hold everything the export meant to write.
func (i *importer) checkCounts(m archive.Manifest) error {
	for _, c := range []struct {
		what       string
		want, have int64
	}{
		{archive.MeasurementsName, m.Counts.Measurements, i.read.Measurements},
		{archive.StatesName, m.Counts.States, i.read.States},
		{archive.SessionsName, m.Counts.Sessions, i.read.Sessions},
		{archive.UnmappedName, m.Counts.Unmapped, i.read.Unmapped},
	} {
		if c.want != c.have {
			return fmt.Errorf("verve archive: %s: %s holds %d rows, the manifest says %d: the file is incomplete",
				i.file, c.what, c.have, c.want)
		}
	}
	return nil
}

// eachLine walks one NDJSON entry, decoding a row per line and handing it to fn
// with its line number for the error message. A missing entry is not an error:
// an Account with no workouts exports an empty sessions entry, and an Archive
// written by an older schema may not have one at all.
func eachLine[Row any](ctx context.Context, i *importer, name string, fn func(row Row, line int) error) error {
	f, err := i.zr.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verve archive: %s: open %s: %w", i.file, name, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(i.progress.Wrap(f))
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)

	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var row Row
		if err := json.Unmarshal(raw, &row); err != nil {
			return fmt.Errorf("verve archive: %s: %s line %d: %w", i.file, name, line, err)
		}
		if err := fn(row, line); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("verve archive: %s: %s: %w", i.file, name, err)
	}
	return nil
}

// measurements reads the scalar family.
//
// A slug the Catalog does not know goes to the Unmapped bin rather than being
// refused: an Archive written by a newer Verve can carry a Metric this build has
// never heard of, and the bin is precisely where a record whose type cannot be
// mapped belongs (ADR 0002). Upgrade later, re-import, and the row lands as a
// Measurement, which is what already happens whenever the Catalog grows.
func (i *importer) measurements(ctx context.Context) error {
	return eachLine(ctx, i, archive.MeasurementsName, func(row archive.Measurement, line int) error {
		if err := i.checkRow(archive.MeasurementsName, line, row.ContentKey, row.StartAt, row.EndAt); err != nil {
			return err
		}
		if math.IsNaN(row.Value) || math.IsInf(row.Value, 0) {
			return i.bad(archive.MeasurementsName, line, "value is not a finite number")
		}
		i.read.Measurements++

		if _, known := catalog.Lookup(row.Metric); !known {
			return i.sink.Unmapped(ctx, data.UnmappedRecord{
				AccountID:  i.accountID,
				SourceType: row.Metric,
				Value:      strconv.FormatFloat(row.Value, 'f', -1, 64),
				Unit:       row.OriginalUnit,
				StartAt:    row.StartAt,
				EndAt:      row.EndAt,
				Source:     row.Source,
				ContentKey: row.ContentKey,
			})
		}
		// The Manual Source travels, and this is the one Connector that may write
		// it: it restores a figure the owner typed rather than inventing a
		// hand-typed row (ADR 0022, ADR 0039). The restored row is still Manual,
		// so it still overlays its day and is still deletable. An Exclusion still
		// refuses it, which the Sink does for every Metric alike: a standing
		// refusal is the more recent statement (ADR 0033).
		return i.sink.Measurement(ctx, data.Measurement{
			AccountID:    i.accountID,
			Metric:       row.Metric,
			Value:        row.Value,
			OriginalUnit: row.OriginalUnit,
			StartAt:      row.StartAt,
			EndAt:        row.EndAt,
			Source:       row.Source,
			ContentKey:   row.ContentKey,
		})
	})
}

func (i *importer) states(ctx context.Context) error {
	return eachLine(ctx, i, archive.StatesName, func(row archive.State, line int) error {
		if err := i.checkRow(archive.StatesName, line, row.ContentKey, row.StartAt, row.EndAt); err != nil {
			return err
		}
		if row.Kind == "" || row.StateValue == "" {
			return i.bad(archive.StatesName, line, "a state needs a kind and a value")
		}
		i.read.States++
		return i.sink.State(ctx, data.State{
			AccountID:  i.accountID,
			Kind:       row.Kind,
			StateValue: row.StateValue,
			StartAt:    row.StartAt,
			EndAt:      row.EndAt,
			Source:     row.Source,
			ContentKey: row.ContentKey,
		})
	})
}

// sessions reads the workouts, copying each Route's GPX out of the Archive
// before the write: the artifact is content-addressed, so it can be on disk
// before any row points at it (ADR 0004).
func (i *importer) sessions(ctx context.Context) error {
	return eachLine(ctx, i, archive.SessionsName, func(row archive.Session, line int) error {
		if err := i.checkRow(archive.SessionsName, line, row.ContentKey, row.StartAt, row.EndAt); err != nil {
			return err
		}
		if row.ActivityType == "" {
			return i.bad(archive.SessionsName, line, "a session needs an activity type")
		}
		i.read.Sessions++

		session := &data.Session{
			AccountID:     i.accountID,
			ActivityType:  row.ActivityType,
			StartAt:       row.StartAt,
			EndAt:         row.EndAt,
			Duration:      row.Duration,
			TotalDistance: row.TotalDistance,
			TotalEnergy:   row.TotalEnergy,
			Source:        row.Source,
			ContentKey:    row.ContentKey,
		}

		stats := make([]data.SessionStat, 0, len(row.Stats))
		for _, s := range row.Stats {
			if _, known := catalog.Lookup(s.Metric); !known {
				continue // a stat this build's Catalog cannot name has no unit to be in
			}
			if !validStat(s.Stat) {
				return i.bad(archive.SessionsName, line, fmt.Sprintf("unknown session stat %q", s.Stat))
			}
			stats = append(stats, data.SessionStat{Metric: s.Metric, Stat: s.Stat, Value: s.Value})
		}

		routes := make([]data.Route, 0, len(row.Routes))
		for _, r := range row.Routes {
			if err := i.checkRow(archive.SessionsName, line, r.ContentKey, r.StartAt, r.EndAt); err != nil {
				return err
			}
			// A Route whose file is absent from the Archive still gets its row.
			// The track is gone either way, and dropping the Route with it would
			// lose the fact that the workout had one, which is the same call the
			// writer makes on the way out.
			if err := i.copyArtifact(r.Artifact); err != nil {
				return err
			}
			routes = append(routes, data.Route{
				AccountID:  i.accountID,
				Artifact:   r.Artifact,
				StartAt:    r.StartAt,
				EndAt:      r.EndAt,
				Source:     r.Source,
				ContentKey: r.ContentKey,
			})
		}

		return i.sink.Workout(ctx, session, stats, routes)
	})
}

func (i *importer) unmapped(ctx context.Context) error {
	return eachLine(ctx, i, archive.UnmappedName, func(row archive.Unmapped, line int) error {
		if err := i.checkRow(archive.UnmappedName, line, row.ContentKey, row.StartAt, row.EndAt); err != nil {
			return err
		}
		if row.SourceType == "" {
			return i.bad(archive.UnmappedName, line, "an unmapped record needs a source type")
		}
		i.read.Unmapped++
		return i.sink.Unmapped(ctx, data.UnmappedRecord{
			AccountID:  i.accountID,
			SourceType: row.SourceType,
			Value:      row.Value,
			Unit:       row.Unit,
			StartAt:    row.StartAt,
			EndAt:      row.EndAt,
			Source:     row.Source,
			ContentKey: row.ContentKey,
		})
	})
}

// copyArtifact writes one GPX from the Archive into the artifacts directory,
// skipping a file already there: the name is the content hash, so a file with
// that name already holds those bytes. An artifact the Archive does not carry is
// not an error; see the caller.
func (i *importer) copyArtifact(name string) error {
	if name == "" || i.artifacts == "" {
		return nil
	}
	dst := filepath.Join(i.artifacts, filepath.Base(name))
	if _, err := os.Stat(dst); err == nil {
		return nil
	}

	src, err := i.zr.Open(path.Join("artifacts", filepath.Base(name)))
	if err != nil {
		return nil // the Route travels without its track
	}
	defer src.Close()

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("verve archive: %s: write route file %s: %w", i.file, name, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, i.progress.Wrap(src)); err != nil {
		return fmt.Errorf("verve archive: %s: copy route file %s: %w", i.file, name, err)
	}
	return nil
}

// checkRow is the validation every family shares: a content key of the right
// shape, and an interval that parses and does not run backwards.
func (i *importer) checkRow(entry string, line int, key, start, end string) error {
	if !contentKeyShape.MatchString(key) {
		return i.bad(entry, line, "content key is not a sha256 hex digest")
	}
	from, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return i.bad(entry, line, fmt.Sprintf("start_at %q is not an RFC 3339 timestamp", start))
	}
	to, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return i.bad(entry, line, fmt.Sprintf("end_at %q is not an RFC 3339 timestamp", end))
	}
	if to.Before(from) {
		return i.bad(entry, line, "end_at is before start_at")
	}
	return nil
}

func (i *importer) bad(entry string, line int, why string) error {
	return fmt.Errorf("verve archive: %s: %s line %d: %s", i.file, entry, line, why)
}

func validStat(s string) bool {
	switch s {
	case data.StatSum, data.StatAverage, data.StatMin, data.StatMax:
		return true
	}
	return false
}

// readableBytes is how much of the Archive this import will read, for progress.
// Unlike the other two Connectors, which estimate, a zip's central directory
// states every entry's uncompressed size, so the ratio here is exact.
func readableBytes(zr *zip.Reader) int64 {
	var total int64
	for _, f := range zr.File {
		if f.Name == archive.ManifestName || f.Name == archive.ImportsName {
			continue // the manifest is read before progress starts; imports are not replayed
		}
		total += int64(f.UncompressedSize64)
	}
	return total
}
