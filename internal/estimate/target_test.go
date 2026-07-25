package estimate

import (
	"context"
	"testing"
	"time"
)

// TestTargetsFromRateAgainstReferenceAccount pins the arithmetic on the reference
// Account's real figures: 2390 kcal expenditure, 91.6 kg, 66.8 kg lean.
func TestTargetsFromRateAgainstReferenceAccount(t *testing.T) {
	in := Inputs{MassKg: ptr(91.6), LeanMassKg: ptr(66.8)}

	got, ok := DeriveTargets(2390, -0.5, in)
	if !ok {
		t.Fatal("targets not derivable with mass and lean mass known")
	}
	// −0.5 %/week of 91.6 kg = −0.458 kg/week = 503 kcal/day of deficit.
	closeTo(t, got.Kcal, 2390-0.005*91.6*energyPerKgMass/7, 0.5, "target kcal")
	closeTo(t, got.Kcal, 1886.3, 1.0, "target kcal (absolute)")

	// A −0.5 %/week cut is halfway to the deep-cut anchor: 1.6 + 0.5·(2.4−1.6) = 2.0.
	closeTo(t, got.ProteinGPerKgLean, 2.0, 0.001, "protein g/kg")
	closeTo(t, got.ProteinG, 2.0*66.8, 0.01, "protein g")
	if got.ProteinFromBodyMass {
		t.Error("protein scaled on body mass although lean mass was known")
	}
	if !got.ConventionalSplit {
		t.Error("the fat/carb split must be flagged as a convention, not a recommendation")
	}
}

// TestBulkAndMaintenanceTakeTheMaintenanceFloor: a surplus does not threaten lean mass,
// so the protein floor does not rise for one.
func TestBulkAndMaintenanceTakeTheMaintenanceFloor(t *testing.T) {
	for _, rate := range []float64{0, 0.25, 0.5} {
		if got := proteinPerKg(rate); got != proteinGPerKgAtMaintenance {
			t.Errorf("proteinPerKg(%v) = %v, want %v", rate, got, proteinGPerKgAtMaintenance)
		}
	}
}

func TestProteinFloorRisesWithTheDeficit(t *testing.T) {
	closeTo(t, proteinPerKg(-0.25), 1.8, 0.001, "shallow cut")
	closeTo(t, proteinPerKg(-1.0), 2.4, 0.001, "deep cut")
	// Beyond the deep-cut anchor the floor is capped, not extrapolated.
	closeTo(t, proteinPerKg(-2.5), 2.4, 0.001, "beyond the anchor")
}

// TestSurplusRaisesTheTarget checks the sign convention end to end: a positive rate is
// a bulk and must *add* calories.
func TestSurplusRaisesTheTarget(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)}
	cut, _ := DeriveTargets(2500, -0.5, in)
	maintain, _ := DeriveTargets(2500, 0, in)
	bulk, _ := DeriveTargets(2500, 0.25, in)

	if !(cut.Kcal < maintain.Kcal && maintain.Kcal < bulk.Kcal) {
		t.Fatalf("targets not ordered cut < maintain < bulk: %.0f / %.0f / %.0f",
			cut.Kcal, maintain.Kcal, bulk.Kcal)
	}
	closeTo(t, maintain.Kcal, 2500, 0.01, "maintenance equals expenditure")
}

// TestProteinFallsBackToBodyMassAndSaysSo: without lean mass the floor still has a basis,
// but a weaker one, and the flag is what lets the UI avoid overclaiming.
func TestProteinFallsBackToBodyMassAndSaysSo(t *testing.T) {
	got, ok := DeriveTargets(2500, -0.5, Inputs{MassKg: ptr(91.0)})
	if !ok {
		t.Fatal("targets not derivable from body mass alone")
	}
	if !got.ProteinFromBodyMass {
		t.Error("ProteinFromBodyMass not flagged")
	}
	closeTo(t, got.ProteinG, 2.0*91.0, 0.01, "protein from body mass")
}

// TestTargetsNeedMass: a rate in percent of body mass has nothing to be a percent of
// without one, and inventing a default weight would be worse than refusing.
func TestTargetsNeedMass(t *testing.T) {
	if _, ok := DeriveTargets(2500, -0.5, Inputs{LeanMassKg: ptr(66.0)}); ok {
		t.Fatal("targets derived with no body mass")
	}
}

