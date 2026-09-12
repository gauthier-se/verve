// Package registry is the compiled-in set of Connectors (ADR 0009): the one place
// that knows which sources Verve can read, and the one file a contributed
// Connector edits beyond its own package.
//
// It sits beside internal/connector rather than inside it so the contract can be
// imported by the implementations without an import cycle: the types point down,
// the registry points up.
package registry

import (
	"fmt"
	"os"

	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/connector/applehealth"
	"github.com/gauthier-se/verve/internal/connector/googlehealth"
	"github.com/gauthier-se/verve/internal/connector/verve"
)

// connectors is every compiled-in Connector, in the order they are asked. Their
// detectors are disjoint by construction, so the order is presentation, not
// precedence.
var connectors = []connector.Connector{
	applehealth.Connector{},
	googlehealth.Connector{},
	verve.Connector{},
}

// All returns the registered Connectors. The returned slice must not be mutated.
func All() []connector.Connector { return connectors }

// Labels returns the registered Connectors as a person names them, for a message
// that has to list what Verve reads.
func Labels() []string {
	out := make([]string, 0, len(connectors))
	for _, c := range connectors {
		out = append(out, c.Label())
	}
	return out
}

// For returns the Connector that accepts path, or connector.ErrUnknownExport.
// Detection is by content, so a caller never has to ask the owner which export
// they have: the file already answers.
func For(path string) (connector.Connector, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: %s", connector.ErrUnknownExport, path)
	}
	for _, c := range connectors {
		if c.Accepts(path) {
			return c, nil
		}
	}
	return nil, connector.ErrUnknownExport
}
