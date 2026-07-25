// Package estimate computes Verve's Estimates: quantities inferred about the Account
// rather than recorded by a Source (ADR 0023). A Metric answers "what was measured";
// an Estimate answers "what is true", which the measurement may approximate badly.
//
// Two Estimates live here. The **Basal estimate** is resting expenditure from a
// published equation. The **Expenditure estimate** is total daily expenditure — the
// figure a calorie target is built on — and it always carries the **Estimate basis**
// that produced it, because a number whose provenance is unknown cannot be trusted or
// argued with.
//
// Both are deliberately kept out of the Catalog. They are not observations, and putting
// them there would let them be graphed beside measured data as if they were.
//
// This package does arithmetic on Series; it never touches SQL. Every read goes through
// internal/query, so the figures here and the figures on a Panel come from one engine.
package estimate

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gauthier-se/verve/internal/query"
)

// energyPerKgMass is the assumed energy content of a kilogram of body-mass change.
// 7700 kcal is a fair approximation for fat loss and a poorer one for a bulk, where
// accrued tissue is mixed — a known, accepted inaccuracy (ADR 0023), not an oversight.
const energyPerKgMass = 7700.0

// observedWindowDays is the span the observed basis fits over. Long enough to average
// out day-to-day water swings, short enough to describe the Account's current life.
const observedWindowDays = 28

// defaultActivityFactor multiplies the Basal estimate for the predicted basis. 1.375 is
// the standard "lightly active" multiplier from the literature. It is deliberately *not*
// tuned to any one Account's observed ratio: the predicted basis exists precisely for
// Accounts with no history, so calibrating it against somebody who has one would be
// fitting a default to a sample of one.
const defaultActivityFactor = 1.375

// Coverage thresholds for the observed basis. Below either one the window cannot
// support a back-computation and the cascade falls through to the next basis.
const (
	minIntakeCoverage = 0.7 // fraction of window days that must carry logged intake
	minMassDays       = 10  // distinct days that must carry a body-mass reading
)

// profileLookbackYears is how far back a profile input may be read. Height is measured
// once and then never again — on the reference Account the last reading is nearly two
// years old — so a rolling window would silently lose it. A day bucket cannot span this
// (the engine caps a request at 1000 points), which is why these reads use months.
const profileLookbackYears = 30

// recentWindowDays is the span a body input is averaged over. A single scale reading
// swings by more than a kilogram day to day; the equations want the Account's mass, not
// this morning's hydration.
const recentWindowDays = 28

// Sex is the biological sex two of the four equations need. Empty means unknown — the
// honest state for an Account whose Apple `Me` record carried none.
type Sex string

const (
	SexMale   Sex = "male"
	SexFemale Sex = "female"
)

// Profile is the Account-level data an equation may need, none of it a Measurement:
// date of birth and biological sex live as columns on the Account, not in the Catalog.
type Profile struct {
	DateOfBirth *time.Time
	Sex         Sex
}

// Age returns the Account's age in years at now, or nil when the date of birth is unset.
func (p Profile) Age(now time.Time) *float64 {
	if p.DateOfBirth == nil {
		return nil
	}
	years := now.Sub(*p.DateOfBirth).Hours() / 24 / 365.25
	return &years
}

// Inputs is the resolved body data the equations are computed from: profile attributes
// plus the Metrics behind them, already smoothed and unit-normalized. A nil field is
// genuinely unknown, which is what makes an equation uncomputable rather than wrong.
type Inputs struct {
	LeanMassKg *float64
	MassKg     *float64
	HeightCm   *float64
	AgeYears   *float64
	Sex        Sex

	// LeanMassDerived records that LeanMassKg was computed as mass × (1 − body fat)
	// rather than read from the lean_body_mass Metric. On many scales the two are the
	// same number by construction, so this is a weaker signal than it looks; the flag
	// lets the UI say where the figure came from instead of implying a measurement.
	LeanMassDerived bool
}

