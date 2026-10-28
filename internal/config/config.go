// Package config loads and validates the Verve configuration from built-in defaults,
// an optional YAML file and VERVE_* environment variables, in increasing precedence.
package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// envPrefix is the prefix of every environment variable.
const envPrefix = "VERVE_"

// Config is the effective configuration. The yaml tags give the YAML keys, the env tags
// the environment variables without envPrefix.
type Config struct {
	DataDir        string    `yaml:"data_dir"        env:"DATA_DIR"`
	ListenAddr     string    `yaml:"listen_addr"     env:"LISTEN_ADDR"`
	PublicURL      PublicURL `yaml:"public_url"      env:"PUBLIC_URL"`
	TrustedProxies Prefixes  `yaml:"trusted_proxies" env:"TRUSTED_PROXIES"`
	Log            LogConfig `yaml:"log"             envPrefix:"LOG_"`
	MaxUploadSize  ByteSize  `yaml:"max_upload_size" env:"MAX_UPLOAD_SIZE"`
	MaxIngestBody  ByteSize  `yaml:"max_ingest_body" env:"MAX_INGEST_BODY"`
	MetricsAddr    string    `yaml:"metrics_addr"    env:"METRICS_ADDR"`

	// Sources tells where each setting comes from, keyed by YAML path. Set by Load.
	Sources Sources `yaml:"-" env:"-"`
}

// LogConfig configures logging.
type LogConfig struct {
	Level  string `yaml:"level"  env:"LEVEL"`
	Format string `yaml:"format" env:"FORMAT"`
}

// Log formats.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Lookup tables, never modified.
var (
	logLevels  = []string{"debug", "info", "warn", "error"}
	logFormats = []string{LogFormatText, LogFormatJSON}
)

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		DataDir:       "/data",
		ListenAddr:    ":8080",
		Log:           LogConfig{Level: "info", Format: LogFormatText},
		MaxUploadSize: 10_000_000_000,
		MaxIngestBody: 1_000_000_000,
	}
}

// Validate checks the values that parsing alone cannot reject and returns every problem at once.
func (c Config) Validate() error {
	return errors.Join(c.validate(c.settings())...)
}

func (c Config) validate(settings map[string]setting) []error {
	var errs []error

	invalid := func(path, value, reason string) {
		errs = append(errs, fmt.Errorf("%s: %w", settings[path].name(), invalidValue(value, reason)))
	}

	oneOf := func(path, value string, allowed []string) {
		if !slices.Contains(allowed, value) {
			invalid(path, value, "must be one of "+strings.Join(allowed, ", "))
		}
	}

	positive := func(path string, size ByteSize) {
		if size <= 0 {
			invalid(path, size.String(), "must be greater than zero")
		}
	}

	oneOf("log.level", c.Log.Level, logLevels)
	oneOf("log.format", c.Log.Format, logFormats)
	positive("max_upload_size", c.MaxUploadSize)
	positive("max_ingest_body", c.MaxIngestBody)

	return errs
}

// Warnings returns problems that do not prevent Verve from starting but deserve the admin's attention.
func (c Config) Warnings() []string {
	var warnings []string

	if c.PublicURL.IsZero() {
		warnings = append(warnings, c.settings()["public_url"].env+" is not set: secure cookies, OIDC and absolute links "+
			"depend on it. Set it to the URL users reach Verve at, for example https://verve.example.com")
	}

	return warnings
}

// settings indexes the settings of the configuration by YAML path.
func (c *Config) settings() map[string]setting {
	return indexSettings(settingsOf(reflect.ValueOf(c).Elem()))
}

// invalidValue is the common form of every value error: `invalid value "x": reason`.
func invalidValue(value, reason string) error {
	return fmt.Errorf("invalid value %q: %s", value, reason)
}
