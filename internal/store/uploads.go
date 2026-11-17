package store

import (
	"context"
	"time"
)

// NewUpload describes an incomplete resumable upload to create (FR-2 of XML import).
type NewUpload struct {
	// ID is random, and names the file of the upload.
	ID     string
	UserID int64
	// FileName is the file name that the client gave, kept for display only, or empty for none.
	FileName string
	// Length is the size of the whole upload in bytes, at least 1.
	Length    int64
	ExpiresAt time.Time
}

// Upload is an incomplete resumable upload of a user. Times are in UTC with millisecond precision.
type Upload struct {
	ID     string
	UserID int64
	// FileName is empty when the client gave none.
	FileName string
	Length   int64
	// Offset is the number of bytes received and synced, from 0 to Length.
	Offset int64
	// CreatedAt is the time of Create.
	CreatedAt time.Time
	// ExpiresAt is the time from which the upload is expired. The store keeps an expired upload
	// until DeleteExpired: its callers check the expiry.
	ExpiresAt time.Time
}

// UploadsRepository stores the incomplete uploads of each user. An upload of another user is never
// returned: it answers ErrNotFound, as an unknown id does. Deleting a user removes their uploads.
type UploadsRepository interface {
	// Create stores an upload at offset 0 with the current time as CreatedAt, and returns it. It
	// returns ErrInvalid for an empty id or a length below 1, ErrConflict for a taken id, and
	// ErrNotFound when no user has its UserID.
	Create(ctx context.Context, upload NewUpload) (Upload, error)
	// Get returns the upload of id of userID, or ErrNotFound.
	Get(ctx context.Context, userID int64, id string) (Upload, error)
	// Advance moves the offset of the upload of id of userID from from to to, and its expiry to
	// expiresAt, and returns it. It returns ErrNotFound for a missing upload, ErrConflict when its
	// offset is not from, and ErrInvalid when to is below from or past its length, leaving it as
	// it was.
	Advance(ctx context.Context, userID int64, id string, from, to int64, expiresAt time.Time) (Upload, error)
	// Delete removes the upload of id of userID, or returns ErrNotFound.
	Delete(ctx context.Context, userID int64, id string) error
	// DeleteExpired removes the uploads of every user that expire at the current time or before, and
	// returns their ids.
	DeleteExpired(ctx context.Context) ([]string, error)
	// IDs returns the ids of every upload, whatever their user.
	IDs(ctx context.Context) ([]string, error)
}
