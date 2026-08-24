package applehealth

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gauthier-se/verve/internal/connector"
)

// connectorName is this Connector's stable identifier, recorded with every Import
// it runs (never shown as a label, see Label).
const connectorName = "applehealth"

// Connector is the Apple Health export Connector: an export.zip, or the bare
// export.xml inside it. It is a value type with no state; the registry holds one.
type Connector struct{}

// Name implements connector.Connector.
func (Connector) Name() string { return connectorName }

// Label implements connector.Connector.
func (Connector) Label() string { return "Apple Health" }

// Accepts recognizes an Apple export by its content and never by its extension:
// an archive holding an export.xml entry (Apple nests it under
// apple_health_export/), or a bare XML file declaring HealthData. Both Connectors
// ship a .zip, so the extension settles nothing.
func (Connector) Accepts(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return false
		}
		defer zr.Close()
		return findExportXML(&zr.Reader) != nil
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	// The DOCTYPE naming HealthData sits within the first few hundred bytes, right
	// after the XML declaration.
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	return bytes.Contains(head[:n], []byte("HealthData"))
}

// Import implements connector.Connector.
func (Connector) Import(ctx context.Context, store connector.Store, accountID int64, path string, opts connector.Options) (connector.Report, error) {
	return Import(ctx, store, accountID, path, opts)
}
