package estimate

// Trust is the Account's own judgement of its body-composition data. It is a
// declaration, not an inference: Verve can see that a scale's body fat tracks body
// weight at r = 0.99 and still asks, because the Account knows whether the figure came
// from a DEXA scan or a bathroom scale and that is a judgement, not a statistic.
type Trust string

const (
	TrustMeasured  Trust = "measured"  // DEXA, hydrostatic — a real composition measurement
	TrustEstimated Trust = "estimated" // a bioimpedance scale
	TrustUnknown   Trust = "unknown"   // unset, or the Account does not know
)

// leanMassEquations are the equations whose only input is lean mass, and therefore the
// ones whose worth depends entirely on that input being real.
var leanMassEquations = map[string]bool{"katch_mcardle": true, "cunningham": true}

// Preselect names the equation the UI should open on: the best computable one, where
// "best" depends on Trust.
//
// With trusted composition, a lean-mass equation wins — it needs neither age nor sex,
// and lean tissue already encodes what they stand in for. With estimated or unknown
// composition it is **demoted below** the anthropometric equations, because a scale that
// derives body fat from body weight turns Katch-McArdle into a weight equation wearing a
// composition costume: Mifflin-St Jeor then uses strictly more independent information.
//
// Demoted, never hidden. The Account may still pick it, and the figure is still shown.
// Returns "" when nothing is computable at all.
func Preselect(basals []BasalEstimate, trust Trust) string {
	computable := map[string]bool{}
	for _, b := range basals {
		if b.Kcal != nil {
			computable[b.Equation] = true
		}
	}

	preferLean := trust == TrustMeasured
	order := []string{"mifflin_st_jeor", "harris_benedict", "katch_mcardle", "cunningham"}
	if preferLean {
		order = []string{"katch_mcardle", "cunningham", "mifflin_st_jeor", "harris_benedict"}
	}

	for _, id := range order {
		if computable[id] {
			return id
		}
	}
	return ""
}

// DerivedTrust is the suggestion offered when the Account has not declared one: a manual
// lean-mass or body-fat entry is treated as measured, anything from a Connector as
// estimated. Typing a value already expresses a judgement — nobody hand-enters a figure
// they distrust — whereas a scale expresses only that a scale was stood on.
func DerivedTrust(leanMassSource string) Trust {
	if leanMassSource == "Manual" {
		return TrustMeasured
	}
	if leanMassSource == "" {
		return TrustUnknown
	}
	return TrustEstimated
}
