package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const (
	testIndex   = "<!doctype html><title>Verve</title>"
	testAppJS   = "console.log('verve')"
	testFavicon = "icon"

	cacheImmutable = "public, max-age=31536000, immutable"
	cacheNoCache   = "no-cache"
	contentHTML    = "text/html; charset=utf-8"
)

// testDist is a Vite build: index.html, a hashed asset, a non-hashed root file, a directory
// of non-hashed files and the .gitignore of internal/webui/dist.
func testDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":               {Data: []byte(testIndex)},
		"assets/index-3f9a1c2b.js": {Data: []byte(testAppJS)},
		"favicon.ico":              {Data: []byte(testFavicon)},
		"icons/icon-192.png":       {Data: []byte("png")},
		".gitignore":               {Data: []byte("*\n!.gitignore\n")},
	}
}

func newTestSPA(t *testing.T, fsys fs.FS) http.Handler {
	t.Helper()

	h, err := NewSPAHandler(fsys)
	require.NoError(t, err)

	return h
}

func TestSPAHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      string
		target      string
		status      int
		contentType string
		cache       string
		body        string
		etag        bool
		allow       string
	}{
		{
			name:   "given GET /, then index.html is revalidated",
			method: http.MethodGet, target: "/",
			status: http.StatusOK, contentType: contentHTML, cache: cacheNoCache, body: testIndex, etag: true,
		},
		{
			name:   "given HEAD /, then the headers of index.html come without the body",
			method: http.MethodHead, target: "/",
			status: http.StatusOK, contentType: contentHTML, cache: cacheNoCache, etag: true,
		},
		{
			name:   "given GET /index.html, then index.html is revalidated",
			method: http.MethodGet, target: "/index.html",
			status: http.StatusOK, contentType: contentHTML, cache: cacheNoCache, body: testIndex, etag: true,
		},
		{
			name:   "given an existing hashed asset, then it is immutable",
			method: http.MethodGet, target: "/assets/index-3f9a1c2b.js",
			status: http.StatusOK, contentType: "text/javascript; charset=utf-8", cache: cacheImmutable, body: testAppJS,
			etag: true,
		},
		{
			name:   "given a client route that matches no file, then index.html is returned",
			method: http.MethodGet, target: "/workouts/123",
			status: http.StatusOK, contentType: contentHTML, cache: cacheNoCache, body: testIndex, etag: true,
		},
		{
			name:   "given a client route with a trailing slash, then index.html is returned",
			method: http.MethodGet, target: "/workouts/",
			status: http.StatusOK, contentType: contentHTML, cache: cacheNoCache, body: testIndex, etag: true,
		},
		{
			name:   "given a directory outside the assets, then index.html is returned instead of a listing",
			method: http.MethodGet, target: "/icons/",
			status: http.StatusOK, contentType: contentHTML, cache: cacheNoCache, body: testIndex, etag: true,
		},
		{
			name:   "given a missing asset, then 404",
			method: http.MethodGet, target: "/assets/missing-abc.js",
			status: http.StatusNotFound,
		},
		{
			name:   "given the assets directory, then 404",
			method: http.MethodGet, target: "/assets/",
			status: http.StatusNotFound,
		},
		{
			name:   "given the assets directory without a trailing slash, then 404",
			method: http.MethodGet, target: "/assets",
			status: http.StatusNotFound,
		},
		{
			name:   "given an existing non-hashed root file, then it is revalidated",
			method: http.MethodGet, target: "/favicon.ico",
			status: http.StatusOK, contentType: "image/x-icon", cache: cacheNoCache, body: testFavicon, etag: true,
		},
		{
			name:   "given an existing non-hashed file in a directory, then it is revalidated",
			method: http.MethodGet, target: "/icons/icon-192.png",
			status: http.StatusOK, contentType: "image/png", cache: cacheNoCache, body: "png", etag: true,
		},
		{
			name:   "given POST on a client route, then 405",
			method: http.MethodPost, target: "/some/page",
			status: http.StatusMethodNotAllowed, allow: "GET, HEAD",
		},
		{
			name:   "given DELETE on an existing file, then 405",
			method: http.MethodDelete, target: "/favicon.ico",
			status: http.StatusMethodNotAllowed, allow: "GET, HEAD",
		},
		{
			name:   "given an existing dotfile, then 404",
			method: http.MethodGet, target: "/.gitignore",
			status: http.StatusNotFound,
		},
		{
			name:   "given a segment starting with a dot in a client route, then 404",
			method: http.MethodGet, target: "/.well-known/security.txt",
			status: http.StatusNotFound,
		},
		{
			name:   "given a dotfile under the assets, then 404",
			method: http.MethodGet, target: "/assets/.env",
			status: http.StatusNotFound,
		},
		{
			name:   "given an encoded path traversal, then 404",
			method: http.MethodGet, target: "/..%2f..%2fetc/passwd",
			status: http.StatusNotFound,
		},
		{
			name:   "given an encoded path traversal to index.html, then 404",
			method: http.MethodGet, target: "/assets/..%2findex.html",
			status: http.StatusNotFound,
		},
	}

	h := newTestSPA(t, testDist())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, h, tt.method, tt.target)

			require.Equal(t, tt.status, rec.Code)
			require.Equal(t, tt.allow, rec.Header().Get("Allow"))

			if tt.status != http.StatusOK {
				require.NotContains(t, rec.Body.String(), testIndex, "never index.html")

				return
			}

			require.Equal(t, tt.contentType, rec.Header().Get("Content-Type"))
			require.Equal(t, tt.cache, rec.Header().Get("Cache-Control"))
			require.Equal(t, tt.body, rec.Body.String())

			if tt.etag {
				require.Regexp(t, `^"[^"]+"$`, rec.Header().Get("ETag"), "a strong ETag")
			}
		})
	}
}

