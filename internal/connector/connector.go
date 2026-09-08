// Package connector is the contract every Connector implements (ADR 0009): the
// store it writes through, the per-run Options it takes, and the Report it gives
// back. The implementations live in subpackages and are wired into one registry
// (internal/connector/registry), so adding a source is a package plus one line.
//
// Nothing here knows about a source format. A Connector's own package owns "how to
// read the file"; the mapping from a source type to a Catalog Metric is declarative
// data inside it (ADR 0009), and the canonical families it writes are internal/data's.
package connector

import (
	"context"
	"errors"

	"github.com/gauthier-se/verve/internal/data"
)

// BatchSize is how many rows accumulate before a Connector flushes. It bounds
// memory and keeps each write transaction (and the WAL) small during a large
// import, which is a property of the store rather than of any one file format.
const BatchSize = 5000

// ErrUnknownExport is returned when no registered Connector recognizes a file. It
// is a sentinel so the callers can turn it into a sentence naming what Verve does
// read, rather than surfacing a path.
var ErrUnknownExport = errors.New("connector: no connector recognizes this export")

// Connector reads an external export and writes it into the canonical families.
// Implementations are compiled into the binary and contributed by pull request
// (ADR 0009).
type Connector interface {
	// Name is the stable identifier recorded with an Import, e.g. "applehealth".
	Name() string
	// Label is the source as a person names it, e.g. "Apple Health".
	Label() string
	// Accepts reports whether path is an export this Connector can read. It is asked
	// before Import and must be cheap: open, look, close. Detection is by content and
	// never by extension, because two sources can ship the same one.
	Accepts(path string) bool
	// Import reads the export at path and writes it to store, scoped to accountID.
	Import(ctx context.Context, store Store, accountID int64, path string, opts Options) (Report, error)
}

// Store is the subset of the data layer a Connector writes through. Rows are
// deduplicated per account by content key; batch inserts return a mask of which
// rows were newly added, and the single-row session/route inserts return whether
// the row was new (see internal/data).
type Store interface {
	InsertBatch(ctx context.Context, ms []data.Measurement) ([]bool, error)
	InsertUnmappedBatch(ctx context.Context, us []data.UnmappedRecord) ([]bool, error)
	InsertStateBatch(ctx context.Context, ss []data.State) ([]bool, error)
	InsertSession(ctx context.Context, s *data.Session) (bool, error)
	InsertSessionStats(ctx context.Context, sessionID int64, stats []data.SessionStat) error
	InsertRoute(ctx context.Context, r *data.Route) (bool, error)
	Record(ctx context.Context, imp *data.Import) error
}

// Progress reports decode progress during an import: decoded is how much of the
// export has been read and total how much there is, in whatever unit the Connector
// counts (bytes, for both shipped implementations). Only the ratio is used, so the
// unit is the Connector's business. It is called frequently, so an implementation
// must be cheap and non-blocking.
type Progress func(decoded, total int64)

// Options are the per-run inputs of an import beyond the source itself. They are
// one struct rather than a growing argument list because the call sites differ only
// by which of these they set.
type Options struct {
	// ArtifactsDir is where GPX route artifacts are copied (ADR 0004).
	ArtifactsDir string
	// Progress, when non-nil, is called as the export is read: the web import's
	// honest second phase, at no cost to the CLI path (which leaves it nil).
	Progress Progress
	// Exclusions are the Metrics and spans this Account refused (ADR 0033). A
	// matching record is dropped and counted rather than written. The zero value
	// excludes nothing, which is the common case and costs one map probe per record.
	Exclusions data.ExclusionSet
}

// Tally is one bucket's counts within a Report, used per Metric, per State kind,
// and per activity type. Excluded is scalar-only: nothing but a Measurement can be
// excluded yet (ADR 0033), and the field stays zero on a State or activity tally.
type Tally struct {
	Added    int
	Skipped  int
	Excluded int
}

// Report is the outcome of one import, suitable for a readable CLI summary.
type Report struct {
	Connector  string // the Connector's Name, recorded with the Import
	SourceFile string
	Added      int
	Skipped    int
	Unmapped   int // newly kept in the Unmapped bin
	// Excluded is how many records the Account's Exclusions refused (ADR 0033). It
	// is reported rather than absorbed silently: an import that drops data without
	// saying how much is worse than the delete that did not stick.
	Excluded      int
	PerMetric     map[string]Tally
	UnmappedTypes map[string]int // raw source type → count newly kept
	// Ignored counts the files an archive holds that are not health records at all
	//, account settings, subscriptions, READMEs, keyed by where they sat. They are
	// not Unmapped: the bin exists so no *source data* is lost to a Catalog gap
	// (ADR 0002), and filing a notification preference there would be filing a
	// receipt as a measurement. They are still counted, because an import that drops
	// something silently is the failure mode every part of this contract avoids.
	Ignored map[string]int

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
