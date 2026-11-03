package audit

import (
	"context"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// purgeBatchSize is the most events that one transaction of the purge removes, so that the purge
// never holds the writer for long.
const purgeBatchSize = 5000

// Purge removes the events whose last occurrence is more than retentionDays days before now, in
// batches of at most purgeBatchSize events. Each batch is a transaction of its own only when events
// is the repository of the store, never the one of a WithTx.
func Purge(ctx context.Context, events store.AuditEventsRepository, now time.Time, retentionDays int) error {
	// UTC days all last 24 hours. The configuration keeps retentionDays within 100 years, so that the
	// cutoff never overflows into the future.
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)

	for {
		purged, err := events.PurgeBefore(ctx, cutoff, purgeBatchSize)
		if err != nil {
			return err
		}

		if purged < purgeBatchSize {
			return nil
		}
	}
}
