package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/version"
)

// Meta describes the running server.
type Meta struct {
	Name      string `json:"name" example:"Verve" doc:"The name of the application."`
	Version   string `json:"version" example:"1.4.0" doc:"The version of the server, dev for a development build."`
	Commit    string `json:"commit" example:"3f9a1c2" doc:"The commit the server was built from, unknown for a development build."`
	BuildDate string `json:"buildDate" example:"2026-10-28T10:00:00Z" doc:"When the server was built in RFC 3339 format, unknown for a development build."`
}

type metaOutput struct {
	Body Meta
}

// registerMeta registers GET /api/v1/meta, which reports build.
func registerMeta(api huma.API, build version.Info) {
	huma.Register(api, huma.Operation{
		OperationID: "get-meta",
		Method:      http.MethodGet,
		Path:        "/api/v1/meta",
		Summary:     "Get the server metadata",
		Description: "Returns the name of the application and the build metadata of the server, without " +
			"authentication. The web app shows the version and reloads itself when it changes.",
		Tags: []string{"Meta"},
	}, func(context.Context, *struct{}) (*metaOutput, error) {
		return &metaOutput{Body: Meta{
			Name:      "Verve",
			Version:   build.Version,
			Commit:    build.Commit,
			BuildDate: build.BuildDate,
		}}, nil
	})
}
