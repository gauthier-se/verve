package estimate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

func ptr[T any](v T) *T { return &v }

func closeTo(t *testing.T, got, want, tol float64, label string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.2f, want %.2f (±%.2f)", label, got, want, tol)
	}
}

// --- Basal equations (pure) ---

// TestEquationsMatchPublishedValues pins each equation against a hand-computed figure,
// so a transcription slip in a coefficient cannot pass.
func TestEquationsMatchPublishedValues(t *testing.T) {
	// The reference Account: 91 kg, 184 cm, 66.4 kg lean, 30 years, male.
	in := Inputs{
		MassKg: ptr(91.0), HeightCm: ptr(184.0), LeanMassKg: ptr(66.4),
		AgeYears: ptr(30.0), Sex: SexMale,
	}
	want := map[string]float64{
		"katch_mcardle":   370 + 21.6*66.4,                           // 1804.24
		"cunningham":      500 + 22*66.4,                             // 1960.80
		"mifflin_st_jeor": 10*91 + 6.25*184 - 5*30 + 5,               // 1915.00
		"harris_benedict": 88.362 + 13.397*91 + 4.799*184 - 5.677*30, // 2018.28
	}
	for _, est := range Basal(in) {
		if est.Kcal == nil {
			t.Fatalf("%s uncomputable with full inputs; missing %v", est.Equation, est.Missing)
		}
		closeTo(t, *est.Kcal, want[est.Equation], 0.01, est.Equation)
	}
}

// TestFemaleConstantsDiffer guards the sex branch, whose two equations use entirely
// different coefficient sets rather than an offset.
func TestFemaleConstantsDiffer(t *testing.T) {
	base := Inputs{MassKg: ptr(65.0), HeightCm: ptr(168.0), AgeYears: ptr(35.0), LeanMassKg: ptr(48.0)}
	male, female := base, base
	male.Sex, female.Sex = SexMale, SexFemale

	byID := func(in Inputs) map[string]float64 {
		out := map[string]float64{}
		for _, e := range Basal(in) {
			if e.Kcal != nil {
				out[e.Equation] = *e.Kcal
			}
		}
		return out
	}
	m, f := byID(male), byID(female)

	closeTo(t, m["mifflin_st_jeor"], 10*65+6.25*168-5*35+5, 0.01, "mifflin male")
	closeTo(t, f["mifflin_st_jeor"], 10*65+6.25*168-5*35-161, 0.01, "mifflin female")
	closeTo(t, f["harris_benedict"], 447.593+9.247*65+3.098*168-4.330*35, 0.01, "harris female")

	// Katch-McArdle and Cunningham are sex-blind by construction — that is the whole
	// reason they stay usable for an Account with no biological sex on file.
	if m["katch_mcardle"] != f["katch_mcardle"] || m["cunningham"] != f["cunningham"] {
		t.Error("a lean-mass equation changed with sex; it must not")
	}
}

// TestNeedsDrivesComputability is what lets the UI grey out an equation and name the
// field that would unlock it, without the client knowing which equation wants what.
func TestNeedsDrivesComputability(t *testing.T) {
	// The reference Account's real state: lean mass known, date of birth and sex absent.
	in := Inputs{MassKg: ptr(91.0), HeightCm: ptr(184.0), LeanMassKg: ptr(66.4)}

	got := map[string]BasalEstimate{}
	for _, e := range Basal(in) {
		got[e.Equation] = e
	}

	if got["katch_mcardle"].Kcal == nil {
		t.Error("Katch-McArdle should be computable from lean mass alone")
	}
	mifflin := got["mifflin_st_jeor"]
	if mifflin.Kcal != nil {
		t.Error("Mifflin-St Jeor should be uncomputable without age and sex")
	}
	if len(mifflin.Missing) != 2 {
		t.Fatalf("missing = %v, want exactly age and sex", mifflin.Missing)
	}
	wantMissing := map[Input]bool{InputAge: true, InputSex: true}
	for _, m := range mifflin.Missing {
		if !wantMissing[m] {
			t.Errorf("unexpected missing input %q", m)
		}
	}
}

