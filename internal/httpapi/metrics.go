package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// Metric is a metric type, with the data availability of the user of the request. Its names are not
// returned: the web app translates its key and the keys of its values.
type Metric struct {
	Key          string              `json:"key" example:"heart_rate" doc:"The key of the type, which names it in the API and in the translations."`
	Identifier   string              `json:"identifier" example:"HKQuantityTypeIdentifierHeartRate" doc:"The identifier received for the type: the HealthKit identifier, or the name an uncurated type was first seen with."`
	Kind         string              `json:"kind" enum:"quantity,category" doc:"The kind of the samples of the type."`
	Unit         string              `json:"unit" example:"count/min" doc:"The canonical unit of the values. A category type has the unit count."`
	Aggregation  string              `json:"aggregation" enum:"cumulative,discrete" doc:"How the values add up: cumulative values are summed, such as steps, discrete ones averaged, such as heart rate."`
	Group        string              `json:"group" enum:"activity,heart,body,sleep,nutrition,respiratory,vitals,mobility,hearing,cycle,symptoms,other" doc:"The display group of the type."`
	Curated      bool                `json:"curated" doc:"False for a type that the catalog does not list, created when its data was first seen."`
	Values       []string            `json:"values" nullable:"false" example:"[\"asleep_core\"]" doc:"The keys of the values of a category type that the catalog lists, in the order of the catalog. Empty for a quantity type and an uncurated type, whose samples keep the values as received."`
	Availability *MetricAvailability `json:"availability" doc:"The data of the current user for the type, or null when they have no sample of it."`
}

// MetricAvailability is the data of a user for a metric type.
type MetricAvailability struct {
	_ struct{} `nullable:"true"`

	FirstAt time.Time `json:"firstAt" doc:"The start of the first sample."`
	LastAt  time.Time `json:"lastAt" doc:"The start of the last sample."`
	Count   int64     `json:"count" example:"1250" doc:"The number of samples."`
}

// metricsOutput is the response of GET /api/v1/metrics.
type metricsOutput struct {
	Body struct {
		Items []Metric `json:"items" nullable:"false" doc:"The curated types, and the uncurated types the current user has samples of, sorted by id."`
	}
}

// registerMetrics registers GET /api/v1/metrics, which lists the metric types of deps.Store with the
// data availability of the user of the request, and their category values from deps.Catalog.
func registerMetrics(api huma.API, deps APIDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-metrics",
		Method:      http.MethodGet,
		Path:        "/api/v1/metrics",
		Summary:     "List the metric types",
		Description: "Returns every curated metric type, and the uncurated types the current user has samples of, " +
			"with the first and last start and the count of their samples. Accepts a session or a token with " +
			"the `read` scope.",
		Tags:     []string{"Metrics"},
		Security: scopeSecurity(store.ScopeRead),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*metricsOutput, error) {
		principal, err := requireScope(ctx, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		metrics, err := listMetrics(ctx, deps.Store, deps.Catalog, principal.UserID)
		if err != nil {
			return nil, err
		}

		out := &metricsOutput{}
		out.Body.Items = metrics

		return out, nil
	})
}

// listMetrics returns the metric types of repos that userID sees: every curated type, and the
// uncurated types they have samples of, with their data availability and the values that c lists.
func listMetrics(ctx context.Context, repos store.Repositories, c *catalog.Catalog, userID int64) ([]Metric, error) {
	types, err := repos.MetricTypes().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list metric types: %w", err)
	}

	stats, err := repos.Samples().Stats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sample stats: %w", err)
	}

	byType := make(map[int64]store.SampleStats, len(stats))
	for _, s := range stats {
		byType[s.TypeID] = s
	}

	metrics := make([]Metric, 0, len(types))

	for _, t := range types {
		s, hasSamples := byType[t.ID]
		if !t.Curated && !hasSamples {
			continue
		}

		metric := Metric{
			Key:         t.Key,
			Identifier:  t.Identifier,
			Kind:        string(t.Kind),
			Unit:        t.Unit,
			Aggregation: string(t.Aggregation),
			Group:       string(t.Group),
			Curated:     t.Curated,
			Values:      valueKeys(c, t),
		}

		if hasSamples {
			metric.Availability = &MetricAvailability{FirstAt: s.FirstAt, LastAt: s.LastAt, Count: s.Count}
		}

		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// valueKeys returns the keys of the values that c lists for t, empty for an uncurated type.
func valueKeys(c *catalog.Catalog, t store.MetricType) []string {
	keys := []string{}
	if !t.Curated {
		return keys
	}

	listed, status := c.Lookup(t.Identifier)
	if status != catalog.Known {
		return keys
	}

	for _, v := range listed.Values {
		keys = append(keys, v.Key)
	}

	return keys
}
