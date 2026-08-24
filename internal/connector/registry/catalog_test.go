package registry

import (
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/connector/applehealth"
	"github.com/gauthier-se/verve/internal/connector/googlehealth"
)

// TestCatalogHasNoOrphan guards the Catalog's half of ADR 0009: every imported
// Metric is claimed by at least one registered Connector, so no slug sits in the
// Catalog that nothing can ever write.
//
// It lives here, and asks the Connectors rather than one of them, because that is
// what the invariant says. Until a second Connector existed the same assertion was
// written inside applehealth as "every imported Metric has an Apple mapping", which
// read as an invariant about the Catalog and was in fact a statement that the
// Catalog was Apple's. A Metric only Google reports, distance, which Google does
// not split by activity, is what made the difference visible.
//
// Derived Metrics are excluded: they are computed from other Metrics and have no
// source to be mapped from (ADR 0014).
func TestCatalogHasNoOrphan(t *testing.T) {
	claimed := map[string]bool{}
	for _, slug := range applehealth.MappedSlugs() {
		claimed[slug] = true
	}
	for _, slug := range googlehealth.MappedSlugs() {
		claimed[slug] = true
	}

	for slug, m := range catalog.All() {
		if m.Nature != catalog.Imported {
			continue
		}
		if !claimed[slug] {
			t.Errorf("imported Catalog metric %q is claimed by no Connector", slug)
		}
	}
}

// Every slug a Connector claims must be a Catalog Metric or a State kind Verve
// stores. This is the forward direction at the registry level: a Connector's own
// package asserts it too, and this catches a Connector added without that test.
func TestClaimedSlugsAreKnown(t *testing.T) {
	// "stand" is a State kind and deliberately not a Metric: apple_stand_time
	// already answers that question as a Measurement (CONTEXT.md).
	stateKinds := map[string]bool{"stand": true}

	for _, slug := range append(applehealth.MappedSlugs(), googlehealth.MappedSlugs()...) {
		if _, ok := catalog.Lookup(slug); !ok && !stateKinds[slug] {
			t.Errorf("Connector claims %q, which is neither a Catalog Metric nor a stored State kind", slug)
		}
	}
}
