package app

import (
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// routeDecoderMaxMemory bounds the memory of decoding a route: route.MaxPoints points of 8 columns
// of varints of at most 10 bytes, with their bitmaps, fit in it.
const routeDecoderMaxMemory = 128 << 20

// ingestion holds what the imports need: the inbox of the pushes and the pipeline.
type ingestion struct {
	inbox    *ingest.Inbox
	pipeline *ingest.Pipeline
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

	return &ingestion{
		inbox: ingest.NewInbox(inboxDir, clk),
		pipeline: ingest.NewPipeline(ingest.Config{
			Store:      st,
			Catalog:    shipped,
			Activities: activities,
			Codec:      route.NewCodec(enc, dec),
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
