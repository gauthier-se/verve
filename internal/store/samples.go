package store

import (
	"context"
	"encoding/json"
	"time"
)

// Origin is the way the data of an import arrived (FR-9 of Ingestion pipeline).
type Origin string

// The origins of an import.
const (
	OriginXML    Origin = "xml"
	OriginHAE    Origin = "hae"
	OriginNative Origin = "native"
	OriginCLI    Origin = "cli"
)

// Known reports whether o is one of the origins of an import.
func (o Origin) Known() bool {
	switch o {
	case OriginXML, OriginHAE, OriginNative, OriginCLI:
		return true
	default:
		return false
	}
}

// ConflictRule is what an upsert does with a sample whose natural key is already stored (FR-1 of
// Deduplication and source priority).
type ConflictRule string

// The conflict rules of a sample.
const (
	// ConflictIgnore keeps the stored sample: the sample is a duplicate.
	ConflictIgnore ConflictRule = "ignore"
	// ConflictUpdateIfChanged replaces the value, min, max and category value of the stored sample
	// when one of them differs: the sample is then updated, and otherwise a duplicate.
	ConflictUpdateIfChanged ConflictRule = "update_if_changed"
	// ConflictReplace replaces the value, min and max of the stored sample: the sample is updated.
	ConflictReplace ConflictRule = "replace"
)

// Known reports whether r is one of the conflict rules.
func (r ConflictRule) Known() bool {
	switch r {
	case ConflictIgnore, ConflictUpdateIfChanged, ConflictReplace:
		return true
	default:
		return false
	}
}

// Sample is a quantity or category sample of a user (FR-3 of Storage and aggregates). Times are in
// UTC with millisecond precision.
type Sample struct {
	ID       int64
	UserID   int64
	TypeID   int64
	SourceID int64
	Start    time.Time
	End      time.Time
	// UTCOffset is the offset of the local time where the sample was recorded, in seconds east of
	// UTC.
	UTCOffset int
	// Value is in the canonical unit of the type.
	Value float64
	// Min and Max are those of a pre-aggregated point, nil for other samples.
	Min *float64
	Max *float64
	// Category is the key of the category value of a category sample, empty for a quantity.
	Category string
	Origin   Origin
	// ImportID is the import that stored the sample. It references nothing.
	ImportID int64
	// UUID is the external uuid of the sample, empty when it has none.
	UUID string
	// NaturalKey is the hash that identifies the sample among those of its user. The natural key of
	// a sample with a uuid is derived from the uuid.
	NaturalKey []byte
	// Metadata is a JSON object, nil when the sample has none.
	Metadata json.RawMessage
}

// Cursor returns the position of the sample in the lists of SamplesRepository.List.
func (s Sample) Cursor() Cursor {
	return Cursor{At: s.Start, ID: s.ID}
}

// NewSample is a sample to upsert, with the rule to apply when its natural key is already stored.
// Its ID and UserID are ignored.
type NewSample struct {
	Sample

	OnConflict ConflictRule
}

// UpsertResult counts the samples of an upsert by what happened to them.
type UpsertResult struct {
	Inserted   int
	Updated    int
	Duplicates int
}

// SampleStats is the data availability of a user for a type: the first and last start of their
// samples, and how many there are.
type SampleStats struct {
	TypeID  int64
	FirstAt time.Time
	LastAt  time.Time
	Count   int64
}

// SamplesQuery asks for the samples of a type whose start is in [From, To), sorted oldest first, by
// start then id, after a position.
type SamplesQuery struct {
	TypeID int64
	From   time.Time
	To     time.Time
	// After is the position of the last sample of the previous page, or zero for the first page.
	After Cursor
	// Limit is the maximum number of samples, at least 1.
	Limit int
}

// SamplesRepository stores the samples of each user and their data availability. A sample of
// another user is never returned.
type SamplesRepository interface {
	// Upsert stores samples for userID in one transaction, in order, and counts them. A sample
	// whose natural key userID has not stored yet is inserted, and otherwise handled by its
	// OnConflict rule: a sample earlier in samples counts as stored.
	//
	// It returns ErrInvalid, and writes nothing of samples, when a sample has an empty natural key,
	// an unknown rule or origin, an end before its start, metadata that is not a JSON object, a type
	// that does not exist, or a source that userID does not have.
	Upsert(ctx context.Context, userID int64, samples []NewSample) (UpsertResult, error)
	// Stats returns the data availability of userID for each type they have samples of, sorted by
	// type id, or an empty list.
	Stats(ctx context.Context, userID int64) ([]SampleStats, error)
	// List returns the samples of userID that query asks for. It returns ErrInvalid when the limit
	// is below 1 or To is not after From.
	List(ctx context.Context, userID int64, query SamplesQuery) ([]Sample, error)
	// DeleteByUser removes at most limit rows of the samples and data availability of userID, the
	// samples first, and returns how many it removed. The data availability is removed once no
	// sample is left, so it may count samples already removed meanwhile. It returns ErrInvalid when
	// limit is below 1.
	DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error)
}
