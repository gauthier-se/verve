package main

import (
	"flag"
	"os"
	"path/filepath"
)

// defaultDataDir is used when neither -data-dir nor VERVE_DATA_DIR is set.
// It matches the /data/ entry ignored in .gitignore.
const defaultDataDir = "./data"

// config holds the runtime configuration, injected into the application.
type config struct {
	// dataDir is the single directory holding verve.db, artifacts/ and imports.
	dataDir string
}

func (c config) dbPath() string       { return filepath.Join(c.dataDir, "verve.db") }
func (c config) artifactsDir() string { return filepath.Join(c.dataDir, "artifacts") }

// parseConfig parses the global flags, falling back to the VERVE_DATA_DIR
// environment variable and then a sensible default. It returns the config and
// the remaining args (the subcommand and its own arguments). The flag package
// stops at the first non-flag argument, so global flags must precede the
// subcommand (e.g. `verve -data-dir=/srv/verve account create ...`).
func parseConfig(args []string) (config, []string, error) {
	fs := flag.NewFlagSet("verve", flag.ContinueOnError)
	dataDir := fs.String("data-dir", envOr("VERVE_DATA_DIR", defaultDataDir),
		"directory holding verve.db, artifacts/ and imports")
	if err := fs.Parse(args); err != nil {
		return config{}, nil, err
	}
	return config{dataDir: *dataDir}, fs.Args(), nil
}

// envOr returns the value of the environment variable key, or fallback if it is
// unset or empty.
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
