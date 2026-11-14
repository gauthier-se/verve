// Package config loads and validates the Verve configuration from built-in defaults,
// an optional YAML file and VERVE_* environment variables, in increasing precedence.
package config

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gauthier-se/verve/internal/jobs"
)

// envPrefix is the prefix of every environment variable.
const envPrefix = "VERVE_"

// minSecretKeyLength is the shortest accepted secret key, in characters.
const minSecretKeyLength = 32

// maxAuditRetentionDays is the longest accepted audit retention, 100 years. A much longer one would
// overflow the cutoff of the purge, which would then remove every event.
const maxAuditRetentionDays = 36_500

// maxRawMaxDays is the longest accepted range of a raw samples read, a leap year.
const maxRawMaxDays = 366

// Config is the effective configuration. The yaml tags give the YAML keys, the env tags
// the environment variables without envPrefix. A setting with `yaml:"-"` and an env tag
// is set by the environment only.
type Config struct {
	DataDir        string       `yaml:"data_dir"        env:"DATA_DIR"`
	ListenAddr     string       `yaml:"listen_addr"     env:"LISTEN_ADDR"`
	PublicURL      PublicURL    `yaml:"public_url"      env:"PUBLIC_URL"`
	TrustedProxies Prefixes     `yaml:"trusted_proxies" env:"TRUSTED_PROXIES"`
	SecretKey      string       `yaml:"-"               env:"SECRET_KEY" secret:"true"`
	Log            LogConfig    `yaml:"log"             envPrefix:"LOG_"`
	MaxUploadSize  ByteSize     `yaml:"max_upload_size" env:"MAX_UPLOAD_SIZE"`
	MaxIngestBody  ByteSize     `yaml:"max_ingest_body" env:"MAX_INGEST_BODY"`
	MetricsAddr    string       `yaml:"metrics_addr"    env:"METRICS_ADDR"`
	Audit          AuditConfig  `yaml:"audit"           envPrefix:"AUDIT_"`
	Backup         BackupConfig `yaml:"backup"          envPrefix:"BACKUP_"`
	RawMaxDays     int          `yaml:"raw_max_days"    env:"RAW_MAX_DAYS"`

	// Sources tells where each setting comes from, keyed by YAML path. Set by Load.
	Sources Sources `yaml:"-" env:"-"`
}

// LogConfig configures logging.
type LogConfig struct {
	Level  string `yaml:"level"  env:"LEVEL"`
	Format string `yaml:"format" env:"FORMAT"`
}

// AuditConfig configures the audit log.
type AuditConfig struct {
	// RetentionDays is the number of days an audit event is kept after its last occurrence.
	RetentionDays int `yaml:"retention_days" env:"RETENTION_DAYS"`
}

// BackupConfig configures the scheduled backups.
type BackupConfig struct {
	// Schedule is a cron expression with the standard five fields, read in the local time of the
	// process, or BackupScheduleOff.
	Schedule string `yaml:"schedule" env:"SCHEDULE"`
	// Keep is the number of scheduled backups that retention keeps.
	Keep int `yaml:"keep" env:"KEEP"`
}

// BackupScheduleOff is the schedule of an instance without scheduled backups.
const BackupScheduleOff = "off"

// Cron returns the cron expression of the scheduled backups, or an empty string when they are off.
func (b BackupConfig) Cron() string {
	if b.Schedule == BackupScheduleOff {
		return ""
	}

	return b.Schedule
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
		Audit:         AuditConfig{RetentionDays: 365},
		Backup:        BackupConfig{Schedule: "0 3 * * *", Keep: 7},
		RawMaxDays:    7,
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

	between := func(path string, n, lowest, highest int) {
		switch {
		case n < lowest:
			invalid(path, strconv.Itoa(n), "must be at least "+strconv.Itoa(lowest))
		case n > highest:
			invalid(path, strconv.Itoa(n), "must be at most "+strconv.Itoa(highest))
		}
	}

	oneOf("log.level", c.Log.Level, logLevels)
	oneOf("log.format", c.Log.Format, logFormats)
	positive("max_upload_size", c.MaxUploadSize)
	positive("max_ingest_body", c.MaxIngestBody)
	between("audit.retention_days", c.Audit.RetentionDays, 1, maxAuditRetentionDays)
	between("backup.keep", c.Backup.Keep, 1, math.MaxInt)
	between("raw_max_days", c.RawMaxDays, 1, maxRawMaxDays)

	if c.Backup.Schedule != BackupScheduleOff {
		if err := jobs.CheckCron(c.Backup.Schedule); err != nil {
			invalid("backup.schedule", c.Backup.Schedule, "must be "+BackupScheduleOff+" or a cron expression with five fields ("+err.Error()+")")
		}
	}

	// Not through invalid, which would print the key.
	if c.SecretKey != "" {
		if err := CheckSecretKey(c.SecretKey); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", settings["secret_key"].name(), err))
		}
	}

	return errs
}

// CheckSecretKey rejects a secret key that is too short, wherever it comes from. The error does
// not hold the key.
func CheckSecretKey(key string) error {
	if utf8.RuneCountInString(key) < minSecretKeyLength {
		return fmt.Errorf("must be at least %d characters", minSecretKeyLength)
	}

	return nil
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
