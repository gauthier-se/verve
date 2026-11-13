package catalog

import "fmt"

// unit is a unit string of the Units table of docs/architecture.md. A value in the unit is
// (value + offset) * factor in the base unit of its dimension. Base units are chosen so that most
// factors are integers, which keeps conversions exact to the precision of a float64.
type unit struct {
	dimension string
	factor    float64
	offset    float64
	// canonical is true for the canonical unit of its row.
	canonical bool
}

// units are the unit strings that the pipeline accepts, matched exactly, as HealthKit writes them.
// Duration and short duration are one dimension, time, with two canonical units.
var units = map[string]unit{
	"count": {dimension: "count", factor: 1, canonical: true},

	// Base: mcg.
	"kg":  {dimension: "mass", factor: 1e9, canonical: true},
	"g":   {dimension: "mass", factor: 1e6},
	"mg":  {dimension: "mass", factor: 1e3},
	"mcg": {dimension: "mass", factor: 1},
	"lb":  {dimension: "mass", factor: 453_592_370},
	"oz":  {dimension: "mass", factor: 28_349_523.125},
	"st":  {dimension: "mass", factor: 6_350_293_180},

	// Base: mm.
	"m":  {dimension: "length", factor: 1e3, canonical: true},
	"km": {dimension: "length", factor: 1e6},
	"cm": {dimension: "length", factor: 10},
	"mm": {dimension: "length", factor: 1},
	"in": {dimension: "length", factor: 25.4},
	"ft": {dimension: "length", factor: 304.8},
	"yd": {dimension: "length", factor: 914.4},
	"mi": {dimension: "length", factor: 1_609_344},

	// Base: mL. The ounces, cups and pints are the US customary and imperial ones, as HKUnit.h
	// defines them: a US cup is 0.2365882365 L, not the 240 mL cup of nutrition labels.
	"L":         {dimension: "volume", factor: 1e3, canonical: true},
	"mL":        {dimension: "volume", factor: 1},
	"fl_oz_us":  {dimension: "volume", factor: 29.5735295625},
	"fl_oz_imp": {dimension: "volume", factor: 28.4130625},
	"cup_us":    {dimension: "volume", factor: 236.5882365},
	"cup_imp":   {dimension: "volume", factor: 284.130625},
	"pt_us":     {dimension: "volume", factor: 473.176473},
	"pt_imp":    {dimension: "volume", factor: 568.26125},

	// Base: J, with the thermochemical calorie of HealthKit. Cal is the food calorie, a kcal.
	"kcal": {dimension: "energy", factor: 4184, canonical: true},
	"Cal":  {dimension: "energy", factor: 4184},
	"cal":  {dimension: "energy", factor: 4.184},
	"kJ":   {dimension: "energy", factor: 1e3},
	"J":    {dimension: "energy", factor: 1},

	// Base: ms.
	"s":   {dimension: "time", factor: 1e3, canonical: true},
	"ms":  {dimension: "time", factor: 1, canonical: true},
	"min": {dimension: "time", factor: 60e3},
	"hr":  {dimension: "time", factor: 3_600e3},
	"d":   {dimension: "time", factor: 86_400e3},

	// Base: count/min.
	"count/min": {dimension: "rate", factor: 1, canonical: true},
	"count/s":   {dimension: "rate", factor: 60},

	// Base: m/hr.
	"m/s":   {dimension: "speed", factor: 3600, canonical: true},
	"km/hr": {dimension: "speed", factor: 1e3},
	"mi/hr": {dimension: "speed", factor: 1_609.344},

	// Base: W.
	"W":  {dimension: "power", factor: 1, canonical: true},
	"kW": {dimension: "power", factor: 1e3},

	// Base: degC.
	"degC": {dimension: "temperature", factor: 1, canonical: true},
	"degF": {dimension: "temperature", factor: 5.0 / 9, offset: -32},
	"K":    {dimension: "temperature", factor: 1, offset: -273.15},

	// HealthKit writes percentages as fractions: 0.97 % is 97 %.
	"%": {dimension: "percentage", factor: 1},

	// Base: mg/L.
	"mg/dL":                      {dimension: "blood_glucose", factor: 10, canonical: true},
	"mmol<180.15588000005408>/L": {dimension: "blood_glucose", factor: 180.15588000005408},

	// Base: Pa, with the conventional millimeter of mercury.
	"mmHg": {dimension: "pressure", factor: 133.322387415, canonical: true},
	"kPa":  {dimension: "pressure", factor: 1e3},

	"mL/min·kg":        {dimension: "vo2max", factor: 1},
	"kcal/hr·kg":       {dimension: "physical_effort", factor: 1},
	"appleEffortScore": {dimension: "effort_score", factor: 1, canonical: true},
	"dBASPL":           {dimension: "sound_level", factor: 1, canonical: true},
	"L/min":            {dimension: "air_flow", factor: 1, canonical: true},

	// Base: mcS.
	"S":   {dimension: "conductance", factor: 1e6, canonical: true},
	"mS":  {dimension: "conductance", factor: 1e3},
	"mcS": {dimension: "conductance", factor: 1},

	"IU": {dimension: "insulin", factor: 1, canonical: true},
}

// canonicalNames are the canonical units that are not unit strings, by the unit string that they
// convert as. A value is never received in them.
var canonicalNames = map[string]string{
	"fraction":    "%",
	"mL/(kg.min)": "mL/min·kg",
	"kcal/(kg.h)": "kcal/hr·kg",
}

// canonicalUnit reports whether unit is a canonical unit of the Units table of docs/architecture.md.
// A category type, whose samples are counted, has the unit count.
func canonicalUnit(unit string) bool {
	if _, ok := canonicalNames[unit]; ok {
		return true
	}

	return units[unit].canonical
}

// Convert converts value from the unit string from to the unit to, a canonical unit or a unit
// string. It returns an error naming from when the table does not list from, or to, or lists them
// for different dimensions.
func Convert(value float64, from, to string) (float64, error) {
	source, ok := units[from]

	target, found := units[to]
	if name, isName := canonicalNames[to]; isName {
		target, found = units[name], true
	}

	if !ok || !found || source.dimension != target.dimension {
		return 0, fmt.Errorf("unit %q is not a unit of %s", from, to)
	}

	if source.factor == target.factor && source.offset == target.offset {
		return value, nil
	}

	return (value+source.offset)*source.factor/target.factor - target.offset, nil
}
