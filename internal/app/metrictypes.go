package app

import (
	"context"
	"log/slog"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// syncMetricTypes stores the types of the catalog shipped with the binary, so that the metric
// types of the database follow it (Storage and aggregates FR-1).
func syncMetricTypes(ctx context.Context, metricTypes store.MetricTypesRepository, logger *slog.Logger) error {
	shipped, err := catalog.Shipped()
	if err != nil {
		return err
	}

	if err := metricTypes.Sync(ctx, shipped); err != nil {
		return err
	}

	logger.InfoContext(ctx, "metric types synchronized", slog.Int("types", len(shipped.Types())))

	return nil
}