func TestUnknownSexIsNotFemale(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), HeightCm: ptr(184.0), AgeYears: ptr(30.0), Sex: ""}
	for _, e := range Basal(in) {
		if e.Equation == "mifflin_st_jeor" && e.Kcal != nil {
			t.Fatal("empty sex computed a value; unknown must never silently pick a default")
		}
	}
}

func TestAgeFromDateOfBirth(t *testing.T) {
	dob := time.Date(1996, 3, 15, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	age := Profile{DateOfBirth: &dob}.Age(now)
	if age == nil {
		t.Fatal("age is nil with a date of birth set")
	}
	closeTo(t, *age, 30.36, 0.05, "age")

	// Parenthesised: a bare composite literal in an if condition is a parse ambiguity.
	if (Profile{}).Age(now) != nil {
		t.Error("age is non-nil with no date of birth")
	}
}

// --- Regression ---

// TestSlopeIsRegressionNotEndpoints is the reason the observed basis fits a line: raw
// weight swings by more than a kilogram between mornings, so differencing the endpoints
// would let one unlucky final reading swamp four weeks of signal.
func TestSlopeIsRegressionNotEndpoints(t *testing.T) {
	origin := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)

	clean := make([]query.Point, 0, 28)
	for i := range 28 {
		clean = append(clean, query.Point{
			Bucket: origin.AddDate(0, 0, i).Format("2006-01-02"),
			Value:  92.75 - 0.0625*float64(i), // −1.75 kg over 28 days
		})
	}
	slope, ok := ordinaryLeastSquares(clean, origin)
	if !ok {
		t.Fatal("clean fit failed")
	}
	closeTo(t, slope, -0.0625, 1e-9, "clean slope")

	// One noisy final reading, the classic morning-hydration spike.
	noisy := append([]query.Point(nil), clean...)
	noisy[len(noisy)-1].Value += 1.5

	noisySlope, ok := ordinaryLeastSquares(noisy, origin)
	if !ok {
		t.Fatal("noisy fit failed")
	}
	// The regression barely moves…
	closeTo(t, noisySlope, -0.0625, 0.012, "noisy slope")

	// …whereas endpoint differencing swings by more than 80%, which is the failure mode
	// this test exists to forbid.
	endpoint := (noisy[len(noisy)-1].Value - noisy[0].Value) / 27
	if math.Abs(endpoint-(-0.0625)) < 0.03 {
		t.Fatalf("endpoint differencing = %.4f, expected it to be badly wrong here", endpoint)
	}
}

func TestSlopeNeedsTwoPoints(t *testing.T) {
	origin := time.Now()
	if _, ok := ordinaryLeastSquares([]query.Point{{Bucket: "2026-07-01", Value: 91}}, origin); ok {
		t.Error("a single point produced a slope")
	}
	if _, ok := ordinaryLeastSquares(nil, origin); ok {
		t.Error("no points produced a slope")
	}
}

// --- Cascade, against a real database ---

