package audit

import (
	"context"
	"fmt"
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
	// Days of the calendar rather than a Duration, which a long retention would overflow. UTC days
	// all last 24 hours.
	before := now.UTC().AddDate(0, 0, -retentionDays)

	for {
		purged, err := events.PurgeBefore(ctx, before, purgeBatchSize)
		if err != nil {
			return fmt.Errorf("purge audit events older than %d days: %w", retentionDays, err)
		}

		if purged < purgeBatchSize {
			return nil
		}
	}
}
