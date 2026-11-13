package store

import (
	"context"
	"time"
)

// ImportStatus is the state of an import (FR-9 of Ingestion pipeline).
type ImportStatus string

// The statuses of an import.
const (
	ImportQueued    ImportStatus = "queued"
	ImportRunning   ImportStatus = "running"
	ImportCompleted ImportStatus = "completed"
	ImportFailed    ImportStatus = "failed"
	ImportCancelled ImportStatus = "cancelled"
)

// Finished reports whether s is the status of an import that ended: completed, failed or cancelled.
func (s ImportStatus) Finished() bool {
	switch s {
	case ImportCompleted, ImportFailed, ImportCancelled:
		return true
	default:
		return false
	}
}

const (
	// ImportHistoryLimit is the number of newest imports of a user among which a finished import is
	// kept (FR-11 of Ingestion pipeline).
	ImportHistoryLimit = 200
	// ImportErrorsLimit is the most sample error messages that a report keeps (FR-3 of Ingestion
	// pipeline).
	ImportErrorsLimit = 100
)

// ImportCounters counts the records of an import by what happened to them (FR-3 of Ingestion
// pipeline).
type ImportCounters struct {
	Received    int64
	Inserted    int64
	Updated     int64
	Duplicates  int64
	Blocked     int64
	Invalid     int64
	Unsupported int64
	Suspicious  int64
	Warnings    int64
	Deleted     int64
	NotFound    int64
}

// ImportReport is the report of an import: its counters in total, per type key and for the
// workouts, and its sample error messages, which name a record and a reason, never a value.
type ImportReport struct {
	Total ImportCounters
	// Types holds the counters of each type key, nil when there are none.
	Types    map[string]ImportCounters
	Workouts ImportCounters
	// Errors holds at most ImportErrorsLimit messages, nil when there are none.
	Errors []string
}

// ImportProgress is what the transaction of a batch stores on its import (FR-5 of Ingestion
// pipeline).
type ImportProgress struct {
	// Position is the position of the next record, defined by the adapter: opaque to the store, and
	// at least 0.
	Position int64
	Report   ImportReport
}

// NewImport describes an import to create.
type NewImport struct {
	UserID int64
	Origin Origin
	// TokenID is the API token that pushed the import, or 0 for none. It references nothing, so that
	// revoking a token keeps the history.
	TokenID int64
	// Client is the metadata of the client that sent the import, nil for none.
	Client map[string]string
	// InboxFile is the name of the inbox file that holds the payload of the import, or empty for an
	// import without one.
	InboxFile string
}

// Import is an import of a user and its report. Times are in UTC with millisecond precision.
type Import struct {
	ID      int64
	UserID  int64
	Origin  Origin
	TokenID int64
	// Client is nil when the import has no client metadata.
	Client map[string]string
	// InboxFile is empty for an import without inbox file.
	InboxFile string
	Status    ImportStatus
	Position  int64
	Report    ImportReport
	// CreatedAt is the time of Create.
	CreatedAt time.Time
	// StartedAt is the time the import first ran, or zero when it never did.
	StartedAt time.Time
	// FinishedAt is the time the import ended, or zero when it did not.
	FinishedAt time.Time
}

// Cursor returns the position of the import in the lists of ImportsRepository.List.
func (i Import) Cursor() Cursor {
	return Cursor{At: i.CreatedAt, ID: i.ID}
}

// ImportsRepository stores the imports of each user and their reports. An import of another user
// is never returned: it answers ErrNotFound, as an unknown id does.
type ImportsRepository interface {
	// Create stores a queued import with the current time as CreatedAt, and returns it. In the same
	// transaction, it removes the finished imports of the user that are not among their
	// ImportHistoryLimit newest ones: queued and running imports are never removed. It returns
	// ErrNotFound when no user has its UserID, and ErrInvalid when its origin is unknown.
	Create(ctx context.Context, imp NewImport) (Import, error)
	// Get returns the import of id of userID, or ErrNotFound.
	Get(ctx context.Context, userID, id int64) (Import, error)
	// List returns the imports of userID, newest first by CreatedAt then ID: at most page.Limit of
	// them, after page.After. It returns ErrInvalid when page.Limit is below 1.
	List(ctx context.Context, userID int64, page PageRequest) ([]Import, error)
	// SetStatus changes the status of the import of id of userID, and returns it. Running sets its
	// StartedAt to the current time, or keeps it for an import already running, which a resumed job
	// runs again. A finished status sets its FinishedAt. It returns ErrNotFound for a missing import,
	// ErrConflict for a finished one, which keeps its status, and ErrInvalid for the status queued or
	// an unknown one.
	SetStatus(ctx context.Context, userID, id int64, status ImportStatus) (Import, error)
	// SaveProgress replaces the position and the report of the running import of id of userID with
	// progress, keeping the first ImportErrorsLimit error messages. Called in the transaction of the
	// writes of a batch, it commits or rolls back with them. It returns ErrNotFound for a missing
	// import, ErrConflict for one that is not running, and ErrInvalid for a negative position.
	SaveProgress(ctx context.Context, userID, id int64, progress ImportProgress) error
	// SetClient replaces the client metadata of the running import of id of userID with client, or
	// removes it when client is empty: a push stores its client once its job reads the payload. It
	// returns ErrNotFound for a missing import and ErrConflict for one that is not running.
	SetClient(ctx context.Context, userID, id int64, client map[string]string) error
	// InboxFilesKept returns the inbox files of the imports, whatever their user, except those of the
	// imports that failed before failedSince: the file of a failed import is kept for a while, for a
	// retry or a diagnosis.
	InboxFilesKept(ctx context.Context, failedSince time.Time) ([]string, error)
	// LastByOrigin returns the FinishedAt of the last completed import of userID for each origin.
	// The origins without a completed import are left out.
	LastByOrigin(ctx context.Context, userID int64) (map[Origin]time.Time, error)
	// DeleteByUser removes at most limit imports of userID, whatever their status, and returns how
	// many it removed. It returns ErrInvalid when limit is below 1.
	DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error)
}
