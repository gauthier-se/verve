package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "verve.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// joined returns the individual errors of an errors.Join result.
func joined(t *testing.T, err error) []error {
	t.Helper()

	var multi interface{ Unwrap() []error }
	require.ErrorAs(t, err, &multi)

	return multi.Unwrap()
}

// Given no file and no environment variable, when loading, then the defaults are used.
func TestLoad_Defaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(LoadOptions{})

	require.NoError(t, err)
	require.Equal(t, "/data", cfg.DataDir)
	require.Equal(t, ":8080", cfg.ListenAddr)
	require.Equal(t, "info", cfg.Log.Level)
	require.Equal(t, "text", cfg.Log.Format)
	require.Equal(t, ByteSize(10_000_000_000), cfg.MaxUploadSize)
	require.Equal(t, ByteSize(1_000_000_000), cfg.MaxIngestBody)
	require.True(t, cfg.PublicURL.IsZero())
	require.Empty(t, cfg.PublicURL.String())
	require.Empty(t, cfg.TrustedProxies)
	require.Empty(t, cfg.MetricsAddr)

	def := Default()
	def.Sources = cfg.Sources
	require.Equal(t, def, cfg)
}

// Given no file and no environment variable, then every setting comes from the defaults.
func TestLoad_DefaultSources(t *testing.T) {
	t.Parallel()

	cfg, err := Load(LoadOptions{})

	require.NoError(t, err)
	require.Equal(t, Sources{
		"data_dir":        SourceDefault,
		"listen_addr":     SourceDefault,
		"public_url":      SourceDefault,
		"trusted_proxies": SourceDefault,
		"log.level":       SourceDefault,
		"log.format":      SourceDefault,
		"max_upload_size": SourceDefault,
		"max_ingest_body": SourceDefault,
		"metrics_addr":    SourceDefault,
	}, cfg.Sources)
}

// Given a YAML file passed with --config containing `listen_addr: ":9090"`,
// then the listen address is `:9090`.
func TestLoad_FileSetsValue(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "listen_addr: \":9090\"\n")

	cfg, err := Load(LoadOptions{File: path})

	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.ListenAddr)
	require.Equal(t, SourceFile, cfg.Sources["listen_addr"])
	require.Equal(t, SourceDefault, cfg.Sources["data_dir"])
}

// Given a YAML file passed with VERVE_CONFIG, then it is loaded like --config.
func TestLoad_FileFromVerveConfig(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "listen_addr: \":9090\"\n")

	cfg, err := Load(LoadOptions{Env: map[string]string{"VERVE_CONFIG": path}})

	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.ListenAddr)
	require.Equal(t, SourceFile, cfg.Sources["listen_addr"])
}

// Given both --config and VERVE_CONFIG, then the explicit --config file wins.
func TestLoad_FileOptionWinsOverVerveConfig(t *testing.T) {
	t.Parallel()

	flagFile := writeFile(t, "listen_addr: \":9090\"\n")
	envFile := writeFile(t, "listen_addr: \":9191\"\n")

	cfg, err := Load(LoadOptions{File: flagFile, Env: map[string]string{"VERVE_CONFIG": envFile}})

	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.ListenAddr)
}

// Given nested keys in the YAML file, then they map to the nested settings.
func TestLoad_FileNestedKeys(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "log:\n  level: debug\n  format: json\nmax_upload_size: 512MiB\n")

	cfg, err := Load(LoadOptions{File: path})

	require.NoError(t, err)
	require.Equal(t, "debug", cfg.Log.Level)
	require.Equal(t, "json", cfg.Log.Format)
	require.Equal(t, ByteSize(536_870_912), cfg.MaxUploadSize)
	require.Equal(t, SourceFile, cfg.Sources["log.level"])
	require.Equal(t, SourceFile, cfg.Sources["log.format"])
	require.Equal(t, SourceFile, cfg.Sources["max_upload_size"])
}

// Given the same file and `VERVE_LISTEN_ADDR=:7070`, then the listen address is `:7070`.
func TestLoad_EnvironmentWinsOverFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "listen_addr: \":9090\"\n")

	cfg, err := Load(LoadOptions{File: path, Env: map[string]string{"VERVE_LISTEN_ADDR": ":7070"}})

	require.NoError(t, err)
	require.Equal(t, ":7070", cfg.ListenAddr)
	require.Equal(t, SourceEnv, cfg.Sources["listen_addr"])
}

// Given an environment variable set to an empty string, then it is ignored.
func TestLoad_EmptyEnvironmentVariableIsIgnored(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "listen_addr: \":9090\"\n")

	cfg, err := Load(LoadOptions{File: path, Env: map[string]string{"VERVE_LISTEN_ADDR": ""}})

	require.NoError(t, err)
	require.Equal(t, ":9090", cfg.ListenAddr)
	require.Equal(t, SourceFile, cfg.Sources["listen_addr"])
}