// Given a request with If-None-Match equal to the current ETag, then the response is 304
// without body, for index.html, for the client routes it answers and for the other files.
func TestSPAHandler_IfNoneMatchReturnsNotModified(t *testing.T) {
	t.Parallel()

	h := newTestSPA(t, testDist())

	for _, tt := range []struct{ target, file string }{
		{target: "/", file: "/"},
		{target: "/workouts/123", file: "/"},
		{target: "/assets/index-3f9a1c2b.js", file: "/assets/index-3f9a1c2b.js"},
		{target: "/favicon.ico", file: "/favicon.ico"},
	} {
		etag := serve(t, h, http.MethodGet, tt.file).Header().Get("ETag")
		require.NotEmpty(t, etag, tt.file)

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil)
		r.Header.Set("If-None-Match", etag)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		require.Equal(t, http.StatusNotModified, rec.Code, tt.target)
		require.Empty(t, rec.Body.String(), tt.target)
		require.Equal(t, etag, rec.Header().Get("ETag"), tt.target)
	}
}

// Given the same content, then the ETag is the same across handlers, so that it survives
// restarts. Given another content, then it changes.
func TestSPAHandler_ETagFollowsContent(t *testing.T) {
	t.Parallel()

	etag := func(index string) string {
		dist := testDist()
		dist["index.html"] = &fstest.MapFile{Data: []byte(index)}

		return serve(t, newTestSPA(t, dist), http.MethodGet, "/").Header().Get("ETag")
	}

	first := etag(testIndex)
	require.Equal(t, first, etag(testIndex))
	require.NotEqual(t, first, etag(testIndex+"<p>new release</p>"))
}

// Given files of each extension of the explicit table, then they are served with its content
// type, and other extensions fall back to the system table, then application/octet-stream.
func TestSPAHandler_ContentTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file string
		want string
	}{
		{file: "index.html", want: "text/html; charset=utf-8"},
		{file: "app.js", want: "text/javascript; charset=utf-8"},
		{file: "worker.mjs", want: "text/javascript; charset=utf-8"},
		{file: "style.css", want: "text/css; charset=utf-8"},
		{file: "data.json", want: "application/json"},
		{file: "logo.svg", want: "image/svg+xml"},
		{file: "logo.png", want: "image/png"},
		{file: "favicon.ico", want: "image/x-icon"},
		{file: "site.webmanifest", want: "application/manifest+json"},
		{file: "font.woff2", want: "font/woff2"},
		{file: "robots.txt", want: "text/plain; charset=utf-8"},
		{file: "module.wasm", want: "application/wasm"},
		{file: "data.verve-unknown", want: "application/octet-stream"},
	}

	dist := fstest.MapFS{}
	for _, tt := range tests {
		dist[tt.file] = &fstest.MapFile{Data: []byte("content")}
	}

	h := newTestSPA(t, dist)

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, h, http.MethodGet, "/"+tt.file)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tt.want, rec.Header().Get("Content-Type"))
		})
	}
}

// Given a build without the SPA, only the placeholder of internal/webui/dist, then GET / and
// the client routes return a minimal HTML page explaining that the web UI is not built.
func TestSPAHandler_NotBuiltReturnsPlaceholder(t *testing.T) {
	t.Parallel()

	h := newTestSPA(t, fstest.MapFS{".gitignore": {Data: []byte("*\n!.gitignore\n")}})

	for _, target := range []string{"/", "/workouts/123"} {
		rec := serve(t, h, http.MethodGet, target)

		require.Equal(t, http.StatusOK, rec.Code, target)
		require.Equal(t, contentHTML, rec.Header().Get("Content-Type"), target)
		require.Equal(t, cacheNoCache, rec.Header().Get("Cache-Control"), target)
		require.Contains(t, rec.Body.String(), "The web UI is not built", target)
		require.Contains(t, rec.Body.String(), "task build", target)
	}

	require.Equal(t, http.StatusNotFound, serve(t, h, http.MethodGet, "/.gitignore").Code)
}

// unreadableFS is fsys where the file name cannot be opened. It only implements Open, so that
// every read goes through it.
type unreadableFS struct {
	fsys fstest.MapFS
	name string
}

func (f unreadableFS) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}

	return f.fsys.Open(name)
}

// Given a file or a directory that cannot be read, then NewSPAHandler fails with its name.
func TestNewSPAHandler_UnreadableFileFails(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"favicon.ico", "assets"} {
		_, err := NewSPAHandler(unreadableFS{fsys: testDist(), name: name})
		require.ErrorIs(t, err, fs.ErrPermission, name)
		require.ErrorContains(t, err, "open "+name, name)
	}
}
