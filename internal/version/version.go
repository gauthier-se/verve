// Package version exposes the build metadata injected at link time.
package version

import "fmt"

// Set at build time with -ldflags "-X github.com/gauthier-se/verve/internal/version.version=...".
// These build-time values are the only package-level variables allowed in the codebase.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// Info describes the running build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// Get returns the build metadata of the running binary.
func Get() Info {
	return Info{Version: version, Commit: commit, BuildDate: date}
}

// String formats the build metadata on one line.
func (i Info) String() string {
	return fmt.Sprintf("verve %s (commit %s, built %s)", i.Version, i.Commit, i.BuildDate)
}
