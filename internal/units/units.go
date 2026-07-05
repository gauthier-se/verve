// Package units converts a scalar value between measurement units of the same
// physical dimension. Connectors use it to normalize source values to a
// Metric's canonical unit at import time (see ADR 0002): the Catalog fixes one
// canonical unit per Metric, and whatever unit a Source reports is converted to
// it, so a series never mixes units.
//
// The table is deliberately small — the dimensions Apple Health emits plus the
// common alternates other Sources use. Units with no meaningful alternate (W,
// dBASPL, count/min…) are absent: converting such a unit to itself succeeds via
// the identity fast-path, and converting it to anything else is an error.
package units

import "fmt"

// dimension groups units that can convert between one another. Two units are
// interconvertible iff they share a dimension.
type dimension string

const (
	dimMass    dimension = "mass"
	dimLength  dimension = "length"
	dimEnergy  dimension = "energy"
	dimVolume  dimension = "volume"
	dimTime    dimension = "time"
	dimSpeed   dimension = "speed"
	dimPercent dimension = "percent"
	dimCount   dimension = "count"
)

// unitDef records a unit's dimension and its size in that dimension's base
// unit, so a value converts as value * from.factor / to.factor.
type unitDef struct {
	dim    dimension
	factor float64
}

// table maps a unit symbol to its definition. Base units (factor 1): gram,
// metre, calorie, millilitre, second, metre-per-second.
var table = map[string]unitDef{
	// mass — base gram
	"kg":  {dimMass, 1000},
	"g":   {dimMass, 1},
	"mg":  {dimMass, 0.001},
	"mcg": {dimMass, 1e-6},
	"µg":  {dimMass, 1e-6},
	"lb":  {dimMass, 453.59237},
	"oz":  {dimMass, 28.349523125},
	"st":  {dimMass, 6350.29318},

	// length — base metre
	"km": {dimLength, 1000},
	"m":  {dimLength, 1},
	"cm": {dimLength, 0.01},
	"mm": {dimLength, 0.001},
	"mi": {dimLength, 1609.344},
	"ft": {dimLength, 0.3048},
	"in": {dimLength, 0.0254},
	"yd": {dimLength, 0.9144},

	// energy — base calorie
	"kcal": {dimEnergy, 1000},
	"cal":  {dimEnergy, 1},
	"kJ":   {dimEnergy, 239.005736},
	"J":    {dimEnergy, 0.239005736},

	// volume — base millilitre
	"L":  {dimVolume, 1000},
	"mL": {dimVolume, 1},
	"l":  {dimVolume, 1000},
	"ml": {dimVolume, 1},

	// time — base second
	"hr":  {dimTime, 3600},
	"min": {dimTime, 60},
	"s":   {dimTime, 1},
	"sec": {dimTime, 1},
	"ms":  {dimTime, 0.001},

	// speed — base metre-per-second
	"m/s":   {dimSpeed, 1},
	"km/hr": {dimSpeed, 1000.0 / 3600.0},
	"km/h":  {dimSpeed, 1000.0 / 3600.0},
	"mph":   {dimSpeed, 0.44704},

	// dimensionless
	"%":     {dimPercent, 1},
	"count": {dimCount, 1},
}

// Convert returns value expressed in unit to, given it is currently in unit
// from. Identical units convert without a table lookup, so opaque units (units
// with no alternate, e.g. "W" or "count/min") still normalize to themselves.
// Converting between different units of different dimensions — or between any
// unknown unit — is an error.
func Convert(value float64, from, to string) (float64, error) {
	if from == to {
		return value, nil
	}
	if v, ok, err := convertTemperature(value, from, to); ok {
		return v, err
	}
	f, okFrom := table[from]
	t, okTo := table[to]
	if !okFrom || !okTo || f.dim != t.dim {
		return 0, fmt.Errorf("units: cannot convert %q to %q", from, to)
	}
	return value * f.factor / t.factor, nil
}

// Temperature scales are affine (an offset, not just a factor) and so cannot
// live in the factor table; these bridge each scale through Celsius.
var (
	toCelsius = map[string]func(float64) float64{
		"degC": func(v float64) float64 { return v },
		"degF": func(v float64) float64 { return (v - 32) * 5 / 9 },
		"K":    func(v float64) float64 { return v - 273.15 },
	}
	fromCelsius = map[string]func(float64) float64{
		"degC": func(v float64) float64 { return v },
		"degF": func(v float64) float64 { return v*9/5 + 32 },
		"K":    func(v float64) float64 { return v + 273.15 },
	}
)

// convertTemperature handles the temperature scales. ok is false when neither
// unit is a temperature, letting Convert fall through to the factor table.
func convertTemperature(value float64, from, to string) (result float64, ok bool, err error) {
	toC, isFromTemp := toCelsius[from]
	conv, isToTemp := fromCelsius[to]
	if !isFromTemp && !isToTemp {
		return 0, false, nil
	}
	if !isFromTemp || !isToTemp {
		return 0, true, fmt.Errorf("units: cannot convert %q to %q", from, to)
	}
	return conv(toC(value)), true, nil
}
