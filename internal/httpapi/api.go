package httpapi

import (
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/gauthier-se/verve/internal/version"
)

// apiVersion is the version of the API in its OpenAPI document. It is not the version of the
// binary, which GET /api/v1/meta reports: every build describes the API with the same document,
// so that the committed copy can be diffed.
const apiVersion = "1"

// APIDeps holds what the API operations need. It grows as operations are added. Its zero value
// is enough to describe the API, which is what verve openapi does.
type APIDeps struct {
	// Version is the build metadata of the running binary.
	Version version.Info
}

// useProblemErrors makes Huma build its errors as problems. huma.NewError is Huma's documented
// customization point, a package variable: setting it once keeps concurrent NewAPI calls from
// racing on it.
var useProblemErrors = sync.OnceFunc(func() { huma.NewError = newHumaError })

// NewAPI registers the API operations on mux and returns the API. The OpenAPI 3.1 spec is served
// at /api/openapi.json and /api/openapi.yaml, and its schemas under /api/schemas. The
// documentation page is not served: it would load its assets from a third-party CDN.
func NewAPI(mux *http.ServeMux, deps APIDeps) huma.API {
	useProblemErrors()

	config := huma.DefaultConfig("Verve API", apiVersion)
	config.OpenAPIPath = "/api/openapi"
	config.DocsPath = ""
	config.SchemasPath = "/api/schemas"
	// Drops the default schema link transformer, which adds a $schema field to the response
	// bodies: they keep the shape of their schema.
	config.CreateHooks = nil

	api := humago.New(mux, config)

	registerMeta(api, deps.Version)

	return api
}