// TestCarbNeverGoesNegative: at an extreme target the protein and fat floors can exceed
// the budget. A negative carbohydrate figure would be nonsense; the guardrail is what
// reports the situation.
func TestCarbNeverGoesNegative(t *testing.T) {
	got, ok := DeriveTargets(1200, -2.5, Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)})
	if !ok {
		t.Fatal("targets not derivable")
	}
	if got.CarbG < 0 {
		t.Fatalf("carb = %v, want floored at zero", got.CarbG)
	}
	rails := Guardrails(got, -2.5, ptr(1804.0), nil)
	if !hasRail(rails, GuardrailCarbSqueezedOut) {
		t.Errorf("no %q guardrail for an unmeetable target: %+v", GuardrailCarbSqueezedOut, rails)
	}
}

// --- Guardrails ---

func hasRail(rails []Guardrail, code string) bool {
	for _, r := range rails {
		if r.Code == code {
			return true
		}
	}
	return false
}

func TestGuardrailTargetBelowBasal(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)}
	targets, _ := DeriveTargets(2390, -1.0, in) // ≈1390 kcal, under a 1804 basal

	rails := Guardrails(targets, -1.0, ptr(1804.0), nil)
	if !hasRail(rails, GuardrailTargetBelowBasal) {
		t.Errorf("no below-basal guardrail: %+v", rails)
	}
	// And it must not fire at maintenance.
	maintain, _ := DeriveTargets(2390, 0, in)
	if hasRail(Guardrails(maintain, 0, ptr(1804.0), nil), GuardrailTargetBelowBasal) {
		t.Error("below-basal guardrail fired at maintenance")
	}
}

func TestGuardrailUnsustainableRate(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)}
	targets, _ := DeriveTargets(2390, -1.5, in)
	if !hasRail(Guardrails(targets, -1.5, nil, nil), GuardrailRateUnsustainable) {
		t.Error("no unsustainable-rate guardrail at −1.5 %/week")
	}
	targets, _ = DeriveTargets(2390, -0.5, in)
	if hasRail(Guardrails(targets, -0.5, nil, nil), GuardrailRateUnsustainable) {
		t.Error("unsustainable-rate guardrail fired at a moderate −0.5 %/week")
	}
}

// TestGuardrailProteinOnlyInDeficit: under-eating protein while bulking is not the
// failure mode this warning is about, and firing there would dilute it.
func TestGuardrailProteinOnlyInDeficit(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)}
	targets, _ := DeriveTargets(2390, -0.5, in)
	low := Adherence{ActualProteinG: ptr(100.0)} // floor is ≈134 g

	if !hasRail(Guardrails(targets, -0.5, nil, &low), GuardrailProteinBelowFloor) {
		t.Error("no protein guardrail in a deficit under the floor")
	}

	bulk, _ := DeriveTargets(2390, 0.25, in)
	if hasRail(Guardrails(bulk, 0.25, nil, &low), GuardrailProteinBelowFloor) {
		t.Error("protein guardrail fired during a surplus")
	}
}

func TestGuardrailsAreNeverNilCoded(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)}
	targets, _ := DeriveTargets(2390, -0.3, in)
	for _, r := range Guardrails(targets, -0.3, ptr(1500.0), nil) {
		if r.Code == "" || r.Message == "" {
			t.Errorf("guardrail with empty code or message: %+v", r)
		}
	}
}

// --- Preselection ---

func basalsFor(in Inputs) []BasalEstimate { return Basal(in) }

// TestPreselectDemotesLeanEquationsWhenCompositionIsEstimated is the mechanism behind the
// whole trust setting: a scale that derives body fat from body weight turns Katch-McArdle
// into a weight equation, so Mifflin-St Jeor then uses strictly more information.
func TestPreselectDemotesLeanEquationsWhenCompositionIsEstimated(t *testing.T) {
	full := Inputs{
		MassKg: ptr(91.0), HeightCm: ptr(184.0), LeanMassKg: ptr(66.4),
		AgeYears: ptr(30.0), Sex: SexMale,
	}
	basals := basalsFor(full)

	if got := Preselect(basals, TrustMeasured); got != "katch_mcardle" {
		t.Errorf("measured trust preselected %q, want katch_mcardle", got)
	}
	if got := Preselect(basals, TrustEstimated); got != "mifflin_st_jeor" {
		t.Errorf("estimated trust preselected %q, want mifflin_st_jeor", got)
	}
	if got := Preselect(basals, TrustUnknown); got != "mifflin_st_jeor" {
		t.Errorf("unknown trust preselected %q, want mifflin_st_jeor", got)
	}
}

