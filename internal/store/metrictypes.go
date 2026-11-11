package store

import (
	"context"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
)

// MetricType is a metric type of the instance: a type of the catalog, or an uncurated type created
// on first sight (FR-1 and FR-2 of Storage and aggregates). Its id never changes, since samples
// reference it.
type MetricType struct {
	ID int64
	// Key names the type in the API, such as heart_rate.
	Key string
	// Identifier is the identifier received for the type, such as HKQuantityTypeIdentifierHeartRate.
	Identifier  string
	Kind        catalog.Kind
	Unit        string
	Aggregation catalog.Aggregation
	Group       catalog.Group
	// Curated is false for a type that the catalog does not list.
	Curated bool
	// CreatedAt is when the type was first stored, in UTC with millisecond precision.
	CreatedAt time.Time
}

// MetricTypesRepository stores the metric types. They are instance-wide and hold no health data.
// The category values stay in the catalog.
type MetricTypesRepository interface {
	// Sync writes the types of c, in one transaction, matched by identifier: a missing type is
	// created with the current time as CreatedAt, and a stored one, uncurated included, takes the
	// attributes of the catalog and becomes curated, keeping its id and CreatedAt. A stored type
	// that c no longer lists becomes uncurated and keeps its attributes. A type that is not in c
	// and whose key c gives to another type gets a new key, derived as by EnsureUncurated.
	Sync(ctx context.Context, c *catalog.Catalog) error
	// EnsureUncurated returns the type of identifier. When no type has it, it first creates one,
	// uncurated, of kind and unit, discrete, in the group other, with the current time as
	// CreatedAt and catalog.UncuratedKey(identifier) as key, followed by _2, _3 and so on when
	// another type has that key. It returns ErrInvalid when identifier is empty or kind unknown.
	EnsureUncurated(ctx context.Context, identifier string, kind catalog.Kind, unit string) (MetricType, error)
	// Get returns the type of id, or ErrNotFound.
	Get(ctx context.Context, id int64) (MetricType, error)
	// GetByKey returns the type of key, or ErrNotFound.
	GetByKey(ctx context.Context, key string) (MetricType, error)
	// GetByIdentifier returns the type of identifier, or ErrNotFound.
	GetByIdentifier(ctx context.Context, identifier string) (MetricType, error)
	// List returns every type, sorted by id.
	List(ctx context.Context) ([]MetricType, error)
}
