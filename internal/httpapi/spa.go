package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	// assetsDir is where Vite writes the hashed assets, whose name changes with their content.
	assetsDir = "assets"
	// indexFile is the entry point of the web app, answered for the client routes.
	indexFile = "index.html"

	// cacheControlImmutable caches the hashed assets for a year without revalidation.
	cacheControlImmutable = "public, max-age=31536000, immutable"
	// cacheControlRevalidate makes browsers revalidate the files that keep their name across builds.
	cacheControlRevalidate = "no-cache"
)

// notBuiltPage is answered in place of index.html by a binary built without the web app.
const notBuiltPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Verve</title>
</head>
<body>
<h1>The web UI is not built</h1>
<p>This Verve binary was built without the web app. Run <code>task build</code> to build a binary that embeds it.</p>
</body>
</html>
`

// contentTypes maps the extensions of the web app files to their content type, so that it does
// not depend on the system tables read by mime.TypeByExtension. Lookup table, never modified.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".ico":         "image/x-icon",
	".webmanifest": "application/manifest+json",
	".woff2":       "font/woff2",
	".txt":         "text/plain; charset=utf-8",
}

// spaFile is a file of the web app with its response headers.
type spaFile struct {
	content      []byte
	contentType  string
	cacheControl string
	etag         string
}

// spaHandler serves the web app, see NewSPAHandler.
type spaHandler struct {
	// files are the files of the web app by path. resolve never looks up a dotfile.
	files map[string]spaFile
	// index is index.html, or the not built page when the web app has none.
	index spaFile
}

// NewSPAHandler returns the handler of the web app built in fsys. It reads the files of fsys once,
// and serves them to GET and HEAD with an ETag:
//   - the hashed assets, under assets/, are cached as immutable,
//   - the other files are revalidated,
//   - any other path is a client route, answered with index.html, except under assets/ (404),
//   - paths with a segment starting with a dot, such as dotfiles, are 404.
//
// When fsys has no index.html, a page explaining that the web UI is not built replaces it.
func NewSPAHandler(fsys fs.FS) (http.Handler, error) {
	h := &spaHandler{files: map[string]spaFile{}}

	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		h.files[name] = newSPAFile(name, content)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read web app: %w", err)
	}

	index, ok := h.files[indexFile]
	if !ok {
		index = newSPAFile(indexFile, []byte(notBuiltPage))
	}

	h.index = index

	return h, nil
}

// newSPAFile returns the file name of the web app with its headers. Its ETag is the start of
// the SHA-256 of its content, so that it only changes with the content.
func newSPAFile(name string, content []byte) spaFile {
	sum := sha256.Sum256(content)

	cacheControl := cacheControlRevalidate
	if isAsset(name) {
		cacheControl = cacheControlImmutable
	}

	return spaFile{
		content:      content,
		contentType:  contentType(name),
		cacheControl: cacheControl,
		etag:         `"` + hex.EncodeToString(sum[:16]) + `"`,
	}
}

// contentType returns the content type of the file name from its extension.
func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))

	if t, ok := contentTypes[ext]; ok {
		return t
	}

	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}

	return "application/octet-stream"
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	f, ok := h.resolve(r.URL.Path)
	if !ok {
		http.NotFound(w, r)

		return
	}

	header := w.Header()
	header.Set("Content-Type", f.contentType)
	header.Set("Cache-Control", f.cacheControl)
	header.Set("ETag", f.etag)

	// ServeContent answers the conditional, range and HEAD requests.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.content))
}

// resolve returns the file that answers the URL path p, false when the answer is a 404.
func (h *spaHandler) resolve(p string) (spaFile, bool) {
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		return h.index, true
	}

	if !servableName(strings.TrimSuffix(name, "/")) {
		return spaFile{}, false
	}

	if f, ok := h.files[name]; ok {
		return f, true
	}

	if isAsset(name) {
		return spaFile{}, false
	}

	return h.index, true
}

// isAsset reports whether name is the assets directory or a path under it.
func isAsset(name string) bool {
	return name == assetsDir || strings.HasPrefix(name, assetsDir+"/")
}

// servableName reports whether name is a valid path of an fs.FS without any segment starting with
// a dot, which rules out dotfiles, "." and "..".
func servableName(name string) bool {
	if !fs.ValidPath(name) {
		return false
	}

	for segment := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}

	return true
}
