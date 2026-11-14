package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/store"
)

// Source is a device or an app that produced data of the user of the request.
type Source struct {
	ID            int64     `json:"id" example:"3" doc:"The id of the source."`
	DisplayName   string    `json:"displayName" example:"Ma montre" doc:"The name shown to the user: the name until they rename the source."`
	Name          string    `json:"name" example:"Apple Watch de Camille" doc:"The name received with the data of the source. With the device model, it identifies the source."`
	DeviceModel   string    `json:"deviceModel" example:"Watch" doc:"The model of the device, such as Watch or iPhone, empty when unknown."`
	Kind          string    `json:"kind" enum:"watch,phone,scale,ring,app,manual,aggregated,unknown" doc:"What produced the data, which places the source in the default priorities. Detected from the data until the user sets it."`
	KindSetByUser bool      `json:"kindSetByUser" doc:"True once the user set the kind, which the data then never changes."`
	FirstSeenAt   time.Time `json:"firstSeenAt" doc:"When the source was first seen, from the time of its records. The web app tells new sources from it."`
	LastSeenAt    time.Time `json:"lastSeenAt" doc:"When the source was last seen, from the time of its records."`
	SampleCount   int64     `json:"sampleCount" example:"125000" doc:"The number of samples of the source, every metric type together."`
}

// sourcesOutput is the response of GET /api/v1/sources.
type sourcesOutput struct {
	Body struct {
		Items []Source `json:"items" nullable:"false" doc:"The sources of the current user, sorted by id."`
	}
}

// sourceOutput is the response of PATCH /api/v1/sources/{id}.
type sourceOutput struct {
	Body Source
}

// updateSourceInput is the input of PATCH /api/v1/sources/{id}.
type updateSourceInput struct {
	ID   int64 `path:"id" doc:"The id of the source."`
	Body struct {
		DisplayName *string `json:"displayName,omitempty" minLength:"1" maxLength:"64" example:"Ma montre" doc:"The name shown to the user, of 1 to 64 characters. Left out, it is unchanged."`
		Kind        *string `json:"kind,omitempty" enum:"watch,phone,scale,ring,app,manual,aggregated,unknown" doc:"The kind of the source, which the data then never changes. Left out, it is unchanged."`
	}
}

// registerSources registers GET /api/v1/sources and PATCH /api/v1/sources/{id}, which list and
// change the sources of the user of the request in deps.Store. A change of kind recomputes the
// aggregates through the jobs of deps.Jobs.
func registerSources(api huma.API, deps APIDeps) {
	recorder := aggregate.NewRecorder(deps.Jobs)

	huma.Register(api, huma.Operation{
		OperationID: "list-sources",
		Method:      http.MethodGet,
		Path:        "/api/v1/sources",
		Summary:     "List the sources",
		Description: "Returns the devices and apps that produced data of the current user, with their sample " +
			"count. Accepts a session or a token with the `read` scope.",
		Tags:     []string{"Sources"},
		Security: scopeSecurity(store.ScopeRead),
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*sourcesOutput, error) {
		principal, err := requireScope(ctx, store.ScopeRead)
		if err != nil {
			return nil, err
		}

		sources, err := deps.Store.Sources().List(ctx, principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("list sources: %w", err)
		}

		counts, err := deps.Store.Sources().SampleCounts(ctx, principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("count samples of the sources: %w", err)
		}

		out := &sourcesOutput{}
		out.Body.Items = make([]Source, 0, len(sources))

		for _, source := range sources {
			out.Body.Items = append(out.Body.Items, sourceOf(source, counts[source.ID]))
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-source",
		Method:      http.MethodPatch,
		Path:        "/api/v1/sources/{id}",
		Summary:     "Change a source",
		Description: "Renames a source of the current user, or sets its kind, and returns the source. A new " +
			"display name recomputes nothing. A new kind recomputes the aggregates of the metric types that the " +
			"source has samples of, since it places the source in their priorities: series show `updating` " +
			"until then. A source of another user answers 404.",
		Tags:     []string{"Sources"},
		Security: sessionSecurity(),
		Errors:   []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *updateSourceInput) (*sourceOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		change := aggregate.SourceChange{DisplayName: in.Body.DisplayName}
		if in.Body.Kind != nil {
			kind := store.SourceKind(*in.Body.Kind)
			change.Kind = &kind
		}

		source, err := aggregate.ChangeSource(ctx, deps.Store, recorder, principal.UserID, in.ID, change)
		if errors.Is(err, store.ErrNotFound) {
			return nil, huma.NewError(http.StatusNotFound, "")
		}

		if err != nil {
			return nil, err
		}

		counts, err := deps.Store.Sources().SampleCounts(ctx, principal.UserID)
		if err != nil {
			return nil, fmt.Errorf("count samples of the sources: %w", err)
		}

		return &sourceOutput{Body: sourceOf(source, counts[source.ID])}, nil
	})
}

// sourceOf returns source, of samples samples, as the API describes it.
func sourceOf(source store.Source, samples int64) Source {
	return Source{
		ID:            source.ID,
		DisplayName:   source.DisplayName,
		Name:          source.Name,
		DeviceModel:   source.DeviceModel,
		Kind:          string(source.Kind),
		KindSetByUser: source.KindSetByUser,
		FirstSeenAt:   source.FirstSeenAt,
		LastSeenAt:    source.LastSeenAt,
		SampleCount:   samples,
	}
}