func setup(t *testing.T) (Engine, data.Models, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	acc := &data.Account{Email: "owner@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return Engine{Query: query.Engine{DB: db}}, models, acc.ID
}

// seedDaily writes one reading per day for `days` days ending the day before `now`,
// with value(i) evaluated at day i.
func seedDaily(t *testing.T, models data.Models, acc int64, metric, unit, source string, now time.Time, days int, value func(i int) float64) {
	t.Helper()
	rows := make([]data.Measurement, 0, days)
	for i := range days {
		at := now.AddDate(0, 0, -days+i).UTC().Format(time.RFC3339)
		rows = append(rows, data.Measurement{
			AccountID: acc, Metric: metric, Value: value(i), OriginalUnit: unit,
			StartAt: at, EndAt: at, Source: source,
			ContentKey: fmt.Sprintf("%s-%s-%d", metric, source, i),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed %s: %v", metric, err)
	}
}

// TestObservedBasisReproducesReferenceWindow is the test the whole cascade exists for.
// It reproduces the reference Account's real 28 days — mean intake 2078 kcal, body mass
// 92.75 → 91.00 kg — and asserts the back-computed figure, ≈2559 kcal.
func TestObservedBasisReproducesReferenceWindow(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 28, func(int) float64 { return 2078 })
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(i int) float64 {
		return 92.75 - 1.75*float64(i)/27
	})

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisObserved {
		t.Fatalf("basis = %q, want %q", exp.Basis, BasisObserved)
	}
	// 2078 + (1.75 kg × 7700 / 27 days) ≈ 2077.9 + 499.1
	closeTo(t, exp.Kcal, 2577, 15, "observed TDEE")
	if exp.MeanIntakeKcal == nil || exp.MassSlopeKgPerDay == nil {
		t.Fatal("observed basis did not carry its arithmetic")
	}
	if *exp.MassSlopeKgPerDay >= 0 {
		t.Errorf("slope = %v, want negative for a losing window", *exp.MassSlopeKgPerDay)
	}
}

// TestObservedBeatsRecordedWhenBothExist is ADR 0023's central claim, exercised: with
// devices claiming far more than the body actually spent, the cascade must take the body.
func TestObservedBeatsRecordedWhenBothExist(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 28, func(int) float64 { return 2078 })
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(i int) float64 {
		return 92.75 - 1.75*float64(i)/27
	})
	// The devices: 2280 basal + 1250 active = 3530/day, the reference Account's real claim.
	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 2280 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1250 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisObserved {
		t.Fatalf("basis = %q, want observed even though recorded data exists", exp.Basis)
	}
	if exp.Kcal > 3000 {
		t.Errorf("TDEE = %.0f; it followed the devices instead of the body", exp.Kcal)
	}
}

// TestFallsThroughToRecorded covers an Account with devices but no food log.
func TestFallsThroughToRecorded(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1600 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 600 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisRecorded {
		t.Fatalf("basis = %q, want recorded", exp.Basis)
	}
	closeTo(t, exp.Kcal, 2200, 1, "recorded TDEE")
}

// TestThinIntakeFallsThrough checks the coverage gate: a handful of logged days is not
// enough to back-compute a month of expenditure from.
func TestThinIntakeFallsThrough(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 5, func(int) float64 { return 2000 })
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(int) float64 { return 91 })
	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1600 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 600 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisRecorded {
		t.Fatalf("basis = %q, want recorded — 5 of 28 days is below the intake gate", exp.Basis)
	}
}

// TestFallsThroughToPredicted covers a fresh Account: no food log, no devices.
func TestFallsThroughToPredicted(t *testing.T) {
	e, _, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, ptr(1804.0), now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisPredicted {
		t.Fatalf("basis = %q, want predicted", exp.Basis)
	}
	closeTo(t, exp.Kcal, 1804*defaultActivityFactor, 0.01, "predicted TDEE")
	if exp.ActivityFactor == nil || exp.BasalKcal == nil {
		t.Error("predicted basis did not carry its arithmetic")
	}
}

// TestNoDataIsAnErrorNotAZero: an Account with nothing must get a refusal, never a
// number. A zero here would flow straight into a calorie target.
func TestNoDataIsAnErrorNotAZero(t *testing.T) {
	e, _, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	_, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("err = %v, want ErrInsufficientData", err)
	}
}

// --- Input resolution ---

// TestBodyFatIsAFractionNotAPercent is the 26-point trap. body_fat_percentage carries a
// "%" unit but stores 0.27, so dividing by 100 would yield a lean mass of 90.75 kg and a
// Katch-McArdle figure near 2330 kcal — wrong, and entirely plausible-looking.
func TestBodyFatIsAFractionNotAPercent(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 14, func(int) float64 { return 91 })
	seedDaily(t, models, acc, metricBodyFat, "%", "Zepp Life", now, 14, func(int) float64 { return 0.27 })

	in, err := e.ResolveInputs(context.Background(), acc, Profile{}, now)
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	if in.LeanMassKg == nil {
		t.Fatal("lean mass not derived from mass and body fat")
	}
	closeTo(t, *in.LeanMassKg, 66.43, 0.05, "derived lean mass")
	if !in.LeanMassDerived {
		t.Error("LeanMassDerived not flagged for a computed figure")
	}

	basal := Basal(in)[0] // katch_mcardle
	closeTo(t, *basal.Kcal, 370+21.6*66.43, 1.0, "Katch-McArdle")
}

