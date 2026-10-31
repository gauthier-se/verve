package synthexport

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"
)

const (
	typeSleep                = "HKCategoryTypeIdentifierSleepAnalysis"
	typeHeartRateVariability = "HKQuantityTypeIdentifierHeartRateVariabilitySDNN"

	deviceWatch = "name:Apple Watch, manufacturer:Apple Inc., model:Watch, hardware:Watch7,3, software:26.0"

	deviceIPhone = "name:iPhone, manufacturer:Apple Inc., model:iPhone, hardware:iPhone14,5, software:26.0"

	deviceAirPodsPro = "name:Alex’s AirPods Pro, bluetoothIdentifier:5D8C0E9A-1B2F-4C3D-8E7F-0A1B2C3D4E5F, " +
		"manufacturer:Apple Inc., model:AirPods Pro, hardware:AirPods2,2, firmware:7A100"

	deviceAirPods = "name:AirPods, manufacturer:Apple Inc., model:0x2002, localIdentifier:02:00:5E:10:00:01-tacl"
	deviceHeadset = "name:Headset, localIdentifier:02:00:5E:10:00:02-tacl"
	deviceSpeaker = "name:Speaker, manufacturer:Acme, localIdentifier:02:00:5E:10:00:03-tacl"
)

// source is an app or a device that writes samples, with the devices it writes them from, if any.
type source struct {
	name, version string
	devices       []string
}

var (
	sourceWatch = source{name: "Apple Watch de Alex", version: "26.0", devices: []string{deviceWatch}}
	// sourceWatchDerived writes what the watch computes, which names no device.
	sourceWatchDerived = source{name: "Apple Watch de Alex", version: "26.0"}
	sourcePhone        = source{name: "iPhone de Alex", version: "26.0", devices: []string{deviceIPhone}}
	sourceOxygen       = source{name: "Oxygène sanguin", version: "26.0", devices: []string{deviceWatch}}
	sourceAudio        = source{
		name: "Bluetooth Device", devices: []string{deviceAirPodsPro, deviceAirPods, deviceHeadset, deviceSpeaker},
	}
	sourceClock = source{name: "Clock", devices: []string{deviceIPhone}}
	sourceApp   = source{name: "FitTrack", version: "612"}
	sourceScale = source{name: "Body Scale", version: "118"}
)

// metadata is a MetadataEntry element.
type metadata struct{ key, value string }

// beat is an InstantaneousBeatsPerMinute element.
type beat struct{ bpm, time string }

// record is a Record element.
type record struct {
	typ, unit, value string
	source           source
	device           string
	start, end       time.Time
	metadata         []metadata
	beats            []beat
}

func (e exporter) writeRecord(d document, r record) {
	attrs := []string{
		"type", r.typ,
		"sourceName", r.source.name,
		"sourceVersion", r.source.version,
		"device", r.device,
		"unit", r.unit,
		"creationDate", e.date(r.end),
		"startDate", e.date(r.start),
		"endDate", e.date(r.end),
		"value", r.value,
	}

	if len(r.metadata) == 0 && len(r.beats) == 0 {
		d.empty(1, "Record", attrs...)

		return
	}

	d.open(1, "Record", attrs...)

	for _, m := range r.metadata {
		d.empty(2, "MetadataEntry", "key", m.key, "value", m.value)
	}

	if len(r.beats) > 0 {
		d.open(2, "HeartRateVariabilityMetadataList")

		for _, b := range r.beats {
			d.empty(3, "InstantaneousBeatsPerMinute", "bpm", b.bpm, "time", b.time)
		}

		d.closeTag(2, "HeartRateVariabilityMetadataList")
	}

	d.closeTag(1, "Record")
}

// device describes a device the way HealthKit prints it. The address changes on every sample.
func (e exporter) device(fields string, r *rand.Rand) string {
	return fmt.Sprintf("<<HKDevice: 0x%x>, %s, creation date:%s>",
		0x100000000+uint64(r.Uint32()), fields, e.first.UTC().Format(dateLayout))
}

// deviceOf picks the device of a sample: a source with several devices changes from day to day.
func (e exporter) deviceOf(s source, day int, r *rand.Rand) string {
	if len(s.devices) == 0 {
		return ""
	}

	return e.device(s.devices[day%len(s.devices)], r)
}

// series is a quantity sampled through the day, or once a day when daily is set.
type series struct {
	typ, unit string
	source    source
	daily     bool
	offset    time.Duration // from midnight, or from the start of its hour
	length    time.Duration // of a sample, none for an instant
	value     func(r *rand.Rand, sample int) string
	metadata  func(r *rand.Rand) []metadata
}

// integer draws whole numbers.
func integer(low, high int) func(*rand.Rand, int) string {
	return func(r *rand.Rand, _ int) string { return strconv.Itoa(low + r.IntN(high-low)) }
}

// decimal draws numbers with a fixed number of decimals.
func decimal(low, high float64, decimals int) func(*rand.Rand, int) string {
	return func(r *rand.Rand, _ int) string {
		return strconv.FormatFloat(low+r.Float64()*(high-low), 'f', decimals, 64)
	}
}