// Input names one datum an equation needs, so the UI can grey out an equation and say
// which field would unlock it — without hardcoding on the client which equation wants what.
type Input string

const (
	InputLeanMass Input = "lean_mass"
	InputMass     Input = "mass"
	InputHeight   Input = "height"
	InputAge      Input = "age"
	InputSex      Input = "sex"
)

// have reports whether Inputs carries the named datum.
func (in Inputs) have(i Input) bool {
	switch i {
	case InputLeanMass:
		return in.LeanMassKg != nil
	case InputMass:
		return in.MassKg != nil
	case InputHeight:
		return in.HeightCm != nil
	case InputAge:
		return in.AgeYears != nil
	case InputSex:
		return in.Sex == SexMale || in.Sex == SexFemale
	}
	return false
}

// Equation is one published Basal equation, declared as data rather than as a branch in
// code: Needs drives both computability and the UI's explanation of what is missing.
type Equation struct {
	ID      string
	Name    string
	Needs   []Input
	compute func(Inputs) float64
}

// Equations are the four supported Basal equations, in the order the UI lists them:
// the two body-composition equations first, then the two anthropometric ones.
//
// The composition equations are the more accurate *when the composition input is real*.
// That qualifier is load-bearing and the caller must not assume it away: a bioimpedance
// scale can report a body fat that is a function of body weight and nothing else, in
// which case Katch-McArdle is an elaborately disguised weight equation. Which equation
// to prefer is therefore the Account's declared judgement, not this package's.
var Equations = []Equation{
	{
		ID:    "katch_mcardle",
		Name:  "Katch-McArdle",
		Needs: []Input{InputLeanMass},
		compute: func(in Inputs) float64 {
			return 370 + 21.6**in.LeanMassKg
		},
	},
	{
		ID:    "cunningham",
		Name:  "Cunningham",
		Needs: []Input{InputLeanMass},
		compute: func(in Inputs) float64 {
			return 500 + 22**in.LeanMassKg
		},
	},
	{
		ID:    "mifflin_st_jeor",
		Name:  "Mifflin-St Jeor",
		Needs: []Input{InputMass, InputHeight, InputAge, InputSex},
		compute: func(in Inputs) float64 {
			v := 10**in.MassKg + 6.25**in.HeightCm - 5**in.AgeYears
			if in.Sex == SexFemale {
				return v - 161
			}
			return v + 5
		},
	},
	{
		// Harris-Benedict as revised by Roza & Shizgal (1984), the form still in use.
		ID:    "harris_benedict",
		Name:  "Harris-Benedict",
		Needs: []Input{InputMass, InputHeight, InputAge, InputSex},
		compute: func(in Inputs) float64 {
			if in.Sex == SexFemale {
				return 447.593 + 9.247**in.MassKg + 3.098**in.HeightCm - 4.330**in.AgeYears
			}
			return 88.362 + 13.397**in.MassKg + 4.799**in.HeightCm - 5.677**in.AgeYears
		},
	},
}

// BasalEstimate is one equation's result. Kcal is nil when Missing is non-empty: an
// equation short of an input yields no number at all, never a number computed from a
// guessed substitute.
type BasalEstimate struct {
	Equation string   `json:"equation"`
	Name     string   `json:"name"`
	Kcal     *float64 `json:"kcal,omitempty"`
	Missing  []Input  `json:"missing,omitempty"`
}

// Basal evaluates every equation against the resolved Inputs, in Equations order. It is
// pure: no I/O, no ordering by preference. Which one to preselect depends on the
// Account's declared trust in its body-composition data and is decided by the caller.
func Basal(in Inputs) []BasalEstimate {
	out := make([]BasalEstimate, 0, len(Equations))
	for _, eq := range Equations {
		est := BasalEstimate{Equation: eq.ID, Name: eq.Name}
		for _, need := range eq.Needs {
			if !in.have(need) {
				est.Missing = append(est.Missing, need)
			}
		}
		if len(est.Missing) == 0 {
			v := eq.compute(in)
			est.Kcal = &v
		}
		out = append(out, est)
	}
	return out
}

