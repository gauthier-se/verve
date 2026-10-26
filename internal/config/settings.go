package config

import (
	"reflect"
	"strings"
)

// tagField describes a struct field that is part of a configuration, from its tags:
// `yaml` gives the key, `env` the environment variable of a leaf, `envPrefix` marks a
// group of nested settings and `secret:"true"` hides the value when printed.
type tagField struct {
	index     int
	goName    string
	yaml      string
	env       string
	envPrefix string
	group     bool
	secret    bool
}

// fieldsOf returns the configuration fields of a struct type in declaration order,
// skipping unexported fields and fields without a YAML key.
func fieldsOf(t reflect.Type) []tagField {
	var fields []tagField

	for i := range t.NumField() {
		sf := t.Field(i)

		key, _, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
		if !sf.IsExported() || key == "" || key == "-" {
			continue
		}

		envKey, _, _ := strings.Cut(sf.Tag.Get("env"), ",")
		prefix, group := sf.Tag.Lookup("envPrefix")

		fields = append(fields, tagField{
			index:     i,
			goName:    sf.Name,
			yaml:      key,
			env:       envKey,
			envPrefix: prefix,
			group:     group,
			secret:    sf.Tag.Get("secret") == "true",
		})
	}

	return fields
}

// setting is a leaf of a configuration.
type setting struct {
	path   string        // YAML path, for example "log.level"
	env    string        // environment variable, for example "VERVE_LOG_LEVEL"
	goName string        // Go field name, as reported by the environment parser
	value  reflect.Value // the field, settable when the configuration was addressable
}

// name identifies the setting in error messages, for example "log.level (VERVE_LOG_LEVEL)".
func (s setting) name() string {
	return s.path + " (" + s.env + ")"
}

// settingsOf lists the leaf settings of a configuration struct value.
func settingsOf(v reflect.Value) []setting {
	return appendSettings(nil, v, "", envPrefix)
}

// indexSettings maps each setting by its YAML path.
func indexSettings(settings []setting) map[string]setting {
	byPath := make(map[string]setting, len(settings))
	for _, s := range settings {
		byPath[s.path] = s
	}

	return byPath
}

func appendSettings(settings []setting, v reflect.Value, path, env string) []setting {
	for _, f := range fieldsOf(v.Type()) {
		fv := v.Field(f.index)

		if f.group {
			settings = appendSettings(settings, fv, path+f.yaml+".", env+f.envPrefix)

			continue
		}

		settings = append(settings, setting{
			path:   path + f.yaml,
			env:    env + f.env,
			goName: f.goName,
			value:  fv,
		})
	}

	return settings
}
