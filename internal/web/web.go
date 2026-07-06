// Package web serves Verve's React SPA, embedded into the binary via go:embed
// so the whole app ships as a single executable (ADR 0005). The Vite build
// writes its static assets into dist/; this package embeds that directory and
// serves it with an index.html fallback on every non-asset path, so the SPA's
// client-side routing (ADR 0013) works on a hard refresh or deep link.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// distFS holds the built SPA. The `all:` prefix embeds dotfiles too, so the
// committed dist/.gitkeep placeholder makes this compile even before the
// front-end is built — CI's Go job needs no Node. A real `make ui` build
// overwrites dist/ with index.html + assets/.
//
//go:embed all:dist
var distFS embed.FS

// unbuiltPage is served when dist/ holds no index.html — i.e. the binary was
// built without first building the SPA. It tells the operator how to fix it
// rather than returning a bare 404.
const unbuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Verve</title></head>
<body style="font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem">
<h1>Verve</h1>
<p>The API is running, but the web UI has not been built into this binary.</p>
<p>Build it with <code>make ui</code> (or <code>npm --prefix web ci &amp;&amp; npm --prefix web run build</code>),
then rebuild the binary with <code>make build</code>.</p>
</body></html>`

// Handler returns an http.Handler that serves the embedded SPA: a real file in
// dist/ is served directly (hashed assets get a long-lived cache header), and
// any other path falls back to index.html so client-side routes resolve. When
// the SPA is unbuilt it serves a short instructions page instead.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist is always embedded (the .gitkeep placeholder guarantees it), so
		// this is unreachable; fall back to the unbuilt page rather than panic.
		return http.HandlerFunc(serveUnbuilt)
	}

	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return http.HandlerFunc(serveUnbuilt)
	}

	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean != "" && fileExists(sub, clean) {
			// Vite fingerprints asset filenames, so an /assets/* file is
			// immutable and safe to cache aggressively.
			if strings.HasPrefix(clean, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// Deep link or client-side route: hand back the SPA shell. index.html
		// itself must never be cached, or a deploy won't reach existing tabs.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

// fileExists reports whether name is a regular file in the SPA filesystem. A
// directory is treated as absent so it falls through to the index fallback.
func fileExists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

func serveUnbuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(unbuiltPage))
}