// Basis names the evidence behind an Expenditure estimate, best first. It is part of the
// answer, not an implementation detail: an Account told 3530 kcal deserves to know that
// figure came from its devices rather than from what its body actually did.
type Basis string

const (
	// BasisObserved back-computes expenditure from logged intake against the body-mass
	// trend — what the body actually did, and the only basis that needs no equation and
	// no body-composition input.
	BasisObserved Basis = "observed"
	// BasisRecorded is the mean of total_energy_expenditure — what the devices claim.
	BasisRecorded Basis = "recorded"
	// BasisPredicted is a Basal estimate times an activity factor — what an equation guesses.
	BasisPredicted Basis = "predicted"
)

// Expenditure is the Expenditure estimate with the evidence behind it. The detail fields
// are populated only for the basis that produced the figure, so a caller can show the
// arithmetic rather than asking the Account to trust a bare number.
type Expenditure struct {
	Kcal  float64 `json:"kcal"`
	Basis Basis   `json:"basis"`

	WindowDays int `json:"window_days"`

	// Observed basis.
	MeanIntakeKcal    *float64 `json:"mean_intake_kcal,omitempty"`
	MassSlopeKgPerDay *float64 `json:"mass_slope_kg_per_day,omitempty"`
	IntakeDays        int      `json:"intake_days,omitempty"`
	MassDays          int      `json:"mass_days,omitempty"`

	// Predicted basis.
	ActivityFactor *float64 `json:"activity_factor,omitempty"`
	BasalKcal      *float64 `json:"basal_kcal,omitempty"`
}

// Rate is the Account's measured speed of body-mass change: the regression slope over
// the window, expressed the way a Target rate is (percent of body mass per week) so the
// two are directly comparable.
type Rate struct {
	PctPerWeek float64 `json:"pct_per_week"`
	KgPerWeek  float64 `json:"kg_per_week"`
	WindowDays int     `json:"window_days"`
	MassDays   int     `json:"mass_days"`
}

// Engine computes Estimates over the query engine. It holds no state and owns no SQL.
type Engine struct {
	Query query.Engine
}

// Metric slugs this package reads. Named rather than inlined so a Catalog rename is a
// compile-time concern in one place.
const (
	metricBodyMass  = "body_mass"
	metricLeanMass  = "lean_body_mass"
	metricBodyFat   = "body_fat_percentage"
	metricHeight    = "height"
	metricIntake    = "dietary_energy"
	metricTotalBurn = "total_energy_expenditure"
)

// ResolveInputs reads the body data the equations need. Mass, lean mass and body fat are
// **averaged over the recent window** rather than taken as a single reading, because a
// scale swings by more than a kilogram between mornings and the equations want the
// Account's body, not its hydration. Height is read over a multi-year window, since it is
// measured once and then never again.
func (e Engine) ResolveInputs(ctx context.Context, accountID int64, profile Profile, now time.Time) (Inputs, error) {
	in := Inputs{Sex: profile.Sex, AgeYears: profile.Age(now)}

	mass, err := e.recentMean(ctx, accountID, metricBodyMass, now)
	if err != nil {
		return Inputs{}, err
	}
	in.MassKg = mass

	height, err := e.everLatest(ctx, accountID, metricHeight, now)
	if err != nil {
		return Inputs{}, err
	}
	in.HeightCm = height

	// Lean mass: the measured Metric first, else mass × (1 − body fat).
	lean, err := e.recentMean(ctx, accountID, metricLeanMass, now)
	if err != nil {
		return Inputs{}, err
	}
	if lean != nil {
		in.LeanMassKg = lean
	} else if fat, err := e.recentMean(ctx, accountID, metricBodyFat, now); err != nil {
		return Inputs{}, err
	} else if fat != nil && mass != nil {
		// body_fat_percentage is stored as a FRACTION despite its "%" unit: 27% body fat
		// is 0.27, and oxygen_saturation is 0.969. Dividing by 100 here would be a
		// 26-point error that still produces a plausible-looking figure.
		v := *mass * (1 - *fat)
		in.LeanMassKg = &v
		in.LeanMassDerived = true
	}

	return in, nil
}

