// Package synthexport generates invented Apple Health exports for the tests of the import. An export
// has the shape of a real one and none of its data: every source, value, name and coordinate is made
// up. Nothing is kept in memory, so an export can be as large as a test needs.
package synthexport

import (
	"archive/zip"
	"bufio"
	"context"
	_ "embed" // for the prolog
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strconv"
	"time"
)

const (
	folder     = "apple_health_export/"
	dateLayout = "2006-01-02 15:04:05 -0700"

	sourceWatch = "Apple Watch de Alex"
	versionOS   = "26.0"

	typeHeartRate            = "HKQuantityTypeIdentifierHeartRate"
	typeHeartRateVariability = "HKQuantityTypeIdentifierHeartRateVariabilitySDNN"
	unitBeatsPerMinute       = "count/min"
)

// A stream of random numbers exists per day, so that anything can be derived again from its day
// alone, such as the workout of a route.
const (
	streamHeartRate = iota
	streamHeartRateVariability
	streamWorkout
	streamActivitySummary
)

// prolog is what comes before the root element of a real export. It holds the declarations of the
// format and no data.
//
//go:embed prolog.txt
var prolog string

// Options selects an export. The same options always give the same bytes.
type Options struct {
	// Seed selects the values. The structure of the export does not depend on it.
	Seed uint64
	// End is the moment of the export. Every date carries its offset, even in another season: a
	// real export writes all its dates in the offset of the day of the export.
	End time.Time
	// Days is the length of the history, whose last day is the one before End.
	Days int
	// Density is the number of samples per hour of the continuous series.
	Density int
}

// Write streams an export.zip to w.
func Write(ctx context.Context, w io.Writer, opts Options) error {
	if opts.Days < 1 {
		return errors.New("synthetic export: days must be at least 1")
	}

	if opts.Density < 1 {
		return errors.New("synthetic export: density must be at least 1")
	}

	zw := zip.NewWriter(w)

	if err := newExporter(opts).writeEntries(ctx, zw); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}

	return nil
}

// exporter holds no state besides what the options determine.
type exporter struct {
	opts  Options
	zone  *time.Location // the offset of every date
	first time.Time      // midnight of the first day
}

func newExporter(opts Options) exporter {
	_, offset := opts.End.Zone()
	zone := time.FixedZone("", offset)
	end := opts.End.In(zone)
	midnight := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, zone)

	return exporter{opts: opts, zone: zone, first: midnight.AddDate(0, 0, -opts.Days)}
}

// entry is the content of a zip entry.
type entry func(ctx context.Context, d document) error

func (e exporter) writeEntries(ctx context.Context, zw *zip.Writer) error {
	if err := writeEntry(ctx, zw, "export.xml", e.writeExportXML); err != nil {
		return err
	}

	for day := range e.opts.Days {
		run, ok := e.runOn(day)
		if !ok {
			continue
		}

		if err := writeEntry(ctx, zw, "workout-routes/"+run.routeFile(), writeRoute); err != nil {
			return err
		}
	}

	if err := e.writeElectrocardiograms(ctx, zw); err != nil {
		return err
	}

	return writeEntry(ctx, zw, "export_cda.xml", writeClinicalDocument)
}

func writeEntry(ctx context.Context, zw *zip.Writer, name string, write entry) error {
	f, err := zw.Create(folder + name)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}

	d := document{w: bufio.NewWriter(f)}

	if err := write(ctx, d); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	if err := d.flush(ctx); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}

func (e exporter) writeExportXML(ctx context.Context, d document) error {
	d.write(prolog, `<HealthData locale="en_FR">`, "\n")
	d.empty(1, "ExportDate", "value", e.date(e.opts.End))
	d.empty(1, "Me",
		"HKCharacteristicTypeIdentifierDateOfBirth", "1990-06-15",
		"HKCharacteristicTypeIdentifierBiologicalSex", "HKBiologicalSexFemale",
		"HKCharacteristicTypeIdentifierBloodType", "HKBloodTypeNotSet",
		"HKCharacteristicTypeIdentifierFitzpatrickSkinType", "HKFitzpatrickSkinTypeNotSet",
		"HKCharacteristicTypeIdentifierCardioFitnessMedicationsUse", "None")

	sections := []func(day int, d document){
		e.writeRecords, e.writeCorrelations, e.writeWorkouts, e.writeActivitySummary, e.writeHeartRateVariability,
	}

	for _, section := range sections {
		for day := range e.opts.Days {
			section(day, d)

			if err := d.flush(ctx); err != nil {
				return err
			}
		}
	}

	d.closeTag(0, "HealthData")

	return nil
}

// record is a Record element.
type record struct {
	typ, source, version, unit, value string
	start, end                        time.Time
}

func (e exporter) writeRecord(d document, r record) {
	d.empty(1, "Record",
		"type", r.typ,
		"sourceName", r.source,
		"sourceVersion", r.version,
		"unit", r.unit,
		"creationDate", e.date(r.end),
		"startDate", e.date(r.start),
		"endDate", e.date(r.end),
		"value", r.value)
}

func (e exporter) writeRecords(day int, d document) {
	r := e.rng(streamHeartRate, day)

	for sample := range 24 * e.opts.Density {
		at := e.dayStart(day).Add(time.Duration(sample) * time.Hour / time.Duration(e.opts.Density))

		e.writeRecord(d, record{
			typ: typeHeartRate, source: sourceWatch, version: versionOS, unit: unitBeatsPerMinute,
			value: strconv.Itoa(50 + r.IntN(40)), start: at, end: at,
		})
	}
}

