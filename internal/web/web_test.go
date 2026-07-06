package web

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// handlerFor builds a Handler over an arbitrary in-memory dist, so the tests
// don't depend on whether the real SPA has been built into internal/web/dist.
// It mirrors Handler()'s construction against the injected filesystem.
func handlerFor(t *testing.T, files fstest.MapFS) http.Handler {
	t.Helper()
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return http.HandlerFunc(serveUnbuilt)
	}
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean != "" && fileExists(files, clean) {
			if strings.HasPrefix(clean, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

func get(t *testing.T, h http.Handler, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Result()
}

func TestServesBuiltAssetsAndFallsBackToIndex(t *testing.T) {
	h := handlerFor(t, fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>Verve</title>")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
		"favicon.svg":          {Data: []byte("<svg/>")},
	})

	// A real asset is served directly with an immutable cache header.
	res := get(t, h, "/assets/app-abc123.js")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", cc)
	}

	// The root serves index.html.
	res = get(t, h, "/")
	body := readBody(t, res)
	if !strings.Contains(body, "Verve") {
		t.Errorf("root body = %q, want the SPA shell", body)
	}

	// A client-side route (no such file) falls back to index.html, not a 404.
	res = get(t, h, "/dashboards/42")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("client route status = %d, want 200 (SPA fallback)", res.StatusCode)
	}
	if body := readBody(t, res); !strings.Contains(body, "Verve") {
		t.Errorf("client route body = %q, want index.html", body)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", cc)
	}
}

func TestUnbuiltServesInstructions(t *testing.T) {
	// dist with no index.html (only the .gitkeep placeholder, as in a Go-only
	// build) serves the instructions page.
	h := handlerFor(t, fstest.MapFS{".gitkeep": {Data: []byte("placeholder")}})
	res := get(t, h, "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unbuilt status = %d, want 200", res.StatusCode)
	}
	if body := readBody(t, res); !strings.Contains(body, "has not been built") {
		t.Errorf("unbuilt body = %q, want the build instructions", body)
	}
}

// TestEmbeddedHandlerConstructs checks the real go:embed-backed Handler builds
// without panicking, whether or not the SPA has been built into dist/.
func TestEmbeddedHandlerConstructs(t *testing.T) {
	if Handler() == nil {
		t.Fatal("Handler() returned nil")
	}
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