// Expenditure resolves the best-supported Expenditure estimate, trying each basis in
// order and falling through when the evidence is too thin. It never returns a zero as if
// it were an answer: an Account with nothing to go on gets an error the caller reports.
func (e Engine) Expenditure(ctx context.Context, accountID int64, in Inputs, basal *float64, now time.Time) (Expenditure, error) {
	if obs, ok, err := e.observed(ctx, accountID, now); err != nil {
		return Expenditure{}, err
	} else if ok {
		return obs, nil
	}

	if rec, ok, err := e.recorded(ctx, accountID, now); err != nil {
		return Expenditure{}, err
	} else if ok {
		return rec, nil
	}

	if basal != nil {
		factor := defaultActivityFactor
		return Expenditure{
			Kcal:           *basal * factor,
			Basis:          BasisPredicted,
			WindowDays:     observedWindowDays,
			ActivityFactor: &factor,
			BasalKcal:      basal,
		}, nil
	}

	return Expenditure{}, ErrInsufficientData
}

// observed back-computes expenditure from what the body actually did:
//
//	TDEE = mean daily intake − (mass slope in kg/day × 7700)
//
// The slope comes from an ordinary least-squares fit over daily readings, never from
// differencing the endpoints: raw weight swings by ±1.5 kg between mornings, so a single
// unlucky final reading would swamp four weeks of signal.
func (e Engine) observed(ctx context.Context, accountID int64, now time.Time) (Expenditure, bool, error) {
	from := now.AddDate(0, 0, -observedWindowDays)

	intake, err := e.dailyPoints(ctx, accountID, metricIntake, from, now)
	if err != nil {
		return Expenditure{}, false, err
	}
	mass, err := e.dailyPoints(ctx, accountID, metricBodyMass, from, now)
	if err != nil {
		return Expenditure{}, false, err
	}

	if float64(len(intake))/observedWindowDays < minIntakeCoverage || len(mass) < minMassDays {
		return Expenditure{}, false, nil
	}

	meanIntake := mean(values(intake))
	slope, ok := ordinaryLeastSquares(mass, from)
	if !ok {
		return Expenditure{}, false, nil
	}

	return Expenditure{
		Kcal:              meanIntake - slope*energyPerKgMass,
		Basis:             BasisObserved,
		WindowDays:        observedWindowDays,
		MeanIntakeKcal:    &meanIntake,
		MassSlopeKgPerDay: &slope,
		IntakeDays:        len(intake),
		MassDays:          len(mass),
	}, true, nil
}

// recorded is the mean of the days the devices reported — total_energy_expenditure, the
// derived Metric (active + basal). It averages over days *with* data rather than over the
// whole window, so a gap does not read as a zero-burn day.
func (e Engine) recorded(ctx context.Context, accountID int64, now time.Time) (Expenditure, bool, error) {
	from := now.AddDate(0, 0, -observedWindowDays)
	points, err := e.dailyPoints(ctx, accountID, metricTotalBurn, from, now)
	if err != nil {
		return Expenditure{}, false, err
	}
	if len(points) == 0 {
		return Expenditure{}, false, nil
	}
	return Expenditure{
		Kcal:       mean(values(points)),
		Basis:      BasisRecorded,
		WindowDays: observedWindowDays,
	}, true, nil
}

