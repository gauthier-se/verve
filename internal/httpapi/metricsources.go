package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/store"
)

// priorityRejections are the errors of aggregate.SetPriorities for priorities it refuses, whose
// messages are the codes of their 422 problems.
var priorityRejections = []error{aggregate.ErrSourceUnknown, aggregate.ErrSourceListedTwice, store.ErrCombineNotDiscrete}

// MetricSource is a source in the priorities of a metric type.
type MetricSource struct {
	ID          int64  `json:"id" example:"3" doc:"The id of the source."`
	DisplayName string `json:"displayName" example:"Ma montre" doc:"The name of the source shown to the user."`
	Kind        string `json:"kind" enum:"watch,phone,scale,ring,app,manual,aggregated,unknown" doc:"The kind of the source."`
}

// MetricPriorities are the resolved priorities of the sources of a metric type.
type MetricPriorities struct {
	Order   []MetricSource `json:"order" nullable:"false" doc:"The sources that are not ignored, highest priority first: those the user ordered, and the others at their default place by kind. Each hour uses the first one with data, unless combine is set."`
	Ignored []MetricSource `json:"ignored" nullable:"false" doc:"The ignored sources, which never contribute."`
	Combine bool           `json:"combine" doc:"True when every source of order contributes, for a discrete type."`
}

// metricSourcesInput is the input of GET /api/v1/metrics/{type}/sources.
type metricSourcesInput struct {
	Type string `path:"type" example:"step_count" doc:"The key of the metric type."`
}

// metricSourcesOutput is the response of the operations on the sources of a metric type.
type metricSourcesOutput struct {
	Body MetricPriorities
}

// setMetricSourcesInput is the input of PUT /api/v1/metrics/{type}/sources.
type setMetricSourcesInput struct {
	Type string `path:"type" example:"step_count" doc:"The key of the metric type."`
	Body struct {
		Order   []int64 `json:"order" nullable:"false" example:"[3, 1]" doc:"The ids of the ordered sources, highest priority first. A source left out of order and ignored takes its default place by kind."`
		Ignored []int64 `json:"ignored" nullable:"false" example:"[2]" doc:"The ids of the ignored sources."`
		Combine bool    `json:"combine" doc:"Makes every source that is not ignored contribute. Only for a discrete type: any other answers 422 combine_not_discrete."`
	}
}

// registerMetricSources registers GET and PUT /api/v1/metrics/{type}/sources, which read and set the
// source priorities of a metric type of the user of the request in deps.Store. Setting them
// recomputes the aggregates of the type through recorder.
func registerMetricSources(api huma.API, deps APIDeps, recorder aggregate.Recorder) {
	huma.Register(api, huma.Operation{
		OperationID: "get-metric-sources",
		Method:      http.MethodGet,
		Path:        "/api/v1/metrics/{type}/sources",
		Summary:     "Get the source priorities of a metric",
		Description: "Returns the priorities of the sources of the current user that have data of the metric type " +
			"or are in its stored priorities: their order, the ignored ones and the combine mode. Without stored " +
			"priorities, the order is the default one. An uncurated type without samples of the user answers " +
			"404. Accepts a session or a token with the `read` scope.",
		Tags:     []string{"Metrics"},
		Security: scopeSecurity(store.ScopeRead),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *metricSourcesInput) (*metricSourcesOutput, error) {
		principal, err := requireScope(ctx, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		metricType, err := metricTypeOf(ctx, deps.Store, principal.UserID, in.Type)
		if err != nil {
			return nil, err
		}

		priorities, err := aggregate.MetricSources(ctx, deps.Store, principal.UserID, metricType)
		if err != nil {
			return nil, err
		}

		return &metricSourcesOutput{Body: metricPrioritiesOf(priorities)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-metric-sources",
		Method:      http.MethodPut,
		Path:        "/api/v1/metrics/{type}/sources",
		Summary:     "Set the source priorities of a metric",
		Description: "Stores the order of the sources of the metric type, the ignored ones and the combine mode, " +
			"and returns the priorities as GET does. The aggregates of the type are recomputed: series show " +
			"`updating` until then. A source of another user answers 422 `source_unknown`, a source listed twice, " +
			"ordered and ignored included, 422 `source_listed_twice`, and the combine mode on a type that is not " +
			"discrete 422 `combine_not_discrete`.",
		Tags:     []string{"Metrics"},
		Security: sessionSecurity(),
		Errors:   []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *setMetricSourcesInput) (*metricSourcesOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		metricType, err := metricTypeOf(ctx, deps.Store, principal.UserID, in.Type)
		if err != nil {
			return nil, err
		}

		priorities, err := aggregate.SetPriorities(ctx, deps.Store, recorder, principal.UserID, metricType,
			store.SourcePriorities{Order: in.Body.Order, Ignored: in.Body.Ignored, Combine: in.Body.Combine})
		for _, rejected := range priorityRejections {
			if errors.Is(err, rejected) {
				return nil, codedProblem(http.StatusUnprocessableEntity, rejected)
			}
		}

		if err != nil {
			return nil, err
		}

		return &metricSourcesOutput{Body: metricPrioritiesOf(priorities)}, nil
	})
}

// metricPrioritiesOf returns priorities as the API describes them.
func metricPrioritiesOf(priorities aggregate.Priorities) MetricPriorities {
	sources := func(list []store.Source) []MetricSource {
		out := make([]MetricSource, 0, len(list))
		for _, source := range list {
			out = append(out, MetricSource{ID: source.ID, DisplayName: source.DisplayName, Kind: string(source.Kind)})
		}

		return out
	}

	return MetricPriorities{Order: sources(priorities.Order), Ignored: sources(priorities.Ignored), Combine: priorities.Combine}
}
