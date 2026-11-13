package ingest

import (
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// ConvertValue converts value, received in unit, to the unit of t (FR-2 stage 2 of Ingestion
// pipeline), with the Units table of docs/architecture.md. A curated type converts to its canonical
// unit. An uncurated type has the unit of its first record: a value of that unit is kept as
// received, and a value of another unit converts to it.
//
// It returns an error naming the unit when the table does not list both units, or lists them for
// different dimensions: the record is then invalid.
func ConvertValue(value float64, unit string, t store.MetricType) (float64, error) {
	if !t.Curated && unit == t.Unit {
		return value, nil
	}

	return catalog.Convert(value, unit, t.Unit)
}
