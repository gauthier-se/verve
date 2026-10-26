package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/caarlos0/env/v11"
	"go.yaml.in/yaml/v3"
)

// Source tells where the value of a setting comes from.
type Source string

// Sources, from lowest to highest precedence. M1 adds a database source between file and default.
const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
)

// Sources maps the YAML path of each setting, for example "log.level", to its source.
type Sources map[string]Source

// LoadOptions are the inputs of Load.
type LoadOptions struct {
	// File is the YAML configuration file given with --config. When empty, VERVE_CONFIG is used.
	File string
	// Env holds the environment variables. The CLI converts os.Environ into it; tests pass
	// a map directly so they can run in parallel.
	Env map[string]string
}

// Load builds the configuration from the defaults, the YAML file and the environment, in
// increasing precedence, then validates it. The error joins every problem found.
func Load(opts LoadOptions) (Config, error) {
	environ := opts.Env
	if environ == nil {
		// The environment parser falls back to the process environment on a nil map.
		environ = map[string]string{}
	}

	cfg := Default()
	settings := cfg.settings()

	cfg.Sources = make(Sources, len(settings))
	for path := range settings {
		cfg.Sources[path] = SourceDefault
	}

	var errs []error

	file := opts.File
	if file == "" {
		file = environ[envPrefix+"CONFIG"]
	}

	if file != "" {
		errs = append(errs, loadFile(file, settings, cfg.Sources)...)
	}

	errs = append(errs, loadEnv(&cfg, environ, settings, cfg.Sources)...)
	errs = append(errs, cfg.validate(settings)...)

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// loadFile decodes each known setting of the YAML file into its field and reports unknown keys.
func loadFile(path string, settings map[string]setting, sources Sources) []error {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the operator chooses the file with --config or VERVE_CONFIG
	if err != nil {
		return []error{fmt.Errorf("read config file: %w", err)}
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return []error{fmt.Errorf("parse config file %s: %w", path, err)}
	}

	if len(root.Content) == 0 {
		return nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return []error{fmt.Errorf("parse config file %s: line %d: must be a mapping of settings", path, doc.Line)}
	}

	l := fileLoader{path: path, settings: settings, groups: map[string]bool{}, sources: sources}
	for p := range settings {
		// Every parent of a nested setting is a group: "log" for "log.level".
		for strings.Contains(p, ".") {
			p = p[:strings.LastIndex(p, ".")]
			l.groups[p] = true
		}
	}

	l.mapping(doc, "")

	return l.errs
}

type fileLoader struct {
	path     string
	settings map[string]setting
	groups   map[string]bool
	sources  Sources
	errs     []error
}

func (l *fileLoader) mapping(node *yaml.Node, prefix string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		path := prefix + key.Value

		// An empty value (`key:` or `key: ~`) leaves the setting or group unset.
		isNull := value.ShortTag() == "!!null"

		if s, ok := l.settings[path]; ok {
			if isNull {
				continue
			}

			if err := value.Decode(s.value.Addr().Interface()); err != nil {
				l.errs = append(l.errs, fmt.Errorf("%s:%d: %s: %w", l.path, value.Line, s.name(), err))

				continue
			}

			l.sources[path] = SourceFile

			continue
		}

		if l.groups[path] {
			switch {
			case isNull:
			case value.Kind == yaml.MappingNode:
				l.mapping(value, path+".")
			default:
				l.errs = append(l.errs, fmt.Errorf("%s:%d: %s: must be a mapping of settings", l.path, value.Line, path))
			}

			continue
		}

		l.errs = append(l.errs, fmt.Errorf("%s:%d: unknown key %q", l.path, key.Line, path))
	}
}

// loadEnv applies the VERVE_* environment variables to target, a pointer to the configuration
// struct that settings describe. Empty variables are ignored.
func loadEnv(target any, environ map[string]string, settings map[string]setting, sources Sources) []error {
	for _, s := range settings {
		if environ[s.env] != "" {
			sources[s.path] = SourceEnv
		}
	}

	err := env.ParseWithOptions(target, env.Options{Prefix: envPrefix, Environment: environ})
	if err == nil {
		return nil
	}

	agg, ok := errors.AsType[env.AggregateError](err)
	if !ok {
		return []error{fmt.Errorf("parse environment: %w", err)}
	}

	errs := make([]error, 0, len(agg.Errors))
	for _, e := range agg.Errors {
		errs = append(errs, describeEnvError(e, environ, settings))
	}

	return errs
}

// describeEnvError names the setting of a parse error. The parser only reports the Go field
// name and type, which nested groups may share, so only settings whose variable is set qualify.
func describeEnvError(err error, environ map[string]string, settings map[string]setting) error {
	parseErr, ok := errors.AsType[env.ParseError](err)
	if !ok {
		return fmt.Errorf("parse environment: %w", err)
	}

	var names []string
	for _, s := range settings {
		if s.goName == parseErr.Name && s.value.Type() == parseErr.Type && environ[s.env] != "" {
			names = append(names, s.name())
		}
	}

	if len(names) == 0 {
		return fmt.Errorf("parse environment: %w", err)
	}

	slices.Sort(names)

	return fmt.Errorf("%s: %w", strings.Join(names, " or "), parseErr.Err)
}
