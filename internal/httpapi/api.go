package httpapi

import (
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
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
	// Store holds the data of the operations.
	Store store.Store
	// Logins logs users in, and Sessions lists the sessions and deletes them, such as at logout.
	Logins   *auth.Logins
	Sessions auth.Sessions
	// Passwords changes the password of the user of a session.
	Passwords auth.Passwords
	// Invitations invites users and creates their account.
	Invitations auth.Invitations
	// PasswordResets replaces passwords through reset links.
	PasswordResets auth.PasswordResets
	// Admin holds the rules of the administration of accounts: status and role changes here.
	Admin auth.Admin
	// Deletions requests the deletion of accounts, by their user or by an admin.
	Deletions auth.Deletions
	// Setup creates the first admin.
	Setup *auth.Setup
	// PublicURL names the session cookie that login sets and logout clears.
	PublicURL config.PublicURL
	// Backups lists, opens and creates the backups of the instance.
	Backups *backup.Backups
	// Jobs queues the jobs of the API, such as manual backups.
	Jobs auth.JobQueue
	// Tokens creates and revokes the API tokens of the users.
	Tokens auth.Tokens
	// Catalog is the metric type catalog shipped with the binary, which gives the category values
	// of the metric types.
	Catalog *catalog.Catalog
	// Inbox stores the bodies of the pushes, of at most MaxIngestBody bytes.
	Inbox         Inbox
	MaxIngestBody int64
}

// useProblemErrors makes Huma build its errors as problems. huma.NewError is Huma's documented
// customization point, a package variable: setting it once keeps concurrent NewAPI calls from
// racing on it.
var useProblemErrors = sync.OnceFunc(func() { huma.NewError = newHumaError })

// NewAPI registers the API operations on mux and returns the API. The OpenAPI 3.1 spec is served
// at /api/openapi.json and /api/openapi.yaml. The documentation page is not served: it would load
// its assets from a third-party CDN.
func NewAPI(mux *http.ServeMux, deps APIDeps) huma.API {
	useProblemErrors()

	config := huma.DefaultConfig("Verve API", apiVersion)
	config.OpenAPIPath = "/api/openapi"
	config.DocsPath = ""
	// Drops the default schema link transformer, which adds a $schema field to the response
	// bodies: they keep the shape of their schema.
	config.CreateHooks = nil
	// Nothing links to the schemas once the transformer is gone, and Huma would answer an unknown
	// schema with 200 null instead of the 404 problem: they are only served in the spec.
	config.SchemasPath = ""
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		sessionCookieSchemeName: sessionCookieScheme(),
		bearerTokenSchemeName:   bearerTokenScheme(),
	}

	api := humago.New(mux, config)

	registerMeta(api, deps.Version)
	registerLoginAndLogout(api, deps)
	registerCurrentUser(api, deps.Store)
	registerPreferences(api, deps.Store)
	registerAccount(api, deps)
	registerSetup(api, deps)
	registerInvitations(api, deps)
	registerPasswordResets(api, deps)
	registerAudit(api, deps.Store)
	registerAdminJobs(api, deps.Store)
	registerAdminUsers(api, deps)
	registerAccountDeletion(api, deps)
	registerAdminBackups(api, deps)
	registerTokens(api, deps)
	registerMetrics(api, deps)
	registerIngest(api, deps)

	return api
}
