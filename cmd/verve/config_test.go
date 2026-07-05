package main

import (
	"path/filepath"
	"testing"
)

func TestParseConfigDataDir(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		// Ensure the env var does not leak in from the runner.
		t.Setenv("VERVE_DATA_DIR", "")
		cfg, _, err := parseConfig(nil)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.dataDir != defaultDataDir {
			t.Errorf("dataDir = %q, want %q", cfg.dataDir, defaultDataDir)
		}
	})

	t.Run("env overrides default", func(t *testing.T) {
		t.Setenv("VERVE_DATA_DIR", "/srv/verve")
		cfg, _, err := parseConfig(nil)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.dataDir != "/srv/verve" {
			t.Errorf("dataDir = %q, want /srv/verve", cfg.dataDir)
		}
	})

	t.Run("flag overrides env", func(t *testing.T) {
		t.Setenv("VERVE_DATA_DIR", "/srv/verve")
		cfg, rest, err := parseConfig([]string{"-data-dir=/tmp/v", "account", "create"})
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.dataDir != "/tmp/v" {
			t.Errorf("dataDir = %q, want /tmp/v", cfg.dataDir)
		}
		if len(rest) != 2 || rest[0] != "account" || rest[1] != "create" {
			t.Errorf("rest = %v, want [account create]", rest)
		}
	})
}

func TestConfigPaths(t *testing.T) {
	cfg := config{dataDir: "/data"}
	if got, want := cfg.dbPath(), filepath.Join("/data", "verve.db"); got != want {
		t.Errorf("dbPath = %q, want %q", got, want)
	}
	if got, want := cfg.artifactsDir(), filepath.Join("/data", "artifacts"); got != want {
		t.Errorf("artifactsDir = %q, want %q", got, want)
	}
}
