package config

import (
	"errors"
	"net/netip"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Prefixes is a list of IP prefixes. As text it is comma-separated, in YAML it is a list.
// A single address is accepted as a host prefix (/32 or /128).
type Prefixes []netip.Prefix

// UnmarshalText implements encoding.TextUnmarshaler for comma-separated values.
func (p *Prefixes) UnmarshalText(text []byte) error {
	return p.set(strings.Split(string(text), ","))
}

// UnmarshalYAML implements yaml.Unmarshaler: the YAML form is a list.
func (p *Prefixes) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return errors.New("must be a list of IP addresses or CIDR prefixes")
	}

	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return errors.New("must be a list of IP addresses or CIDR prefixes")
		}

		values = append(values, item.Value)
	}

	return p.set(values)
}

// MarshalYAML implements yaml.Marshaler: the YAML form is a list, empty when unset.
func (p Prefixes) MarshalYAML() (any, error) {
	values := make([]string, 0, len(p))
	for _, prefix := range p {
		values = append(values, prefix.String())
	}

	return values, nil
}

func (p *Prefixes) set(values []string) error {
	prefixes := make(Prefixes, 0, len(values))

	for _, value := range values {
		prefix, err := parsePrefix(strings.TrimSpace(value))
		if err != nil {
			return err
		}

		prefixes = append(prefixes, prefix)
	}

	*p = prefixes

	return nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	invalid := invalidValue(s, "must be an IP address or a CIDR prefix such as 10.0.0.0/8")

	if strings.Contains(s, "/") {
		prefix, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, invalid
		}

		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return netip.Prefix{}, invalid
	}

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}
