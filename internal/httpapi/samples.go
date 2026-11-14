package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/preferences"
	"github.com/gauthier-se/verve/internal/store"
)

// rawRejections are the errors of aggregate.ReadRaw for a query it refuses, whose messages are the
// codes of their 422 problems.
var rawRejections = []error{
	aggregate.ErrPeriodInvalid, aggregate.ErrRangeTooLong, aggregate.ErrMaxPointsWithCursor, aggregate.ErrMaxPointsUnsupported,
}

// samplesInput is the input of GET /api/v1/metrics/{type}/samples.
type samplesInput struct {
	Type      string    `path:"type" example:"heart_rate" doc:"The key of the metric type."`
	From      time.Time `query:"from" required:"true" example:"2026-11-10T00:00:00Z" doc:"The first instant of the range, in RFC 3339."`
	To        time.Time `query:"to" required:"true" example:"2026-11-11T00:00:00Z" doc:"The end of the range, excluded, in RFC 3339. It must be after from, and the range at most VERVE_RAW_MAX_DAYS local days long in the timezone of the current user, 7 by default, or the read answers 422 range_too_long."`
	Cursor    string    `query:"cursor" doc:"The nextCursor of the previous page, left out for the first page."`
	Limit     int       `query:"limit" default:"500" minimum:"1" maximum:"5000" doc:"The maximum number of samples of the page."`
	MaxPoints int       `query:"maxPoints" minimum:"2" maximum:"5000" doc:"Returns the whole range downsampled to at most that many samples, by largest triangle three buckets, the first and last kept, without a cursor. For quantity types only: a category type answers 422 max_points_unsupported, and a cursor 422 max_points_with_cursor."`

	// after is the position that Cursor encodes, set by Resolve.
	after store.Cursor
}

// Resolve decodes the cursor, so that Huma reports a malformed one with the other invalid fields.
func (in *samplesInput) Resolve(huma.Context) []error {
	return resolveCursor(in.Cursor, &in.after)
}

// MetricSample is a raw sample of a metric type.
type MetricSample struct {
	ID        int64        `json:"id" example:"1042"`
	Start     time.Time    `json:"start"`
	End       time.Time    `json:"end" doc:"The end of the sample, its start for an instantaneous sample."`
	UTCOffset int          `json:"utcOffset" example:"3600" doc:"The offset of the local time where the sample was recorded, in seconds east of UTC."`
	Value     float64      `json:"value" example:"62" doc:"The value in the canonical unit of the type, 1 for a category sample."`
	Min       *float64     `json:"min,omitempty" doc:"The minimum of a pre-aggregated point, left out for other samples."`
	Max       *float64     `json:"max,omitempty" doc:"The maximum of a pre-aggregated point, left out for other samples."`
	Category  string       `json:"category,omitempty" example:"asleep_core" doc:"The key of the value of a category sample, left out for a quantity."`
	Source    SampleSource `json:"source" doc:"The source of the sample."`

	UsedInAggregates bool `json:"usedInAggregates" doc:"True when the sample counts in the aggregates: its source is the one selected for its local hour, or, for a type in combine mode, a source not ignored."`
}

// SampleSource is the source of a raw sample.
type SampleSource struct {
	ID   int64  `json:"id" example:"3"`
	Name string `json:"name" example:"Apple Watch" doc:"The name shown to the user."`
}

// samplesOutput is the response of GET /api/v1/metrics/{type}/samples.
type samplesOutput struct {
	Body struct {
		Unit  string         `json:"unit" example:"count/min" doc:"The canonical unit of the values."`
		Items []MetricSample `json:"items" nullable:"false" doc:"The samples that start in the range, sorted by start then id."`
		// NextCursor is empty on the last page.
		NextCursor string `json:"nextCursor,omitempty" doc:"The cursor of the next page, left out on the last page and when maxPoints is set."`
	}
}

// registerSamples registers GET /api/v1/metrics/{type}/samples, which reads the raw samples of a
// metric type of the user of the request from deps.Store, over at most deps.RawMaxDays days.
func registerSamples(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-metric-samples",
		Method:      http.MethodGet,
		Path:        "/api/v1/metrics/{type}/samples",
		Summary:     "List the raw samples of a metric",
		Description: "Returns the samples of a metric type of the current user that start in a short range, with " +
			"their source and whether they count in the aggregates, paginated, or downsampled with maxPoints. " +
			"An uncurated type without samples of the user answers 404, as it is not listed to them. Accepts a " +
			"session or a token with the `read` scope.",
		Tags:     []string{"Metrics"},
		Security: scopeSecurity(store.ScopeRead),
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *samplesInput) (*samplesOutput, error) {
		principal, err := requireScope(ctx, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		metricType, err := metricTypeOf(ctx, deps.Store, principal.UserID, in.Type)
		if err != nil {
			return nil, err
		}

		loc, err := preferences.Location(ctx, deps.Store.Preferences(), principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("read timezone: %w", err)
		}

		page, err := aggregate.ReadRaw(ctx, deps.Store, principal.UserID, metricType, loc, aggregate.RawQuery{
			From: in.From, To: in.To, MaxDays: deps.RawMaxDays, After: in.after, Limit: in.Limit, MaxPoints: in.MaxPoints,
		})
		for _, rejected := range rawRejections {
			if errors.Is(err, rejected) {
				return nil, codedProblem(http.StatusUnprocessableEntity, rejected)
			}
		}

		if err != nil {
			return nil, err
		}

		names, err := sourceNames(ctx, deps.Store, principal.UserID)
		if err != nil {
			return nil, err
		}

		out := &samplesOutput{}
		out.Body.Unit = metricType.Unit
		out.Body.Items = make([]MetricSample, 0, len(page.Samples))

		for _, s := range page.Samples {
			out.Body.Items = append(out.Body.Items, MetricSample{
				ID: s.ID, Start: s.Start, End: s.End, UTCOffset: s.UTCOffset, Value: s.Value, Min: s.Min, Max: s.Max,
				Category: s.Category, Source: SampleSource{ID: s.SourceID, Name: names[s.SourceID]},
				UsedInAggregates: s.UsedInAggregates,
			})
		}

		if page.More {
			out.Body.NextCursor = encodeCursor(page.Samples[len(page.Samples)-1].Cursor())
		}

		return out, nil
	})
}

// sourceNames returns the names shown to userID of their sources, by id.
func sourceNames(ctx context.Context, repos store.Repositories, userID int64) (map[int64]string, error) {
	sources, err := repos.Sources().List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}

	names := make(map[int64]string, len(sources))
	for _, source := range sources {
		names[source.ID] = source.DisplayName
	}

	return names, nil
}