// Given nested settings in the environment, then the prefixed variables set them.
func TestLoad_EnvironmentNestedAndTyped(t *testing.T) {
	t.Parallel()

	cfg, err := Load(LoadOptions{Env: map[string]string{
		"VERVE_DATA_DIR":        "/srv/verve",
		"VERVE_LOG_LEVEL":       "warn",
		"VERVE_LOG_FORMAT":      "json",
		"VERVE_MAX_UPLOAD_SIZE": "512MiB",
		"VERVE_MAX_INGEST_BODY": "1048576",
		"VERVE_METRICS_ADDR":    "127.0.0.1:9100",
		"UNRELATED":             "ignored",
	}})

	require.NoError(t, err)
	require.Equal(t, "/srv/verve", cfg.DataDir)
	require.Equal(t, "warn", cfg.Log.Level)
	require.Equal(t, "json", cfg.Log.Format)
	require.Equal(t, ByteSize(536_870_912), cfg.MaxUploadSize)
	require.Equal(t, ByteSize(1_048_576), cfg.MaxIngestBody)
	require.Equal(t, "127.0.0.1:9100", cfg.MetricsAddr)
	require.Equal(t, SourceEnv, cfg.Sources["log.level"])
	require.Equal(t, SourceEnv, cfg.Sources["max_ingest_body"])
}

// Given a YAML file with an unknown key `listen_adress`, then loading fails
// with an error naming the key and the file.
func TestLoad_UnknownKeyInFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "listen_adress: \":9090\"\n")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Contains(t, err.Error(), "listen_adress")
	require.Contains(t, err.Error(), path)
}

// Given an unknown nested key, then the error names its full path.
func TestLoad_UnknownNestedKeyInFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "log:\n  levle: debug\n")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Contains(t, err.Error(), "log.levle")
	require.Contains(t, err.Error(), path)
}

// Given a --config file that does not exist, then loading fails naming the file.
func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Contains(t, err.Error(), path)
}

// Given a file that is not valid YAML, then loading fails naming the file.
func TestLoad_MalformedFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "listen_addr: [\n")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Contains(t, err.Error(), path)
}

// Given an empty file, then the defaults are used.
func TestLoad_EmptyFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "")

	cfg, err := Load(LoadOptions{File: path})

	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.ListenAddr)
}

// Given several invalid values at once, then loading returns all of them,
// each naming the setting (YAML key and environment variable) and the invalid value.
func TestLoad_AggregatesAllErrors(t *testing.T) {
	t.Parallel()

	_, err := Load(LoadOptions{Env: map[string]string{
		"VERVE_TRUSTED_PROXIES": "10.0.0.0/33",
		"VERVE_LOG_LEVEL":       "verbose",
		"VERVE_PUBLIC_URL":      "verve.local",
	}})

	require.Error(t, err)

	errs := joined(t, err)
	require.Len(t, errs, 3)

	msg := err.Error()
	require.Contains(t, msg, `trusted_proxies (VERVE_TRUSTED_PROXIES): invalid value "10.0.0.0/33"`)
	require.Contains(t, msg, `log.level (VERVE_LOG_LEVEL): invalid value "verbose": must be one of debug, info, warn, error`)
	require.Contains(t, msg, `public_url (VERVE_PUBLIC_URL): invalid value "verve.local"`)
}

// Given invalid values in the file, then the errors name the setting, the value and the file,
// and are reported together with the validation errors.
func TestLoad_FileValueErrors(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "max_upload_size: 10XB\nlog:\n  format: xml\n")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Len(t, joined(t, err), 2)

	msg := err.Error()
	require.Contains(t, msg, path)
	require.Contains(t, msg, `max_upload_size (VERVE_MAX_UPLOAD_SIZE): invalid value "10XB"`)
	require.Contains(t, msg, `log.format (VERVE_LOG_FORMAT): invalid value "xml": must be one of text, json`)
}

// Given a size of zero, then loading fails: uploads and ingestion need a positive limit.
func TestLoad_ZeroSizeIsInvalid(t *testing.T) {
	t.Parallel()

	_, err := Load(LoadOptions{Env: map[string]string{"VERVE_MAX_INGEST_BODY": "0"}})

	require.Error(t, err)
	require.Contains(t, err.Error(), `max_ingest_body (VERVE_MAX_INGEST_BODY): invalid value "0": must be greater than zero`)
}

// Given a size `10XB` or `-1` in the environment, then loading fails.
func TestLoad_InvalidSizesFromEnvironment(t *testing.T) {
	t.Parallel()

	_, err := Load(LoadOptions{Env: map[string]string{
		"VERVE_MAX_UPLOAD_SIZE": "10XB",
		"VERVE_MAX_INGEST_BODY": "-1",
	}})

	require.Error(t, err)
	require.Contains(t, err.Error(), `max_upload_size (VERVE_MAX_UPLOAD_SIZE): invalid value "10XB"`)
	require.Contains(t, err.Error(), `max_ingest_body (VERVE_MAX_INGEST_BODY): invalid value "-1"`)
}