// allSeries lists the series in the order of export.xml. The series of a type follow each other,
// since a type is one block.
func allSeries() []series {
	return []series{
		{
			typ: "HKQuantityTypeIdentifierHeartRate", unit: "count/min", source: sourceWatch, value: integer(50, 120),
			metadata: func(r *rand.Rand) []metadata {
				return []metadata{{"HKMetadataKeyHeartRateMotionContext", strconv.Itoa(r.IntN(3))}}
			},
		},
		{
			typ: "HKQuantityTypeIdentifierStepCount", unit: "count", source: sourceWatch,
			length: 10 * time.Minute, value: integer(1, 400),
		},
		{
			typ: "HKQuantityTypeIdentifierStepCount", unit: "count", source: sourcePhone,
			offset: 90 * time.Second, length: 10 * time.Minute, value: integer(1, 500),
		},
		{
			typ: "HKQuantityTypeIdentifierDistanceWalkingRunning", unit: "km", source: sourceWatch,
			length: 10 * time.Minute, value: distance,
		},
		{
			typ: "HKQuantityTypeIdentifierActiveEnergyBurned", unit: "kcal", source: sourceWatch,
			length: 10 * time.Minute, value: decimal(0.1, 20, 3),
		},
		{
			typ: "HKQuantityTypeIdentifierActiveEnergyBurned", unit: "kcal", source: sourceApp, daily: true,
			offset: 18 * time.Hour, length: 30 * time.Minute, value: decimal(50, 300, 1),
			metadata: func(*rand.Rand) []metadata {
				return []metadata{{"Type", "Steps"}, {"Custom Activity", "Séance libre"}}
			},
		},
		{
			typ: "HKQuantityTypeIdentifierPhysicalEffort", unit: "kcal/hr·kg", source: sourceWatchDerived,
			length: 10 * time.Minute, value: integer(1, 20),
			metadata: func(*rand.Rand) []metadata { return []metadata{{"HKAlgorithmVersion", "1"}} },
		},
		{
			typ: "HKQuantityTypeIdentifierOxygenSaturation", unit: "%", source: sourceOxygen, value: decimal(0.93, 1, 2),
			metadata: func(r *rand.Rand) []metadata {
				pressure := 98 + r.Float64()*6
				return []metadata{{"HKMetadataKeyBarometricPressure", strconv.FormatFloat(pressure, 'f', 3, 64) + " kPa"}}
			},
		},
		{
			typ: "HKQuantityTypeIdentifierHeadphoneAudioExposure", unit: "dBASPL", source: sourceAudio,
			length: 15 * time.Minute, value: audioExposure,
		},
		{
			typ: "HKQuantityTypeIdentifierRestingHeartRate", unit: "count/min", source: sourceWatchDerived,
			daily: true, length: 20 * time.Hour, value: integer(45, 65),
		},
		{
			typ: "HKQuantityTypeIdentifierVO2Max", unit: "mL/min·kg", source: sourceWatchDerived, daily: true,
			offset: 10 * time.Hour, value: decimal(40, 50, 2),
		},
		{
			typ: "HKQuantityTypeIdentifierBodyMass", unit: "kg", source: sourceScale, daily: true,
			offset: 7 * time.Hour, value: decimal(70, 74, 1),
		},
		{
			typ: "HKQuantityTypeIdentifierBodyFatPercentage", unit: "%", source: sourceScale, daily: true,
			offset: 7 * time.Hour, value: decimal(0.18, 0.22, 3),
		},
		{
			typ: "HKCategoryTypeIdentifierLowHeartRateEvent", source: sourceWatch, daily: true,
			offset: 3 * time.Hour, length: 10 * time.Minute,
			value: func(*rand.Rand, int) string { return "HKCategoryValueNotApplicable" },
			metadata: func(*rand.Rand) []metadata {
				return []metadata{{"HKHeartRateEventThreshold", "48 count/min"}}
			},
		},
	}
}

// distance is in kilometers, with the six significant digits of a real export, which turn to
// scientific notation for the tiny distances that open a day.
func distance(r *rand.Rand, sample int) string {
	km := 0.01 + r.Float64()*0.5
	if sample == 0 {
		km *= 1e-9
	}

	return strconv.FormatFloat(km, 'g', 6, 64)
}

// audioExposure opens each day with the silence of headphones that nothing plays through.
func audioExposure(r *rand.Rand, sample int) string {
	if sample == 0 {
		return "0"
	}

	return strconv.FormatFloat(40+r.Float64()*45, 'g', 6, 64)
}

// recordSections returns what writes each block of records, a day at a time.
func (e exporter) recordSections() []func(day int, d document) {
	var sections []func(int, document)

	for i, s := range allSeries() {
		sections = append(sections, func(day int, d document) { e.writeSeries(s, streamSeries+i, day, d) })
	}

	return append(sections, e.writeSleep, e.writeStandHours, e.writeSleepGoal)
}

