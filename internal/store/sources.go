package store

import (
	"context"
	"time"
)

// SourceKind is what produced the data of a source (FR-2 of Deduplication and source priority).
// The default priorities order sources by kind.
type SourceKind string

// The kinds of a source.
const (
	SourceWatch SourceKind = "watch"
	SourcePhone SourceKind = "phone"
	SourceScale SourceKind = "scale"
	SourceRing  SourceKind = "ring"
	SourceApp   SourceKind = "app"
	// SourceAggregated is the kind of the synthetic sources of data that HealthKit already
	// deduplicated across devices, such as summarized Health Auto Export points.
	SourceAggregated SourceKind = "aggregated"
	SourceUnknown    SourceKind = "unknown"
)

// Known reports whether k is one of the kinds of a source.
func (k SourceKind) Known() bool {
	switch k {
	case SourceWatch, SourcePhone, SourceScale, SourceRing, SourceApp, SourceAggregated, SourceUnknown:
		return true
	default:
		return false
	}
}

// SourceIdentity identifies a source of a user: its name, compared as received, and its device
// model, such as Watch or iPhone, empty when unknown.
type SourceIdentity struct {
	Name        string
	DeviceModel string
}

// SourceInfo is the source of a record, as an adapter receives it. Every field but Name may be
// empty.
type SourceInfo struct {
	Name string
	// DeviceModel is the model of the HealthKit device, such as Watch or iPhone.
	DeviceModel     string
	BundleID        string
	DeviceName      string
	Manufacturer    string
	HardwareVersion string
	SoftwareVersion string
}

// Source is a device or an app that produced data of a user. Its identity is its Name and
// DeviceModel. Times are in UTC with millisecond precision.
type Source struct {
	ID     int64
	UserID int64
	Name   string
	// DeviceModel is empty while no record of the source gave one.
	DeviceModel string
	// DisplayName is the name shown to the user: Name until the user renames the source.
	DisplayName string
	// BundleID, DeviceName, Manufacturer, HardwareVersion and SoftwareVersion are those of the latest
	// record that gave them.
	BundleID        string
	DeviceName      string
	Manufacturer    string
	HardwareVersion string
	SoftwareVersion string
	Kind            SourceKind
	// KindSetByUser is true once the user set Kind, which Resolve then keeps.
	KindSetByUser bool
	// FirstSeenAt is the time given to the Resolve that created the source, and LastSeenAt the latest
	// time given to a Resolve of it.
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// SourcesRepository stores the sources of each user and the aliases of merged sources (FR-2 and
// FR-3 of Deduplication and source priority). A source or an alias of another user is never
// returned, and answers ErrNotFound.
type SourcesRepository interface {
	// Resolve returns the source of userID that info matches, seen at seenAt, creating it with kind
	// when there is none. It writes, so that a pipeline resolves each source once per batch, not
	// once per record.
	//
	// The identities of the sources and of the aliases of userID are matched by the rule of FR-2:
	//   - info with a device model matches the identity of its name and model, otherwise the
	//     identity of its name without model, which then takes the model, otherwise it creates a
	//     source of its name and model;
	//   - info without device model matches the only source of its name, otherwise the identity of
	//     its name without model, otherwise it creates a source of its name without model.
	//
	// An alias matched returns its source. A matched source keeps its FirstSeenAt, and its
	// LastSeenAt never moves back. When seenAt is not before LastSeenAt, the details of info that
	// are not empty, and kind unless it is SourceUnknown, replace those of the source. Otherwise
	// they only fill the empty ones, and kind only replaces SourceUnknown. A kind set by the user
	// is always kept.
	//
	// It returns ErrInvalid when the name of info is empty or kind not Known, and ErrNotFound when
	// no user has userID.
	Resolve(ctx context.Context, userID int64, info SourceInfo, kind SourceKind, seenAt time.Time) (Source, error)
	// AddAlias makes identity an alias of the source of userID whose id is sourceID: Resolve of
	// identity then returns that source. It returns ErrNotFound when no source of userID has
	// sourceID, ErrConflict when a source or an alias of userID has identity, and ErrInvalid when
	// its name is empty.
	AddAlias(ctx context.Context, userID int64, identity SourceIdentity, sourceID int64) error
	// Get returns the source of userID whose id is id, or ErrNotFound.
	Get(ctx context.Context, userID, id int64) (Source, error)
	// List returns the sources of userID, sorted by id, or an empty list.
	List(ctx context.Context, userID int64) ([]Source, error)
	// Rename sets the DisplayName of the source of userID whose id is id, and returns the source.
	// Its identity does not change. It returns ErrNotFound when no source of userID has id, and
	// ErrInvalid when displayName is empty.
	Rename(ctx context.Context, userID, id int64, displayName string) (Source, error)
	// SetKind sets the Kind of the source of userID whose id is id, as set by the user, and returns
	// the source. It returns ErrNotFound when no source of userID has id, and ErrInvalid when kind
	// is not Known.
	SetKind(ctx context.Context, userID, id int64, kind SourceKind) (Source, error)
}
