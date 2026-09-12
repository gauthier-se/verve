// Package verve is the Connector that reads a Verve Archive back in (ADR 0039).
//
// It is the third Connector and the only one with no mapping table. Every other
// Connector's bulk is the declarative map from a source type to a Catalog Metric
// (ADR 0009); this one's source is already the canonical model, so what is left
// is a reader, a validator and the Sink every Connector shares. If you came here
// looking for the mapping file, there isn't one.
//
// Reading an Archive is a Connector rather than a restore path because the
// contract already describes it: a file the owner exported, written through the
// canonical Store, deduped by content key, recorded as an Import, reported like
// any other run, and filtered by the Account's Exclusions (ADR 0033). A bespoke
// restore would have had to reinvent all five.
package verve

import (
	"archive/zip"
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/gauthier-se/verve/internal/archive"
	"github.com/gauthier-se/verve/internal/connector"
)

// connectorName is this Connector's stable identifier, recorded with every
// Import it runs (never shown as a label, see Label).
const connectorName = "verve"

// Connector is the Verve Archive Connector. It is a value type with no state;
// the registry holds one.
type Connector struct{}

// Name implements connector.Connector.
func (Connector) Name() string { return connectorName }

// Label implements connector.Connector.
func (Connector) Label() string { return "Verve Archive" }

// Accepts recognizes an Archive by its manifest's marker, which costs one small
// entry read. It is disjoint from the other two Connectors by construction: an
// Apple export declares HealthData, a Takeout has its Google Health directory,
// and neither carries a manifest.json with a verve_archive field.
//
// The marker is checked, not the schema version: an Archive from a newer Verve
// is still recognizably an Archive, and is refused by Import with a message
// naming the version rather than falling through to "no connector reads this".
func (Connector) Accepts(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()

	m, err := readManifest(&zr.Reader)
	return err == nil && m.VerveArchive > 0
}

// Import implements connector.Connector.
func (Connector) Import(ctx context.Context, store connector.Store, accountID int64, path string, opts connector.Options) (connector.Report, error) {
	return Import(ctx, store, accountID, path, opts)
}

// readManifest reads and parses the manifest entry. A zip without one is not an
// Archive, which is the whole of the detection.
func readManifest(zr *zip.Reader) (archive.Manifest, error) {
	var m archive.Manifest
	f, err := zr.Open(archive.ManifestName)
	if err != nil {
		return m, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return m, err
	}
	return m, nil
}

// sourceName is what the Import records as the file it read.
func sourceName(path string) string { return filepath.Base(path) }