func (e exporter) writeSeries(s series, stream, day int, d document) {
	r := e.rng(stream, day)
	samples, step := 24*e.opts.Density, time.Hour/time.Duration(e.opts.Density)

	if s.daily {
		samples = 1
	}

	for sample := range samples {
		start := e.dayStart(day).Add(s.offset + time.Duration(sample)*step)

		var meta []metadata
		if s.metadata != nil {
			meta = s.metadata(r)
		}

		e.writeRecord(d, record{
			typ: s.typ, unit: s.unit, value: s.value(r, sample), source: s.source, device: e.deviceOf(s.source, day, r),
			start: start, end: start.Add(s.length), metadata: meta,
		})
	}
}

// writeSleep writes the night that ends on a day, which starts on the evening before: the time in
// bed, then the stages. The clock of the phone, which has no version, adds the first night in bed.
func (e exporter) writeSleep(day int, d document) {
	r := e.rng(streamSleep, day)
	zone := []metadata{{"HKTimeZone", "Europe/Paris"}}
	bedtime := e.dayStart(day).Add(-time.Hour + time.Duration(r.IntN(60))*time.Minute)

	stages := []struct {
		value  string
		length time.Duration
	}{
		{"HKCategoryValueSleepAnalysisAsleepCore", 100 * time.Minute},
		{"HKCategoryValueSleepAnalysisAsleepDeep", 50 * time.Minute},
		{"HKCategoryValueSleepAnalysisAsleepREM", 30 * time.Minute},
		{"HKCategoryValueSleepAnalysisAwake", 5 * time.Minute},
		{"HKCategoryValueSleepAnalysisAsleepCore", 120 * time.Minute},
		{"HKCategoryValueSleepAnalysisAsleepREM", 40 * time.Minute},
		{"HKCategoryValueSleepAnalysisAsleepCore", 80 * time.Minute},
	}

	var total time.Duration
	for _, stage := range stages {
		total += stage.length
	}

	sleep := func(value string, s source, start, end time.Time) {
		e.writeRecord(d, record{
			typ: typeSleep, value: value, source: s, device: e.deviceOf(s, day, r),
			start: start, end: end, metadata: zone,
		})
	}

	sleep("HKCategoryValueSleepAnalysisInBed", sourceWatchDerived, bedtime, bedtime.Add(total))

	if day == 0 {
		sleep("HKCategoryValueSleepAnalysisInBed", sourceClock, bedtime, bedtime.Add(total))
	}

	start := bedtime
	for _, stage := range stages {
		sleep(stage.value, sourceWatchDerived, start, start.Add(stage.length))
		start = start.Add(stage.length)
	}

	// A nap on the phone, once a week, has no stages.
	const napDay = 3
	if day%7 == napDay {
		nap := e.dayStart(day).Add(14 * time.Hour)
		sleep("HKCategoryValueSleepAnalysisAsleepUnspecified", sourcePhone, nap, nap.Add(30*time.Minute))
	}
}

// writeStandHours writes one category per hour, whatever the density.
func (e exporter) writeStandHours(day int, d document) {
	r := e.rng(streamStandHours, day)

	for hour := range 24 {
		start := e.dayStart(day).Add(time.Duration(hour) * time.Hour)
		value := "HKCategoryValueAppleStandHourIdle"

		if r.IntN(3) > 0 {
			value = "HKCategoryValueAppleStandHourStood"
		}

		e.writeRecord(d, record{
			typ: "HKCategoryTypeIdentifierAppleStandHour", value: value, source: sourceWatch,
			device: e.deviceOf(sourceWatch, day, r), start: start, end: start.Add(time.Hour),
		})
	}
}

// writeSleepGoal writes the only record of a type outside the prefixes of the catalog.
func (e exporter) writeSleepGoal(day int, d document) {
	if day != 0 {
		return
	}

	at := e.dayStart(day).Add(9 * time.Hour)

	e.writeRecord(d, record{
		typ: "HKDataTypeSleepDurationGoal", unit: "hr", value: "8", source: sourcePhone, start: at, end: at,
	})
}

// writeHeartRateVariability writes the measure of a day with its beats, each one a beat after the
// previous one.
func (e exporter) writeHeartRateVariability(day int, d document) {
	r := e.rng(streamHeartRateVariability, day)
	end := e.dayStart(day).Add(6 * time.Hour)
	at := end.Add(-time.Minute)
	beats := make([]beat, 8*e.opts.Density)

	for i := range beats {
		bpm := 50 + r.IntN(30)
		beats[i] = beat{bpm: strconv.Itoa(bpm), time: at.Format("15:04:05,00")}
		at = at.Add(time.Minute / time.Duration(bpm))
	}

	e.writeRecord(d, record{
		typ: typeHeartRateVariability, unit: "ms", value: strconv.FormatFloat(20+r.Float64()*60, 'g', 6, 64),
		source: sourceWatch, device: e.deviceOf(sourceWatch, day, r),
		start: end.Add(-time.Minute), end: end,
		metadata: []metadata{{"HKAlgorithmVersion", "2"}}, beats: beats,
	})
}
