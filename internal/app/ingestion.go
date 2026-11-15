package app

import (
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// routeDecoderMaxMemory bounds the memory of decoding a route: route.MaxPoints points of 8 columns
// of varints of at most 10 bytes, with their bitmaps, fit in it.
const routeDecoderMaxMemory = 128 << 20

// ingestion holds what the imports and the merges of sources need: the inbox of the pushes, the
// pipeline and the merger.
type ingestion struct {
	inbox    *ingest.Inbox
	pipeline *ingest.Pipeline
	merger   *ingest.Merger
	enc      *zstd.Encoder
	dec      *zstd.Decoder
}

// newIngestion returns the ingestion of st, whose metric types are those of shipped, with the
// inbox of the directory inboxDir. Close releases its codec.
func newIngestion(st store.Store, shipped *catalog.Catalog, inboxDir string, clk clock.Clock) (*ingestion, error) {
	activities, err := catalog.ShippedActivities()
	if err != nil {
		return nil, err
	}

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("create route encoder: %w", err)
	}

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(routeDecoderMaxMemory))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create route decoder: %w", err), enc.Close())
	}

	// The recompute jobs are written without waking a worker: the pipeline runs before the worker
	// exists, and the worker of the import or of the merge claims again as soon as it ends.
	recorder := aggregate.NewRecorder(nil)
	detector := workouts.NewDetector(activities)

	return &ingestion{
		inbox: ingest.NewInbox(inboxDir, clk),
		pipeline: ingest.NewPipeline(ingest.Config{
			Store:       st,
			Catalog:     shipped,
			Activities:  activities,
			Codec:       route.NewCodec(enc, dec),
			DirtyRanges: recorder,
			Duplicates:  detector,
		}),
		merger: ingest.NewMerger(ingest.MergeConfig{
			Store:       st,
			DirtyRanges: recorder,
			Duplicates:  detector,
			Recompute:   recorder,
		}),
		enc: enc,
		dec: dec,
	}, nil
}

// Close releases the codec of the routes.
func (i *ingestion) Close() error {
	i.dec.Close()

	return i.enc.Close()
}