// ActualRate is the Account's measured speed of body-mass change over the observed
// window. The Plan page opens its Target rate slider on this, so the page starts by
// stating what the Account is already doing rather than presenting an empty form.
func (e Engine) ActualRate(ctx context.Context, accountID int64, now time.Time) (*Rate, error) {
	from := now.AddDate(0, 0, -observedWindowDays)
	points, err := e.dailyPoints(ctx, accountID, metricBodyMass, from, now)
	if err != nil {
		return nil, err
	}
	if len(points) < minMassDays {
		return nil, nil
	}
	slope, ok := ordinaryLeastSquares(points, from)
	if !ok {
		return nil, nil
	}
	avgMass := mean(values(points))
	if avgMass == 0 {
		return nil, nil
	}
	kgPerWeek := slope * 7
	return &Rate{
		PctPerWeek: kgPerWeek / avgMass * 100,
		KgPerWeek:  kgPerWeek,
		WindowDays: observedWindowDays,
		MassDays:   len(points),
	}, nil
}

// dailyPoints reads one Metric as daily buckets over [from, now). An empty window is an
// empty slice, not an error — thin data is a normal state here, and the cascade's job is
// to notice it.
func (e Engine) dailyPoints(ctx context.Context, accountID int64, metric string, from, to time.Time) ([]query.Point, error) {
	s, err := e.Query.Series(ctx, query.Request{
		AccountID: accountID, Metric: metric, From: from, To: to, Bucket: query.Day,
	})
	if err != nil {
		return nil, fmt.Errorf("estimate: series %s: %w", metric, err)
	}
	return s.Points, nil
}

// recentMean averages a body Metric over the recent window. For a `latest` Metric the
// engine already carries that mean on the Series (it is what a period-average trend
// shows), so this reuses the server's figure rather than re-folding buckets client-side.
func (e Engine) recentMean(ctx context.Context, accountID int64, metric string, now time.Time) (*float64, error) {
	s, err := e.Query.Series(ctx, query.Request{
		AccountID: accountID, Metric: metric,
		From: now.AddDate(0, 0, -recentWindowDays), To: now, Bucket: query.Day,
	})
	if err != nil {
		return nil, fmt.Errorf("estimate: series %s: %w", metric, err)
	}
	if s.Mean != nil {
		return s.Mean, nil
	}
	if s.Summary != nil {
		v := s.Summary.Value
		return &v, nil
	}
	// Nothing recent: fall back to the most recent reading of any age. Better a stale
	// body mass than an equation that cannot run at all.
	return e.everLatest(ctx, accountID, metric, now)
}

// everLatest reads a Metric's most recent value however old it is, over a multi-year
// window in month buckets. Height needs this: it is measured once and then never again,
// and the engine caps a request at 1000 points so a day bucket cannot span the years.
func (e Engine) everLatest(ctx context.Context, accountID int64, metric string, now time.Time) (*float64, error) {
	s, err := e.Query.Series(ctx, query.Request{
		AccountID: accountID, Metric: metric,
		From: now.AddDate(-profileLookbackYears, 0, 0), To: now, Bucket: query.Month,
	})
	if err != nil {
		return nil, fmt.Errorf("estimate: series %s: %w", metric, err)
	}
	if s.Summary == nil {
		return nil, nil
	}
	v := s.Summary.Value
	return &v, nil
}

// ordinaryLeastSquares fits value ≈ a + b·day over the points and returns b, the slope in
// units per day. The x axis is days elapsed from origin, so a gap in the series is a
// genuinely missing x rather than a compressed one. Fewer than two distinct x values, or
// a degenerate fit, yields ok=false.
func ordinaryLeastSquares(points []query.Point, origin time.Time) (float64, bool) {
	if len(points) < 2 {
		return 0, false
	}
	var n, sumX, sumY, sumXY, sumXX float64
	for _, p := range points {
		day, err := time.Parse("2006-01-02", p.Bucket)
		if err != nil {
			continue
		}
		x := day.Sub(origin).Hours() / 24
		n++
		sumX += x
		sumY += p.Value
		sumXY += x * p.Value
		sumXX += x * x
	}
	if n < 2 {
		return 0, false
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 || math.IsNaN(denom) {
		return 0, false
	}
	return (n*sumXY - sumX*sumY) / denom, true
}

func values(points []query.Point) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = p.Value
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
