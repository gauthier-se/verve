package config

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// printable is a test-only configuration with a secret, since Config has none yet.
type printable struct {
	Name    string   `yaml:"name" env:"NAME"`
	Token   string   `yaml:"token" env:"TOKEN" secret:"true"`
	Unset   string   `yaml:"unset_secret" env:"UNSET_SECRET" secret:"true"`
	Size    ByteSize `yaml:"size" env:"SIZE"`
	Proxies Prefixes `yaml:"proxies" env:"PROXIES"`
	None    Prefixes `yaml:"no_proxies" env:"NO_PROXIES"`
	Nested  struct {
		Level string `yaml:"level" env:"LEVEL"`
	} `yaml:"nested" envPrefix:"NESTED_"`
	Ignored string `yaml:"-"`
}

// Given a configuration and its sources, then writeAnnotatedYAML prints every value annotated with
// its source, and a secret with a value as `********`.
func TestWriteAnnotatedYAML_SourcesAndSecrets(t *testing.T) {
	t.Parallel()

	v := printable{
		Name:    "verve",
		Token:   "s3cr3t",
		Size:    10_000_000_000,
		Proxies: Prefixes{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")},
		Ignored: "hidden",
	}
	v.Nested.Level = "debug"

	sources := Sources{
		"name":         SourceDefault,
		"token":        SourceEnv,
		"unset_secret": SourceDefault,
		"size":         SourceFile,
		"proxies":      SourceFile,
		"no_proxies":   SourceDefault,
		"nested.level": SourceEnv,
	}

	var buf bytes.Buffer
	require.NoError(t, writeAnnotatedYAML(&buf, v, sources))

	require.Equal(t, strings.Join([]string{
		`name: verve # default`,
		`token: "********" # env`,
		`unset_secret: "" # default`,
		`size: 10GB # file`,
		`proxies: # file`,
		`  - 10.0.0.0/8`,
		`  - fd00::/8`,
		`no_proxies: [] # default`,
		`nested:`,
		`  level: debug # env`,
		``,
	}, "\n"), buf.String())
	require.NotContains(t, buf.String(), "s3cr3t")
	require.NotContains(t, buf.String(), "hidden")
}

// Given the loaded configuration, then Config.WriteYAML prints the effective values
// with their sources.
func TestConfig_WriteYAML(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "log:\n  level: debug\n")

	cfg, err := Load(LoadOptions{File: path, Env: map[string]string{
		"VERVE_PUBLIC_URL":      "https://verve.example.com/",
		"VERVE_TRUSTED_PROXIES": "10.0.0.0/8",
	}})
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, cfg.WriteYAML(&buf))

	out := buf.String()
	require.Contains(t, out, "data_dir: /data # default\n")
	require.Contains(t, out, "public_url: https://verve.example.com # env\n")
	require.Contains(t, out, "trusted_proxies: # env\n  - 10.0.0.0/8\n")
	require.Contains(t, out, "log:\n  level: debug # file\n  format: text # default\n")
	require.Contains(t, out, "max_upload_size: 10GB # default\n")
	require.Contains(t, out, "max_ingest_body: 1GB # default\n")
	require.Contains(t, out, `metrics_addr: "" # default`+"\n")
}
