package estimate

import (
	"context"
	"math"
	"time"
)

// Protein floors in grams per kilogram of lean mass. The floor rises with the depth of
// the deficit: in energy restriction, protein is what decides whether the mass lost is
// fat or muscle, which makes it the one non-calorie variable with a large, documented
// effect. Interpolated linearly between the two anchors.
const (
	proteinGPerKgAtMaintenance = 1.6 // rate ≥ 0
	proteinGPerKgAtDeepCut     = 2.4 // rate ≤ deepCutPctPerWeek
	deepCutPctPerWeek          = -1.0
)

// Fat floor. 0.6 g per kg of body mass is the usual hormonal-sufficiency figure; the
// 20%-of-calories floor keeps it sane at a very low target.
const (
	fatGPerKgBodyMass  = 0.6
	fatMinEnergyShare  = 0.20
	kcalPerGramProtein = 4
	kcalPerGramCarb    = 4
	kcalPerGramFat     = 9
)

// Guardrail thresholds.
const (
	unsustainableRatePctPerWeek = 1.0 // |rate| beyond this…
	unsustainableAfterDays      = 56  // …held longer than this (8 weeks)
	thinAdherenceWindowDays     = 14
)

// Targets is what a Target rate implies, derived against the Expenditure estimate.
//
// Protein is a **floor with evidence behind it**. Fat and carbohydrate are a
// **convention**: past a hormonal fat floor, the split between them has no demonstrated
// effect at equal calories and protein, so Verve states one rather than claiming to know
// it. ConventionalSplit exists so the UI can say which is which instead of presenting
// three numbers of equal authority.
type Targets struct {
	Kcal float64 `json:"kcal"`
	// KgPerWeek is the same Target rate in kilograms — the intuitive reading of a figure
	// the Account otherwise only sees as a percentage. Server-side because the client has
	// no body mass to convert with, and would otherwise back it out of the calorie gap:
	// arithmetic on the client is arithmetic nobody tests (ADR 0019).
	KgPerWeek         float64 `json:"kg_per_week"`
	ProteinG          float64 `json:"protein_g"`
	FatG              float64 `json:"fat_g"`
	CarbG             float64 `json:"carb_g"`
	ProteinGPerKgLean float64 `json:"protein_g_per_kg_lean"`
	// ProteinFromBodyMass records that the protein floor was scaled by body mass because
	// no lean mass was available. It is the weaker basis and the UI should say so.
	ProteinFromBodyMass bool `json:"protein_from_body_mass,omitempty"`
	// ConventionalSplit marks the fat/carbohydrate figures as a stated convention rather
	// than a recommendation.
	ConventionalSplit bool `json:"conventional_split"`
}

// DeriveTargets turns a Target rate into a calorie figure and macro targets against the
// Expenditure estimate:
//
//	kcal = expenditure + (rate% / 100 × mass × 7700 / 7)
//
// The sign is an addition, not a subtraction, because the rate is **signed**: a cut is a
// negative rate and adding a negative is what produces the deficit. (The PRD wrote this
// as a subtraction, which silently turned every cut into a surplus — caught by
// TestSurplusRaisesTheTarget, which asserts the ordering rather than any one figure.)
//
// Mass is required; without it a rate in percent of body mass has nothing to be a
// percent of, and inventing a default weight would be worse than refusing.
func DeriveTargets(expenditureKcal, ratePctPerWeek float64, in Inputs) (Targets, bool) {
	if in.MassKg == nil {
		return Targets{}, false
	}
	mass := *in.MassKg

	kcal := expenditureKcal + (ratePctPerWeek/100)*mass*energyPerKgMass/7

	// Protein scales on lean mass when known, body mass otherwise — a weaker basis,
	// flagged rather than silently substituted.
	proteinBasis, fromBodyMass := mass, true
	if in.LeanMassKg != nil {
		proteinBasis, fromBodyMass = *in.LeanMassKg, false
	}
	gPerKg := proteinPerKg(ratePctPerWeek)
	proteinG := gPerKg * proteinBasis

	fatG := math.Max(fatGPerKgBodyMass*mass, kcal*fatMinEnergyShare/kcalPerGramFat)

	// Carbohydrate is the remainder, floored at zero: at a very low target the protein
	// and fat floors can already exceed the budget, and a negative carbohydrate figure
	// would be nonsense rather than information. The guardrails are what flag that case.
	carbKcal := kcal - proteinG*kcalPerGramProtein - fatG*kcalPerGramFat
	carbG := math.Max(0, carbKcal/kcalPerGramCarb)

	return Targets{
		Kcal:                kcal,
		KgPerWeek:           ratePctPerWeek / 100 * mass,
		ProteinG:            proteinG,
		FatG:                fatG,
		CarbG:               carbG,
		ProteinGPerKgLean:   gPerKg,
		ProteinFromBodyMass: fromBodyMass,
		ConventionalSplit:   true,
	}, true
}

// proteinPerKg interpolates the protein floor between maintenance and a deep cut. A bulk
// takes the maintenance floor: a surplus does not threaten lean mass.
func proteinPerKg(ratePctPerWeek float64) float64 {
	if ratePctPerWeek >= 0 {
		return proteinGPerKgAtMaintenance
	}
	if ratePctPerWeek <= deepCutPctPerWeek {
		return proteinGPerKgAtDeepCut
	}
	frac := ratePctPerWeek / deepCutPctPerWeek // 0 at maintenance → 1 at a deep cut
	return proteinGPerKgAtMaintenance + frac*(proteinGPerKgAtDeepCut-proteinGPerKgAtMaintenance)
}