// Given VERVE_PUBLIC_URL, then only absolute http(s) URLs without a sub-path are accepted,
// and a trailing `/` is normalized away.
func TestLoad_PublicURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "https", in: "https://verve.example.com", want: "https://verve.example.com"},
		{name: "trailing slash", in: "https://verve.example.com/", want: "https://verve.example.com"},
		{name: "http with port", in: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "sub-path", in: "https://verve.example.com/sub", wantErr: "sub-path"},
		{name: "sub-path with slash", in: "https://verve.example.com/sub/", wantErr: "sub-path"},
		{name: "no scheme", in: "verve.local", wantErr: "http or https"},
		{name: "other scheme", in: "ftp://verve.example.com", wantErr: "http or https"},
		{name: "no host", in: "https://", wantErr: "host"},
		{name: "query", in: "https://verve.example.com?x=1", wantErr: "query"},
		{name: "fragment", in: "https://verve.example.com#x", wantErr: "fragment"},
		{name: "user info", in: "https://user:pass@verve.example.com", wantErr: "user info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(LoadOptions{Env: map[string]string{"VERVE_PUBLIC_URL": tt.in}})

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), `public_url (VERVE_PUBLIC_URL): invalid value "`+tt.in+`"`)
				require.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.False(t, cfg.PublicURL.IsZero())
			require.Equal(t, tt.want, cfg.PublicURL.String())
			require.Equal(t, tt.want, cfg.PublicURL.URL().String())
		})
	}
}

// Given `VERVE_TRUSTED_PROXIES="10.0.0.0/8, 172.16.0.0/12,fd00::/8"`, then three prefixes
// are parsed; a single address is accepted as a host prefix.
func TestLoad_TrustedProxiesFromEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Prefixes
	}{
		{
			in: "10.0.0.0/8, 172.16.0.0/12,fd00::/8",
			want: Prefixes{
				netip.MustParsePrefix("10.0.0.0/8"),
				netip.MustParsePrefix("172.16.0.0/12"),
				netip.MustParsePrefix("fd00::/8"),
			},
		},
		{in: "192.168.1.10", want: Prefixes{netip.MustParsePrefix("192.168.1.10/32")}},
		{in: "::1", want: Prefixes{netip.MustParsePrefix("::1/128")}},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(LoadOptions{Env: map[string]string{"VERVE_TRUSTED_PROXIES": tt.in}})

			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.TrustedProxies)
			require.Equal(t, SourceEnv, cfg.Sources["trusted_proxies"])
		})
	}
}

// Given trusted proxies in YAML, then the setting is a list.
func TestLoad_TrustedProxiesFromFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "trusted_proxies:\n  - 10.0.0.0/8\n  - 192.168.1.10\n")

	cfg, err := Load(LoadOptions{File: path})

	require.NoError(t, err)
	require.Equal(t, Prefixes{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.10/32"),
	}, cfg.TrustedProxies)
	require.Equal(t, SourceFile, cfg.Sources["trusted_proxies"])
}

// Given trusted proxies as a YAML string instead of a list, then loading fails.
func TestLoad_TrustedProxiesFromFileMustBeList(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "trusted_proxies: 10.0.0.0/8\n")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Contains(t, err.Error(), "trusted_proxies (VERVE_TRUSTED_PROXIES)")
	require.Contains(t, err.Error(), "must be a list")
}

// Given an invalid trusted proxy in a YAML list, then the error names the entry.
func TestLoad_TrustedProxiesFromFileInvalidEntry(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "trusted_proxies:\n  - 10.0.0.0/8\n  - 10.0.0.0/33\n")

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	require.Contains(t, err.Error(), `trusted_proxies (VERVE_TRUSTED_PROXIES): invalid value "10.0.0.0/33"`)
}

// Given an empty entry in VERVE_TRUSTED_PROXIES, then loading fails.
func TestLoad_TrustedProxiesEmptyEntry(t *testing.T) {
	t.Parallel()

	_, err := Load(LoadOptions{Env: map[string]string{"VERVE_TRUSTED_PROXIES": "10.0.0.0/8,,fd00::/8"}})

	require.Error(t, err)
	require.Contains(t, err.Error(), `trusted_proxies (VERVE_TRUSTED_PROXIES): invalid value ""`)
}

// Given VERVE_PUBLIC_URL is not set, then Warnings contains a warning explaining that
// secure cookies, OIDC and absolute links depend on it.
func TestConfig_WarningsWithoutPublicURL(t *testing.T) {
	t.Parallel()

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	warnings := cfg.Warnings()

	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "VERVE_PUBLIC_URL")
	require.Contains(t, warnings[0], "secure cookies")
	require.Contains(t, warnings[0], "OIDC")
	require.Contains(t, warnings[0], "absolute links")
}

// Given VERVE_PUBLIC_URL is set, then there is no warning.
func TestConfig_NoWarningsWithPublicURL(t *testing.T) {
	t.Parallel()

	cfg, err := Load(LoadOptions{Env: map[string]string{"VERVE_PUBLIC_URL": "https://verve.example.com"}})
	require.NoError(t, err)

	require.Empty(t, cfg.Warnings())
}
