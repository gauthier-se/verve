package archive

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/gauthier-se/verve/internal/data"
)

// ModelSource is the one Source over the storage layer: the adapter that knows
// both the models and the format. Everything above it (the CLI command, the
// handler) hands Write a Source and knows neither.
//
// It is constructed from a Models rather than from a *sql.DB so that a caller
// inside a transaction passes the bound Models and gets a consistent snapshot,
// which is exactly how both callers use it (see the CLI's export command).
type ModelSource struct {
	models       data.Models
	accountID    int64
	artifactsDir string
}

// NewModelSource wires a Source to one Account.
func NewModelSource(models data.Models, accountID int64, artifactsDir string) ModelSource {
	return ModelSource{models: models, accountID: accountID, artifactsDir: artifactsDir}
}

func (s ModelSource) Counts(ctx context.Context) (data.ExportCounts, error) {
	return s.models.ExportCounts(ctx, s.accountID)
}

// Profile reads the three static account fields. It answers nil when the Account
// has none of them, so an Archive from an account that never imported an Apple
// `Me` record carries no empty object.
func (s ModelSource) Profile(ctx context.Context) (*Profile, error) {
	acc, err := s.models.Accounts.GetByID(ctx, s.accountID)
	if err != nil {
		return nil, err
	}
	p := Profile{
		DateOfBirth:   deref(acc.DateOfBirth),
		BiologicalSex: deref(acc.BiologicalSex),
		BloodType:     deref(acc.BloodType),
	}
	if p == (Profile{}) {
		return nil, nil
	}
	return &p, nil
}

func (s ModelSource) Measurements(ctx context.Context, after data.MeasurementCursor, limit int) ([]data.Measurement, data.MeasurementCursor, error) {
	return s.models.Measurements.ExportPage(ctx, s.accountID, after, limit)
}

func (s ModelSource) States(ctx context.Context, after data.StateCursor, limit int) ([]data.State, data.StateCursor, error) {
	return s.models.States.ExportPage(ctx, s.accountID, after, limit)
}

func (s ModelSource) Sessions(ctx context.Context, after data.SessionCursor, limit int) ([]data.ExportSession, data.SessionCursor, error) {
	return s.models.Sessions.ExportPage(ctx, s.accountID, after, limit)
}

func (s ModelSource) Unmapped(ctx context.Context, after data.UnmappedCursor, limit int) ([]data.UnmappedRecord, data.UnmappedCursor, error) {
	return s.models.Imports.ExportUnmappedPage(ctx, s.accountID, after, limit)
}

func (s ModelSource) Imports(ctx context.Context) ([]data.Import, error) {
	return s.models.Imports.ExportImports(ctx, s.accountID)
}

// OpenArtifact opens a stored GPX by name. filepath.Base is the same guard the
// route download uses: the name comes from a row this Account owns, and it still
// never becomes a path.
func (s ModelSource) OpenArtifact(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.artifactsDir, filepath.Base(name)))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