// Adherence compares what the Account meant to do against what it did, over the open
// Phase's real window — not a fixed 28 days, because a Phase opened five days ago must be
// judged over five days.
//
// It deliberately carries **no lean-mass figure**. Where body composition comes from a
// bioimpedance scale, reported lean mass can be a function of body weight alone, so a
// cut mechanically renders lean-mass "loss" whether or not any muscle was lost. Reporting
// a fabricated failure on the most consequential variable is worse than reporting nothing.
type Adherence struct {
	WindowDays int `json:"window_days"`
	// Thin marks a window too short to read much into, rather than suppressing the figures.
	Thin bool `json:"thin,omitempty"`

	TargetRatePctPerWeek float64  `json:"target_rate_pct_per_week"`
	ActualRatePctPerWeek *float64 `json:"actual_rate_pct_per_week,omitempty"`

	TargetKcal     float64  `json:"target_kcal"`
	ActualKcal     *float64 `json:"actual_kcal,omitempty"`
	TargetProteinG float64  `json:"target_protein_g"`
	ActualProteinG *float64 `json:"actual_protein_g,omitempty"`
}

// Adherence measures the open Phase against its own window. A nil actual is a gap (no
// data over the window), never a zero.
func (e Engine) Adherence(ctx context.Context, accountID int64, startedAt time.Time, targets Targets, targetRate float64, now time.Time) (Adherence, error) {
	days := int(math.Round(now.Sub(startedAt).Hours() / 24))
	if days < 1 {
		days = 1
	}
	out := Adherence{
		WindowDays:           days,
		Thin:                 days < thinAdherenceWindowDays,
		TargetRatePctPerWeek: targetRate,
		TargetKcal:           targets.Kcal,
		TargetProteinG:       targets.ProteinG,
	}

	// A Phase opened moments ago has no window to measure over. Return the targets with
	// no actuals rather than querying: the engine rejects a non-positive range outright
	// (ErrInvalidRange), which would turn "you just started" into a 500.
	if !now.After(startedAt) {
		return out, nil
	}

	if intake, err := e.dailyPoints(ctx, accountID, metricIntake, startedAt, now); err != nil {
		return Adherence{}, err
	} else if len(intake) > 0 {
		v := mean(values(intake))
		out.ActualKcal = &v
	}

	if protein, err := e.dailyPoints(ctx, accountID, metricProtein, startedAt, now); err != nil {
		return Adherence{}, err
	} else if len(protein) > 0 {
		v := mean(values(protein))
		out.ActualProteinG = &v
	}

	mass, err := e.dailyPoints(ctx, accountID, metricBodyMass, startedAt, now)
	if err != nil {
		return Adherence{}, err
	}
	if len(mass) >= 2 {
		if slope, ok := ordinaryLeastSquares(mass, startedAt); ok {
			if avg := mean(values(mass)); avg != 0 {
				rate := slope * 7 / avg * 100
				out.ActualRatePctPerWeek = &rate
			}
		}
	}

	return out, nil
}

// Guardrail is one advisory warning. Verve warns and never blocks: it does not know the
// Account's medical context, the same reason it refuses to colour a delta good or bad
// (ADR 0015).
type Guardrail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Guardrail codes, stable so the client can style or translate them.
const (
	GuardrailTargetBelowBasal  = "target_below_basal"
	GuardrailRateUnsustainable = "rate_unsustainable"
	GuardrailProteinBelowFloor = "protein_below_floor_in_deficit"
	GuardrailCarbSqueezedOut   = "carb_squeezed_out"
)

// Guardrails reports what is worth warning about for this rate and these targets. It
// returns advice, never an error: every caller renders these beside a usable control.
func Guardrails(targets Targets, ratePctPerWeek float64, basalKcal *float64, adherence *Adherence) []Guardrail {
	// Non-nil so the field marshals as [] rather than null: the contract says array, and a
	// client that trusts it (`guardrails.length`) would otherwise crash on the happy path —
	// the one case where nothing is worth warning about.
	out := []Guardrail{}

	if basalKcal != nil && targets.Kcal < *basalKcal {
		out = append(out, Guardrail{
			Code: GuardrailTargetBelowBasal,
			Message: "This target is below your resting expenditure. Sustained, that is more" +
				" restriction than the rate itself calls for.",
		})
	}

	if math.Abs(ratePctPerWeek) > unsustainableRatePctPerWeek {
		out = append(out, Guardrail{
			Code: GuardrailRateUnsustainable,
			Message: "Beyond about 1% of body mass per week, lean mass is increasingly likely" +
				" to go with the fat — especially past eight weeks.",
		})
	}

	if targets.CarbG == 0 {
		out = append(out, Guardrail{
			Code: GuardrailCarbSqueezedOut,
			Message: "The protein and fat floors already exceed this calorie target, so there" +
				" is nothing left for carbohydrate. The target is too low to be met as stated.",
		})
	}

	if adherence != nil && ratePctPerWeek < 0 &&
		adherence.ActualProteinG != nil && *adherence.ActualProteinG < targets.ProteinG {
		out = append(out, Guardrail{
			Code: GuardrailProteinBelowFloor,
			Message: "You are in a deficit and averaging under your protein floor — the" +
				" combination that costs the most lean mass.",
		})
	}

	return out
}
