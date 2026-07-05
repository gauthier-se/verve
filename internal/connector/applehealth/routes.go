package applehealth

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// routeOpener resolves a Workout route's FileReference path — e.g.
// "/workout-routes/route_2025-07-24_11.54am.gpx" — to the bytes of that GPX,
// wherever the export keeps them. The two implementations cover the two ways an
// export is supplied: a directory (export.xml with sibling folders) or a zip.
type routeOpener interface {
	open(refPath string) (io.ReadCloser, error)
}

// dirRouteOpener resolves FileReference paths relative to the export's directory
// (Apple stores routes in a workout-routes/ folder beside export.xml).
type dirRouteOpener struct{ baseDir string }

func (d dirRouteOpener) open(refPath string) (io.ReadCloser, error) {
	rel := filepath.FromSlash(strings.TrimPrefix(refPath, "/"))
	f, err := os.Open(filepath.Join(d.baseDir, rel))
	if err != nil {
		return nil, fmt.Errorf("applehealth: open route %s: %w", refPath, err)
	}
	return f, nil
}

// zipRouteOpener resolves FileReference paths to entries in the export archive,
// matched by suffix because Apple nests everything under apple_health_export/.
type zipRouteOpener struct{ zr *zip.Reader }

func (z zipRouteOpener) open(refPath string) (io.ReadCloser, error) {
	want := strings.TrimPrefix(path.Clean(refPath), "/")
	for _, f := range z.zr.File {
		if strings.HasSuffix(f.Name, want) {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("applehealth: open route %s in zip: %w", f.Name, err)
			}
			return rc, nil
		}
	}
	return nil, fmt.Errorf("applehealth: route %s not found in zip", refPath)
}

// copyRouteArtifact copies one route's GPX into artifactsDir under a
// content-addressed name (<sha256>.gpx) and returns that content key and
// filename. The file is streamed through the hash into a temp file, then renamed
// into place, so it never loads whole into memory and a crash mid-copy leaves no
// partial artifact. When the content-addressed name already exists (a re-import
// of identical bytes), the existing copy is kept and the temp discarded (ADR
// 0004 artifacts-as-files, ADR 0006 idempotency).
func copyRouteArtifact(opener routeOpener, refPath, artifactsDir string) (key, artifact string, err error) {
	if opener == nil {
		return "", "", fmt.Errorf("applehealth: route %s referenced but no opener", refPath)
	}
	rc, err := opener.open(refPath)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()

	tmp, err := os.CreateTemp(artifactsDir, ".route-*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("applehealth: temp route artifact: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), rc); err != nil {
		tmp.Close()
		return "", "", fmt.Errorf("applehealth: copy route %s: %w", refPath, err)
	}
	if err := tmp.Close(); err != nil {
		return "", "", fmt.Errorf("applehealth: close route artifact: %w", err)
	}

	key = hex.EncodeToString(h.Sum(nil))
	artifact = key + ".gpx"
	final := filepath.Join(artifactsDir, artifact)
	if _, statErr := os.Stat(final); statErr == nil {
		return key, artifact, nil // already present; temp removed by defer
	}
	if err := os.Rename(tmpName, final); err != nil {
		return "", "", fmt.Errorf("applehealth: place route artifact: %w", err)
	}
	tmpName = "" // renamed into place; keep it
	return key, artifact, nil
}