// TestPreselectFallsBackToWhatIsComputable is the reference Account's real case: age and
// sex absent, so despite distrusting the scale there is nothing else to offer.
func TestPreselectFallsBackToWhatIsComputable(t *testing.T) {
	in := Inputs{MassKg: ptr(91.0), HeightCm: ptr(184.0), LeanMassKg: ptr(66.4)}
	if got := Preselect(basalsFor(in), TrustEstimated); got != "katch_mcardle" {
		t.Errorf("preselected %q, want katch_mcardle — demoted is not hidden", got)
	}
}

func TestPreselectEmptyWhenNothingComputable(t *testing.T) {
	if got := Preselect(basalsFor(Inputs{}), TrustMeasured); got != "" {
		t.Errorf("preselected %q with no inputs, want empty", got)
	}
}

// TestDerivedTrustTreatsManualAsMeasured: typing a value already expresses a judgement —
// nobody hand-enters a figure they distrust — whereas a scale expresses only that a scale
// was stood on.
func TestDerivedTrustTreatsManualAsMeasured(t *testing.T) {
	if got := DerivedTrust("Manual"); got != TrustMeasured {
		t.Errorf("Manual → %q, want measured", got)
	}
	if got := DerivedTrust("Zepp Life"); got != TrustEstimated {
		t.Errorf("Zepp Life → %q, want estimated", got)
	}
	if got := DerivedTrust(""); got != TrustUnknown {
		t.Errorf("no source → %q, want unknown", got)
	}
}

// --- Adherence ---

// TestAdherenceMeasuresOverThePhaseWindow uses a backdated start, which the HTTP layer
// cannot express: a Phase always opens at "now" there, so the API test can only prove the
// empty-window case. This one proves the figures.
func TestAdherenceMeasuresOverThePhaseWindow(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	started := now.AddDate(0, 0, -20)

	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 20, func(int) float64 { return 2100 })
	seedDaily(t, models, acc, metricProtein, "g", "Yazio", now, 20, func(int) float64 { return 118 })
	seedDaily(t, models, acc, metricBodyMass, "kg", "Zepp Life", now, 20, func(i int) float64 {
		return 92 - 1.2*float64(i)/19
	})

	targets, _ := DeriveTargets(2500, -0.5, Inputs{MassKg: ptr(91.0), LeanMassKg: ptr(66.4)})
	got, err := e.Adherence(context.Background(), acc, started, targets, -0.5, now)
	if err != nil {
		t.Fatalf("Adherence: %v", err)
	}

	if got.WindowDays != 20 {
		t.Errorf("window = %d days, want the phase's 20 — not a fixed 28", got.WindowDays)
	}
	if got.Thin {
		t.Error("a 20-day window is flagged thin")
	}
	if got.ActualKcal == nil || got.ActualProteinG == nil || got.ActualRatePctPerWeek == nil {
		t.Fatalf("actuals missing: %+v", got)
	}
	closeTo(t, *got.ActualKcal, 2100, 1, "actual intake")
	closeTo(t, *got.ActualProteinG, 118, 1, "actual protein")
	if *got.ActualRatePctPerWeek >= 0 {
		t.Errorf("actual rate = %v, want negative over a losing window", *got.ActualRatePctPerWeek)
	}
	// Adherence carries no lean-mass figure, deliberately: on a bioimpedance scale a cut
	// mechanically renders lean-mass "loss" whether or not any muscle was lost.
	closeTo(t, got.TargetProteinG, targets.ProteinG, 0.01, "target protein carried through")
}

// TestAdherenceOnAJustOpenedPhaseHasNoActuals: the empty window must yield absent figures,
// never zeros, and must not reach the engine with a non-positive range.
func TestAdherenceOnAJustOpenedPhaseHasNoActuals(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	seedDaily(t, models, acc, metricIntake, "kcal", "Yazio", now, 20, func(int) float64 { return 2100 })

	targets, _ := DeriveTargets(2500, -0.5, Inputs{MassKg: ptr(91.0)})
	got, err := e.Adherence(context.Background(), acc, now, targets, -0.5, now)
	if err != nil {
		t.Fatalf("Adherence: %v", err)
	}
	if !got.Thin {
		t.Error("a zero-length window is not flagged thin")
	}
	if got.ActualKcal != nil {
		t.Errorf("actual intake = %v, want absent", *got.ActualKcal)
	}
}