// TestMeasuredLeanMassWinsOverDerived: the Metric is preferred when present, and the
// derived flag stays off so the UI does not claim a computation that did not happen.
func TestMeasuredLeanMassWinsOverDerived(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 14, func(int) float64 { return 91 })
	seedDaily(t, models, acc, metricBodyFat, "%", "Zepp Life", now, 14, func(int) float64 { return 0.40 })
	seedDaily(t, models, acc, metricLeanMass, "kg", "Zepp Life", now, 14, func(int) float64 { return 66.4 })

	in, err := e.ResolveInputs(context.Background(), acc, Profile{}, now)
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	closeTo(t, *in.LeanMassKg, 66.4, 0.01, "lean mass")
	if in.LeanMassDerived {
		t.Error("LeanMassDerived flagged although the Metric was measured")
	}
}

// TestStaleHeightIsStillFound is the reference Account's real situation: height was last
// recorded almost two years ago, and a rolling recent window would silently lose it —
// taking Mifflin-St Jeor and Harris-Benedict down with it.
func TestStaleHeightIsStillFound(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	at := time.Date(2024, 8, 28, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := models.Measurements.InsertBatch(context.Background(), []data.Measurement{{
		AccountID: acc, Metric: metricHeight, Value: 184, OriginalUnit: "cm",
		StartAt: at, EndAt: at, Source: "iPhone", ContentKey: "h1",
	}}); err != nil {
		t.Fatalf("seed height: %v", err)
	}

	in, err := e.ResolveInputs(context.Background(), acc, Profile{}, now)
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	if in.HeightCm == nil {
		t.Fatal("a two-year-old height was not found")
	}
	closeTo(t, *in.HeightCm, 184, 0.01, "height")
}

func TestMissingInputsStayNil(t *testing.T) {
	e, _, acc := setup(t)
	in, err := e.ResolveInputs(context.Background(), acc, Profile{}, time.Now())
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	if in.MassKg != nil || in.HeightCm != nil || in.LeanMassKg != nil {
		t.Errorf("empty Account resolved non-nil inputs: %+v", in)
	}
}

// --- Actual rate ---

func TestActualRateIsPercentOfBodyMassPerWeek(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(i int) float64 {
		return 92.75 - 1.75*float64(i)/27
	})

	rate, err := e.ActualRate(context.Background(), acc, now)
	if err != nil {
		t.Fatalf("ActualRate: %v", err)
	}
	if rate == nil {
		t.Fatal("no rate from 28 days of readings")
	}
	// −1.75 kg over 27 day-steps ≈ −0.4537 kg/week on a mean mass of ≈91.9 kg.
	closeTo(t, rate.KgPerWeek, -0.4537, 0.01, "kg/week")
	closeTo(t, rate.PctPerWeek, -0.494, 0.01, "%/week")
}

func TestActualRateNilWhenTooFewReadings(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 3, func(int) float64 { return 91 })

	rate, err := e.ActualRate(context.Background(), acc, now)
	if err != nil {
		t.Fatalf("ActualRate: %v", err)
	}
	if rate != nil {
		t.Errorf("rate = %+v, want nil below the minimum reading count", rate)
	}
}

// --- The shortfall: why the better basis went away ---

