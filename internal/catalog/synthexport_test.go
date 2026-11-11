package catalog_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/synthexport"
)

// recordValues returns the values written by the Record elements of the export.xml of a synthetic
// export, by record type. A quantity record writes a number, which the caller ignores.
func recordValues(t *testing.T) map[string][]string {
	t.Helper()

	var archive bytes.Buffer
	require.NoError(t, synthexport.Write(t.Context(), &archive, synthexport.Fixture()))

	zr, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)

	f, err := zr.Open("apple_health_export/export.xml")
	require.NoError(t, err)

	defer func() { require.NoError(t, f.Close()) }()

	values := map[string][]string{}
	dec := xml.NewDecoder(f)

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return values
		}

		require.NoError(t, err)

		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "Record" {
			continue
		}

		var typ, value string

		for _, a := range start.Attr {
			switch a.Name.Local {
			case "type":
				typ = a.Value
			case "value":
				value = a.Value
			}
		}

		if !slices.Contains(values[typ], value) {
			values[typ] = append(values[typ], value)
		}
	}
}

// Given the record types written by internal/synthexport, then each is in the catalog, except
// HKDataTypeSleepDurationGoal, which stays unknown.
func TestSynthexport_RecordTypesAreInTheCatalog(t *testing.T) {
	t.Parallel()

	c := shipped(t)
	types := slices.Sorted(maps.Keys(recordValues(t)))
	require.Contains(t, types, "HKDataTypeSleepDurationGoal")

	for _, identifier := range types {
		_, status := c.Lookup(identifier)

		if identifier == "HKDataTypeSleepDurationGoal" {
			require.Equal(t, catalog.Unknown, status, identifier)

			continue
		}

		require.Equal(t, catalog.Known, status, identifier)
	}
}

// Given the category records written by internal/synthexport, then each value is a value of its type.
func TestSynthexport_CategoryValuesAreInTheCatalog(t *testing.T) {
	t.Parallel()

	c := shipped(t)

	for identifier, values := range recordValues(t) {
		typ, status := c.Lookup(identifier)
		if status != catalog.Known || typ.Kind != catalog.KindCategory {
			continue
		}

		known := make([]string, 0, len(typ.Values))
		for _, v := range typ.Values {
			known = append(known, v.Identifier)
		}

		for _, value := range values {
			require.Contains(t, known, value, identifier)
		}
	}
}
