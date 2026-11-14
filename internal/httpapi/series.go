package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/preferences"
	"github.com/gauthier-se/verve/internal/store"
)

// seriesInput is the input of GET /api/v1/metrics/{type}/series.
type seriesInput struct {
	Type      string              `path:"type" example:"step_count" doc:"The key of the metric type."`
	From      time.Time           `query:"from" required:"true" timeFormat:"2006-01-02" format:"date" example:"2026-11-01" doc:"The first local day of the period, in the timezone of the current user, UTC without one."`
	To        time.Time           `query:"to" required:"true" timeFormat:"2006-01-02" format:"date" example:"2026-11-30" doc:"The last local day of the period, included. It must not be before from."`
	Bucket    aggregate.Bucket    `query:"bucket" required:"true" enum:"auto,hour,day,week,month,year" example:"day" doc:"The bucket of the points. Weeks start on Monday. auto picks the smallest bucket that yields at most 400 points. A bucket that yields more than 5,000 points over the period answers 422 too_many_points."`
	Statistic aggregate.Statistic `query:"statistic" required:"true" enum:"sum,mean,min,max,last,count" example:"sum" doc:"The value of each point. A cumulative type accepts sum, and mean, min and max over the daily sums of the bucket for week, month and year. A discrete type accepts mean, min, max and last, and a category type sum, in seconds, and count. Any other answers 422 statistic_unsupported."`
}

// SeriesPoint is the value of a bucket with data.
type SeriesPoint struct {
	Start time.Time `json:"start" doc:"The start of the bucket: a local hour, or the local midnight that starts a day, a week, a month or a year."`
	Value float64   `json:"value" example:"8412" doc:"The statistic of the bucket, in the unit of the series."`
}

// seriesOutput is the response of GET /api/v1/metrics/{type}/series.
type seriesOutput struct {
	Body struct {
		Bucket   aggregate.Bucket `json:"bucket" enum:"hour,day,week,month,year" doc:"The bucket used, the one auto picked for auto."`
		Unit     string           `json:"unit" example:"count" doc:"The canonical unit of the values."`
		Points   []SeriesPoint    `json:"points" nullable:"false" doc:"The points of the buckets with data in the period, oldest first. Empty buckets are left out."`
		Updating bool             `json:"updating" doc:"True while the aggregates of the period are being recomputed, such as after an import: the points may change."`
	}
}

// registerSeries registers GET /api/v1/metrics/{type}/series, which reads the series of a metric
// type of the user of the request from the aggregates of deps.Store, in their timezone.
func registerSeries(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-metric-series",
		Method:      http.MethodGet,
		Path:        "/api/v1/metrics/{type}/series",
		Summary:     "Read a metric as a series",
		Description: "Returns the points of a metric type over a period of local days, from the hourly and daily " +
			"aggregates of the current user. A curated type without samples answers no points, and an uncurated " +
			"type without samples of the user answers 404, as it is not listed to them. Accepts a session or a " +
			"token with the `read` scope.",
		Tags:     []string{"Metrics"},
		Security: scopeSecurity(store.ScopeRead),
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *seriesInput) (*seriesOutput, error) {
		principal, err := requireScope(ctx, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		metricType, err := listedType(ctx, deps.Store, principal.UserID, in.Type)
		if errors.Is(err, store.ErrNotFound) {
			return nil, huma.NewError(http.StatusNotFound, "")
		}

		if err != nil {
			return nil, err
		}

		loc, err := preferences.Location(ctx, deps.Store.Preferences(), principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("read timezone: %w", err)
		}

		series, err := aggregate.ReadSeries(ctx, deps.Store, principal.UserID, metricType, loc, aggregate.SeriesQuery{
			From: in.From, To: in.To, Bucket: in.Bucket, Statistic: in.Statistic,
		})
		if errors.Is(err, aggregate.ErrPeriodInvalid) || errors.Is(err, aggregate.ErrTooManyPoints) ||
			errors.Is(err, aggregate.ErrStatisticUnsupported) {
			return nil, codedProblem(http.StatusUnprocessableEntity, err)
		}

		if err != nil {
			return nil, err
		}

		out := &seriesOutput{}
		out.Body.Bucket = series.Bucket
		out.Body.Unit = metricType.Unit
		out.Body.Updating = series.Updating
		out.Body.Points = make([]SeriesPoint, 0, len(series.Points))

		for _, p := range series.Points {
			out.Body.Points = append(out.Body.Points, SeriesPoint{Start: p.Start, Value: p.Value})
		}

		return out, nil
	})
}

// listedType returns the metric type of key if it is listed to userID: a curated type, or an
// uncurated type they have samples of (FR-2 of Storage and aggregates), and store.ErrNotFound
// otherwise.
func listedType(ctx context.Context, repos store.Repositories, userID int64, key string) (store.MetricType, error) {
	metricType, err := repos.MetricTypes().GetByKey(ctx, key)
	if err != nil {
		return store.MetricType{}, fmt.Errorf("get metric type: %w", err)
	}

	if metricType.Curated {
		return metricType, nil
	}

	stats, err := repos.Samples().Stats(ctx, userID)
	if err != nil {
		return store.MetricType{}, fmt.Errorf("list sample stats: %w", err)
	}

	if !slices.ContainsFunc(stats, func(s store.SampleStats) bool { return s.TypeID == metricType.ID }) {
		return store.MetricType{}, store.ErrNotFound
	}

	return metricType, nil
}