// seedDailyEnding writes one reading per day for `days` days ending `endedDaysAgo` days
// before `now`. seedDaily always ends yesterday, which cannot express the case this
// feature exists for: dense logging that stopped a while back.
func seedDailyEnding(t *testing.T, models data.Models, acc int64, metric, unit, source string,
	now time.Time, days, endedDaysAgo int, value func(i int) float64,
) {
	t.Helper()
	rows := make([]data.Measurement, 0, days)
	for i := range days {
		at := now.AddDate(0, 0, -endedDaysAgo-days+i).UTC().Format(time.RFC3339)
		rows = append(rows, data.Measurement{
			AccountID: acc, Metric: metric, Value: value(i), OriginalUnit: unit,
			StartAt: at, EndAt: at, Source: source,
			ContentKey: fmt.Sprintf("%s-%s-ended%d-%d", metric, source, endedDaysAgo, i),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed %s: %v", metric, err)
	}
}

// TestIntakeDaysNeededMatchesTheCoverageRule keeps the number on screen honest: it is
// written out as a literal because Go will not truncate an untyped float in a const,
// and this is what stops it drifting from the fraction that actually gates the cascade.
func TestIntakeDaysNeededMatchesTheCoverageRule(t *testing.T) {
	want := int(math.Ceil(minIntakeCoverage * observedWindowDays))
	if intakeDaysNeeded != want {
		t.Errorf("intakeDaysNeeded = %d, want ceil(%.2f x %d) = %d",
			intakeDaysNeeded, minIntakeCoverage, int(observedWindowDays), want)
	}
}

// TestShortfallNamesWhatWasMissing is an Account whose logging is too thin to qualify
// *anywhere* in the lookback: two days a week, never 20 in any 28. The figure falls to
// the devices; the shortfall says why, and the counts are the current window's, because
// that is the window that will qualify first if they start logging tonight.
func TestShortfallNamesWhatWasMissing(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	// Sparse logging across the whole lookback: every third day, so no 28-day placement
	// reaches 20 intake days.
	seedEveryNth(t, models, acc, metricIntake, "kcal", "Yazio", now, 150, 3, func(int) float64 { return 2263 })
	seedDailyEnding(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 44, 20, func(i int) float64 {
		return 92.15 - 4.85*float64(i)/43
	})
	// The devices kept recording throughout, which is exactly why the fallback exists
	// and exactly why it is misleading without a reason attached.
	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 2246 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1643 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisRecorded {
		t.Fatalf("basis = %q, want %q — the fixture no longer reproduces the degraded state", exp.Basis, BasisRecorded)
	}
	if exp.Shortfall == nil {
		t.Fatal("fell back to the devices and said nothing about why")
	}
	got := *exp.Shortfall
	if got.Basis != BasisObserved {
		t.Errorf("shortfall basis = %q, want %q", got.Basis, BasisObserved)
	}
	if got.IntakeDaysNeed != intakeDaysNeeded || got.MassDaysNeed != minMassDays {
		t.Errorf("thresholds = %d/%d, want %d/%d",
			got.IntakeDaysNeed, got.MassDaysNeed, intakeDaysNeeded, minMassDays)
	}
	if got.IntakeDays >= got.IntakeDaysNeed {
		t.Errorf("intake days %d meets the threshold %d, so this is not the case under test",
			got.IntakeDays, got.IntakeDaysNeed)
	}
	// The field that makes the note actionable. The log need not be *outside* the
	// window — the reference case has it 20 days back, well inside 28 — what matters is
	// that most of the window is silence, which a bare percentage does not convey and a
	// date does.
	if got.LastIntakeDay == "" {
		t.Fatal("no last intake day, so the note can only state a percentage")
	}
	last, err := time.Parse("2006-01-02", got.LastIntakeDay)
	if err != nil {
		t.Fatalf("last intake day %q is not a date: %v", got.LastIntakeDay, err)
	}
	if daysAgo := int(now.Sub(last).Hours() / 24); daysAgo > 4 {
		t.Errorf("last intake day is %d days ago; this fixture logs every third day", daysAgo)
	}
}

// seedEveryNth writes a reading every nth day over `days` days ending yesterday: thin
// logging, the kind that never fills a window however the window is placed.
func seedEveryNth(t *testing.T, models data.Models, acc int64, metric, unit, source string,
	now time.Time, days, n int, value func(i int) float64,
) {
	t.Helper()
	rows := make([]data.Measurement, 0, days/n+1)
	for i := 0; i < days; i += n {
		at := now.AddDate(0, 0, -days+i).UTC().Format(time.RFC3339)
		rows = append(rows, data.Measurement{
			AccountID: acc, Metric: metric, Value: value(i), OriginalUnit: unit,
			StartAt: at, EndAt: at, Source: source,
			ContentKey: fmt.Sprintf("%s-%s-nth%d-%d", metric, source, n, i),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed %s: %v", metric, err)
	}
}

// TestLastIntakeDayLooksPastTheWindow is the other half: an Account whose logging
// stopped *before* the window opened still gets a date, because the lookback is a year
// and not 28 days. Reading only the window would answer "" here and the note would lose
// the one sentence that tells the Account what to do.
func TestLastIntakeDayLooksPastTheWindow(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	// Logging that ended 90 days ago: nothing at all inside the observed window.
	seedDailyEnding(t, models, acc, metricIntake, "kcal", "Yazio", now, 30, 90, func(int) float64 { return 2263 })
	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 2246 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1643 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Shortfall == nil {
		t.Fatal("no shortfall")
	}
	if exp.Shortfall.IntakeDays != 0 {
		t.Errorf("intake days = %d, want 0 — nothing was logged inside the window", exp.Shortfall.IntakeDays)
	}
	if exp.Shortfall.LastIntakeDay == "" {
		t.Fatal("the lookback did not reach a log 90 days back")
	}
	last, _ := time.Parse("2006-01-02", exp.Shortfall.LastIntakeDay)
	if daysAgo := int(now.Sub(last).Hours() / 24); daysAgo < observedWindowDays {
		t.Errorf("last intake day is %d days ago; it should be the real one, ~90", daysAgo)
	}
}

// TestShortfallAbsentWhenObservedSucceeds is the guard that this cannot start appearing
// on healthy Accounts: a dense window produces the good figure and no explanation,
// because there is nothing to explain.
func TestShortfallAbsentWhenObservedSucceeds(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 28, func(int) float64 { return 2078 })
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(i int) float64 {
		return 92.75 - 1.75*float64(i)/27
	})

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisObserved {
		t.Fatalf("basis = %q, want %q", exp.Basis, BasisObserved)
	}
	if exp.Shortfall != nil {
		t.Errorf("a healthy Account was handed an explanation: %+v", *exp.Shortfall)
	}
}

// TestShortfallLastIntakeDayEmptyWhenNeverLogged: an Account that has never logged food
// gets "" rather than 0001-01-01, so the copy can offer the instruction instead of
// printing a date from the first century.
func TestShortfallLastIntakeDayEmptyWhenNeverLogged(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 2246 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1643 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Shortfall == nil {
		t.Fatal("no shortfall")
	}
	if exp.Shortfall.LastIntakeDay != "" {
		t.Errorf("last intake day = %q for an Account that never logged", exp.Shortfall.LastIntakeDay)
	}
	if exp.Shortfall.IntakeDays != 0 {
		t.Errorf("intake days = %d, want 0", exp.Shortfall.IntakeDays)
	}
}

// TestShortfallRidesThePredictedBasisToo: the explanation belongs to whichever figure
// was served, not only to the recorded one. An Account with no devices either lands on
// predicted, and has the same thing to fix.
func TestShortfallRidesThePredictedBasisToo(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	seedDailyEnding(t, models, acc, metricIntake, "kcal", "Yazio", now, 44, 20, func(int) float64 { return 2263 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, ptr(1900.0), now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisPredicted {
		t.Fatalf("basis = %q, want %q", exp.Basis, BasisPredicted)
	}
	if exp.Shortfall == nil {
		t.Error("the predicted basis carries no explanation of what it stands in for")
	}
}

// --- The window searches back (ADR 0023, extended) ---

// TestObservedPrefersTheMostRecentQualifyingWindow is the reference Account's real
// state on 16 September 2026: 44 dense days that ended on 27 August. The calendar
// window holds 8 of them and fails; a window ending at the last log holds all 28 it
// needs. The figure must come from the second, and must say so.
func TestObservedPrefersTheMostRecentQualifyingWindow(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	seedDailyEnding(t, models, acc, metricIntake, "kcal", "Yazio", now, 44, 20, func(int) float64 { return 2270 })
	seedDailyEnding(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 44, 20, func(i int) float64 {
		return 92.15 - 4.85*float64(i)/43
	})
	// The devices kept recording throughout, so `recorded` is available and would have
	// won before this change.
	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 2246 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1643 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisObserved {
		t.Fatalf("basis = %q, want %q — the search did not reach the dense stretch", exp.Basis, BasisObserved)
	}
	if exp.Shortfall != nil {
		t.Errorf("a figure was produced and an explanation came with it: %+v", *exp.Shortfall)
	}
	// It is dated, and the dates are the ones it used — not today's.
	if exp.WindowTo == "" || exp.WindowFrom == "" {
		t.Fatalf("window bounds absent: %q → %q", exp.WindowFrom, exp.WindowTo)
	}
	if exp.WindowTo >= day(now) {
		t.Errorf("window ends %q, which is today or later; the search did not move", exp.WindowTo)
	}
	to, err := time.Parse("2006-01-02", exp.WindowTo)
	if err != nil {
		t.Fatalf("window_to %q is not a date: %v", exp.WindowTo, err)
	}
	from, _ := time.Parse("2006-01-02", exp.WindowFrom)
	if d := int(to.Sub(from).Hours() / 24); d != observedWindowDays {
		t.Errorf("window spans %d days, want %d", d, int(observedWindowDays))
	}
	// And it used the full window, which is the whole point of moving it.
	if exp.IntakeDays < intakeDaysNeeded {
		t.Errorf("intake days = %d, below the threshold %d it was moved to satisfy",
			exp.IntakeDays, intakeDaysNeeded)
	}
}

// TestObservedUnchangedWhenTodayQualifies proves the common path did not move: an
// Account logging right now gets the window ending today, as it always did.
func TestObservedUnchangedWhenTodayQualifies(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 28, func(int) float64 { return 2078 })
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(i int) float64 {
		return 92.75 - 1.75*float64(i)/27
	})

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisObserved {
		t.Fatalf("basis = %q", exp.Basis)
	}
	if exp.WindowTo != day(now) {
		t.Errorf("window ends %q, want today (%q) — a dense Account's figure moved", exp.WindowTo, day(now))
	}
	// The same figure TestObservedBasisReproducesReferenceWindow asserts, on the same
	// fixture: 2078 + (1.75 kg x 7700 / 27 days). Pinned tightly here on purpose — the
	// point of this test is that the search left a dense Account's number alone, so a
	// loose tolerance would hide exactly what it is watching for.
	closeTo(t, exp.Kcal, 2577, 1, "observed TDEE")
}

// TestObservedGivesUpPastTheLookback: a qualifying window far enough back describes a
// body that has since changed, and admitting the gap beats dating a stale answer.
func TestObservedGivesUpPastTheLookback(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	// Dense logging that ended 150 days ago — well past the 90-day cap.
	seedDailyEnding(t, models, acc, metricIntake, "kcal", "Yazio", now, 44, 150, func(int) float64 { return 2270 })
	seedDailyEnding(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 44, 150, func(i int) float64 {
		return 92.15 - 4.85*float64(i)/43
	})
	seedDaily(t, models, acc, "basal_energy", "kcal", "Watch", now, 28, func(int) float64 { return 2246 })
	seedDaily(t, models, acc, "active_energy", "kcal", "Watch", now, 28, func(int) float64 { return 1643 })

	exp, err := e.Expenditure(context.Background(), acc, Inputs{}, nil, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	if exp.Basis != BasisRecorded {
		t.Errorf("basis = %q, want %q — the search reached past the lookback", exp.Basis, BasisRecorded)
	}
	if exp.Shortfall == nil {
		t.Error("gave up on the observed basis without saying why")
	}
}

// TestActualRateSearchesBackToo: the rate must not vanish at the moment the expenditure
// learns to find its window, or the Plan page's slider opens on nothing while the
// weigh-ins that would answer it sit a fortnight away.
func TestActualRateSearchesBackToo(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	seedDailyEnding(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 44, 20, func(i int) float64 {
		return 92.15 - 4.85*float64(i)/43
	})

	rate, err := e.ActualRate(context.Background(), acc, now)
	if err != nil {
		t.Fatalf("ActualRate: %v", err)
	}
	if rate == nil {
		t.Fatal("no rate, though 44 weigh-ins sit 20 days back")
	}
	if rate.WindowTo == "" || rate.WindowTo >= day(now) {
		t.Errorf("window ends %q; the rate did not move its window", rate.WindowTo)
	}
	if rate.KgPerWeek >= 0 {
		t.Errorf("rate = %.3f kg/week on a falling series", rate.KgPerWeek)
	}
}

// TestActualRateNeedsNoIntake is the reason the two windows are reported rather than
// forced equal: weighing without logging food still answers "how fast", and would not
// if the rate borrowed the expenditure's rule.
func TestActualRateNeedsNoIntake(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 28, func(i int) float64 {
		return 92.75 - 1.75*float64(i)/27
	})

	rate, err := e.ActualRate(context.Background(), acc, now)
	if err != nil {
		t.Fatalf("ActualRate: %v", err)
	}
	if rate == nil {
		t.Fatal("no rate for an Account that weighs but does not log food")
	}
	if rate.WindowTo != day(now) {
		t.Errorf("window ends %q, want today", rate.WindowTo)
	}
}

// TestLatestQualifyingWindowTriesTodayFirst pins the ordering directly: whatever the
// data, the first placement offered is the one ending now, so nothing about a
// currently-logging Account can depend on the search at all.
func TestLatestQualifyingWindowTriesTodayFirst(t *testing.T) {
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	var tried []time.Time
	latestQualifyingWindow(now, anchor, func(_, to time.Time) bool {
		tried = append(tried, to)
		return false
	})
	if len(tried) != 2 {
		t.Fatalf("tried %d placements, want 2 (now, then the anchor)", len(tried))
	}
	if !tried[0].Equal(now) {
		t.Errorf("first window tried ends %v, want %v", tried[0], now)
	}
	// The bound is exclusive, so a window anchored on 27 August ends on the 28th and
	// covers the 27th.
	if want := anchor.AddDate(0, 0, 1); !tried[1].Equal(want) {
		t.Errorf("second window ends %v, want %v", tried[1], want)
	}
}

// TestLatestQualifyingWindowFillsTheWindowItReports is the bug that running this against
// a real export exposed, written down. A day-by-day scan returns the most recent
// placement that merely *counts* enough readings, which for an Account whose weigh-ins
// stopped on 27 August was 8 August – 5 September: ten readings crammed into the first
// third, twenty empty days, and a rate labelled "over 28 days" describing ten.
func TestLatestQualifyingWindowFillsTheWindowItReports(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	// Weigh-ins every day for 44 days, ending 20 days ago.
	seedDailyEnding(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 44, 20, func(i int) float64 {
		return 92.15 - 4.85*float64(i)/43
	})

	rate, err := e.ActualRate(context.Background(), acc, now)
	if err != nil {
		t.Fatalf("ActualRate: %v", err)
	}
	if rate == nil {
		t.Fatal("no rate")
	}
	// The window must end where the readings do, so all 28 of its days carry one.
	if rate.MassDays != int(observedWindowDays) {
		t.Errorf("window %s → %s holds %d weigh-ins of %d days; it is not the window the "+
			"readings fill", rate.WindowFrom, rate.WindowTo, rate.MassDays, int(observedWindowDays))
	}
	// And the rate it reports is the one that stretch actually moved at, not the speed of
	// whichever ten days happened to fall inside a lopsided placement.
	closeTo(t, rate.KgPerWeek, -4.85*7/43, 0.05, "kg/week")
}
