package synthexport

import (
	"fmt"
	"math/rand/v2"
	"time"
)

const (
	typeSystolic  = "HKQuantityTypeIdentifierBloodPressureSystolic"
	typeDiastolic = "HKQuantityTypeIdentifierBloodPressureDiastolic"
)

var (
	sourceFood = source{name: "Nutrio", version: "1200"}
	sourceCuff = source{name: "Cardio Cuff", version: "42"}
)

// correlation is a Correlation element. A real export writes its records twice: inside it and at
// top level, which is why correlationsOn derives them from the day alone.
type correlation struct {
	typ      string
	source   source
	at       time.Time
	metadata []metadata
	records  []record
}

// nutrient is a quantity of every meal.
type nutrient struct {
	typ, unit string
	value     func(r *rand.Rand, sample int) string
}

func nutrients() []nutrient {
	return []nutrient{
		{"HKQuantityTypeIdentifierDietaryEnergyConsumed", "kcal", decimal(150, 800, 1)},
		{"HKQuantityTypeIdentifierDietaryProtein", "g", decimal(5, 45, 1)},
		{"HKQuantityTypeIdentifierDietaryCarbohydrates", "g", decimal(10, 90, 1)},
		{"HKQuantityTypeIdentifierDietaryFatTotal", "g", decimal(3, 40, 2)},
		{"HKQuantityTypeIdentifierDietaryFiber", "g", decimal(0, 12, 2)},
	}
}

// correlationsOn returns the meals and the blood pressure reading of a day.
func (e exporter) correlationsOn(day int) []correlation {
	meals := []struct {
		at   time.Duration
		meta metadata
	}{
		{8 * time.Hour, metadata{"HKFoodType", "Yaourt & fruits"}},
		{12*time.Hour + 30*time.Minute, metadata{"HKFoodType", "Salade d'été"}},
		{16 * time.Hour, metadata{"Repas", "Goûter"}},
		{20 * time.Hour, metadata{"HKFoodType", "Pâtes à la crème"}},
	}

	r := e.rng(streamCorrelations, day)
	correlations := make([]correlation, 0, len(meals)+1)

	for _, meal := range meals {
		at := e.dayStart(day).Add(meal.at)
		meta := []metadata{{"HKExternalUUID", uuid(r)}, meal.meta}
		records := make([]record, 0, len(nutrients()))

		for _, n := range nutrients() {
			records = append(records, record{
				typ: n.typ, unit: n.unit, value: n.value(r, 0), source: sourceFood, start: at, end: at, metadata: meta,
			})
		}

		correlations = append(correlations, correlation{
			typ: "HKCorrelationTypeIdentifierFood", source: sourceFood, at: at, metadata: meta, records: records,
		})
	}

	at := e.dayStart(day).Add(8*time.Hour + 15*time.Minute)
	entered := []metadata{{"HKWasUserEntered", "1"}}

	return append(correlations, correlation{
		typ: "HKCorrelationTypeIdentifierBloodPressure", source: sourceCuff, at: at, metadata: entered,
		records: []record{
			{
				typ: typeSystolic, unit: "mmHg", value: integer(105, 135)(r, 0), source: sourceCuff,
				start: at, end: at, metadata: entered,
			},
			{
				typ: typeDiastolic, unit: "mmHg", value: integer(65, 85)(r, 0), source: sourceCuff,
				start: at, end: at, metadata: entered,
			},
		},
	})
}

// correlatedSections returns what writes, at top level, each type that correlations hold.
func (e exporter) correlatedSections() []func(day int, d document) {
	types := []string{typeSystolic, typeDiastolic}
	for _, n := range nutrients() {
		types = append(types, n.typ)
	}

	sections := make([]func(int, document), 0, len(types))

	for _, typ := range types {
		sections = append(sections, func(day int, d document) {
			for _, c := range e.correlationsOn(day) {
				for _, r := range c.records {
					if r.typ == typ {
						e.writeRecord(d, 1, r)
					}
				}
			}
		})
	}

	return sections
}

func (e exporter) writeCorrelations(day int, d document) {
	for _, c := range e.correlationsOn(day) {
		d.open(1, "Correlation",
			"type", c.typ,
			"sourceName", c.source.name,
			"sourceVersion", c.source.version,
			"creationDate", e.date(c.at),
			"startDate", e.date(c.at),
			"endDate", e.date(c.at))
		writeMetadata(d, 2, c.metadata)

		for _, r := range c.records {
			e.writeRecord(d, 2, r)
		}

		d.closeTag(1, "Correlation")
	}
}

// uuid draws an identifier in the format of HealthKit.
func uuid(r *rand.Rand) string {
	const (
		mask16 = 0xffff
		mask48 = 0xffffffffffff
	)

	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		r.Uint32(), r.Uint32()&mask16, r.Uint32()&mask16, r.Uint32()&mask16, r.Uint64()&mask48)
}
