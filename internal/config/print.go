package config

import (
	"fmt"
	"io"
	"reflect"

	"go.yaml.in/yaml/v3"
)

// secretMask replaces the value of a secret setting when printed.
const secretMask = "********"

// WriteYAML prints the effective configuration as YAML, each value annotated with its
// source and secrets masked.
func (c Config) WriteYAML(w io.Writer) error {
	return writeAnnotatedYAML(w, c, c.Sources)
}

// writeAnnotatedYAML prints a configuration struct as YAML, each value annotated with its source
// (`# default`, `# file`, `# env`). Fields tagged `secret:"true"` that have a value are
// printed as ********. A setting missing from sources is reported as default.
func writeAnnotatedYAML(w io.Writer, v any, sources Sources) error {
	node, err := mappingNode(reflect.ValueOf(v), "", sources)
	if err != nil {
		return err
	}

	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)

	if err := enc.Encode(node); err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}

	return nil
}

func mappingNode(v reflect.Value, prefix string, sources Sources) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}

	for _, f := range fieldsOf(v.Type()) {
		fv := v.Field(f.index)
		path := prefix + f.yaml
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: f.yaml}

		if f.group {
			child, err := mappingNode(fv, path+".", sources)
			if err != nil {
				return nil, err
			}

			node.Content = append(node.Content, key, child)

			continue
		}

		value := &yaml.Node{}
		if f.secret && !fv.IsZero() {
			value = &yaml.Node{Kind: yaml.ScalarNode, Value: secretMask, Style: yaml.DoubleQuotedStyle}
		} else if err := value.Encode(fv.Interface()); err != nil {
			return nil, fmt.Errorf("encode %s: %w", path, err)
		}

		source := sources[path]
		if source == "" {
			source = SourceDefault
		}

		// A comment after a block value would land below it: put it on the key instead.
		if value.Kind == yaml.ScalarNode || len(value.Content) == 0 {
			value.LineComment = string(source)
		} else {
			key.LineComment = string(source)
		}

		node.Content = append(node.Content, key, value)
	}

	return node, nil
}