func (e exporter) writeCorrelations(day int, d document) {
	at := e.dayStart(day).Add(12 * time.Hour)

	d.empty(1, "Correlation",
		"type", "HKCorrelationTypeIdentifierFood",
		"sourceName", "Nutrio",
		"sourceVersion", "1200",
		"creationDate", e.date(at),
		"startDate", e.date(at),
		"endDate", e.date(at))
}

func (e exporter) writeWorkouts(day int, d document) {
	run, ok := e.runOn(day)
	if !ok {
		return
	}

	d.empty(1, "Workout",
		"workoutActivityType", "HKWorkoutActivityTypeRunning",
		"duration", strconv.FormatFloat(run.end.Sub(run.start).Minutes(), 'f', -1, 64),
		"durationUnit", "min",
		"sourceName", sourceWatch,
		"sourceVersion", versionOS,
		"creationDate", e.date(run.end),
		"startDate", e.date(run.start),
		"endDate", e.date(run.end))
}

func (e exporter) writeActivitySummary(day int, d document) {
	r := e.rng(streamActivitySummary, day)

	d.empty(1, "ActivitySummary",
		"dateComponents", e.dayStart(day).Format(time.DateOnly),
		"activeEnergyBurned", strconv.FormatFloat(250+r.Float64()*400, 'f', 2, 64),
		"activeEnergyBurnedGoal", "500",
		"activeEnergyBurnedUnit", "kcal",
		"appleMoveTime", "0",
		"appleMoveTimeGoal", "0",
		"appleExerciseTime", strconv.Itoa(10+r.IntN(50)),
		"appleExerciseTimeGoal", "30",
		"appleStandHours", strconv.Itoa(6+r.IntN(9)),
		"appleStandHoursGoal", "12")
}

func (e exporter) writeHeartRateVariability(day int, d document) {
	r := e.rng(streamHeartRateVariability, day)
	end := e.dayStart(day).Add(6 * time.Hour)

	e.writeRecord(d, record{
		typ: typeHeartRateVariability, source: sourceWatch, version: versionOS, unit: "ms",
		value: strconv.FormatFloat(20+r.Float64()*60, 'f', 4, 64), start: end.Add(-time.Minute), end: end,
	})
}

// run is a running workout.
type run struct {
	start, end time.Time
}

// routeFile names the route of the run, in the offset of the export.
func (w run) routeFile() string {
	return "route_" + w.start.Format("2006-01-02_3.04pm") + ".gpx"
}

// runOn returns the run of a day, if it has one.
func (e exporter) runOn(day int) (run, bool) {
	const everyDays = 7
	if day%everyDays != 0 {
		return run{}, false
	}

	r := e.rng(streamWorkout, day)
	start := e.dayStart(day).Add(7*time.Hour + time.Duration(r.IntN(120))*time.Minute)

	return run{start: start, end: start.Add(25*time.Minute + time.Duration(r.IntN(30))*time.Minute)}, true
}

func writeRoute(_ context.Context, d document) error {
	d.write(`<?xml version="1.0" encoding="UTF-8"?>`, "\n", `<gpx version="1.1" creator="Apple Health Export"/>`, "\n")

	return nil
}

// writeElectrocardiograms writes two recordings of the first day: the second one takes the suffix a
// real export gives to a name that is already used.
func (e exporter) writeElectrocardiograms(ctx context.Context, zw *zip.Writer) error {
	date := e.dayStart(0).Format(time.DateOnly)

	for i, name := range []string{"ecg_" + date + ".csv", "ecg_" + date + "_1.csv"} {
		recorded := e.dayStart(0).Add(time.Duration(10+i) * time.Hour)

		err := writeEntry(ctx, zw, "electrocardiograms/"+name, func(_ context.Context, d document) error {
			writeElectrocardiogram(d, e.date(recorded))

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func writeElectrocardiogram(d document, recorded string) {
	d.write("Name,Alex Martin\n", "Date of Birth,1990-06-15\n", "Recorded Date,", recorded, "\n",
		"Classification,Sinus Rhythm\n", "Symptoms,\n", "Software Version,2.0\n", "Device,\"Watch7,3\"\n",
		"Sample Rate,512 hertz\n", "\n", "\n", "Lead,Lead I\n", "Unit,µV\n", "\n")

	for i := range 32 {
		d.write(strconv.Itoa(i*i%97-48), "\n")
	}
}

// writeClinicalDocument writes the head of the clinical document, which the import never reads.
func writeClinicalDocument(_ context.Context, d document) error {
	d.write(`<?xml version="1.0" encoding="UTF-8"?>`, "\n",
		`<ClinicalDocument xmlns="urn:hl7-org:v3">`, "\n",
		` <realmCode code="US"/>`, "\n",
		` <title>Health Data Export</title>`, "\n",
		`</ClinicalDocument>`, "\n")

	return nil
}

// dayStart returns the midnight of a day of the history.
func (e exporter) dayStart(day int) time.Time {
	return e.first.AddDate(0, 0, day)
}

func (e exporter) date(t time.Time) string {
	return t.In(e.zone).Format(dateLayout)
}

// rng returns the random numbers of a stream and a day.
func (e exporter) rng(stream, day int) *rand.Rand {
	return rand.New(rand.NewPCG(e.opts.Seed, uint64(stream)<<32|uint64(day))) //nolint:gosec // G404, G115: reproducible test data, and day is never negative
}
