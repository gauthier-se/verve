package estimate

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

// TestProbeRealDatabase runs the whole engine against a copy of a real Apple Health
// export and logs what it resolves. It is a diagnostic, not an assertion: fixtures can
// only prove the arithmetic, whereas the questions that actually bite here — is height
// stale, does the intake window have coverage, is the resulting ratio physiologically
// plausible — need real data to answer.
//
// Skipped unless VERVE_PROBE_DB points at a database copy, so it never runs in CI and
// never touches a live one. Run it with:
//
//	VERVE_PROBE_DB=/path/to/copy.db go test ./internal/estimate -run Probe -v
func TestProbeRealDatabase(t *testing.T) {
	path := os.Getenv("VERVE_PROBE_DB")
	if path == "" {
		t.Skip("VERVE_PROBE_DB not set")
	}
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	e := Engine{Query: query.Engine{DB: db}}
	now := time.Now().UTC()
	ctx := context.Background()

	// Read the real Account columns rather than passing an empty Profile. The whole point
	// of this probe is to answer "what will the app actually show", and the two
	// anthropometric equations depend entirely on these two fields — an empty Profile
	// reports them uncomputable even when the Account has them filled in.
	account, err := data.NewModels(db).Accounts.GetByID(ctx, 1)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	profile := Profile{}
	if account.DateOfBirth != nil {
		if dob, err := time.Parse("2006-01-02", *account.DateOfBirth); err == nil {
			profile.DateOfBirth = &dob
		}
	}
	if account.BiologicalSex != nil {
		profile.Sex = Sex(*account.BiologicalSex)
	}
	trust := TrustUnknown
	if account.BodyCompositionTrust != nil {
		trust = Trust(*account.BodyCompositionTrust)
	}
	dobLabel := "absent"
	if account.DateOfBirth != nil {
		dobLabel = *account.DateOfBirth
	}
	t.Logf("PROFILE      dob=%s sex=%q trust=%s", dobLabel, profile.Sex, trust)

	in, err := e.ResolveInputs(ctx, 1, profile, now)
	if err != nil {
		t.Fatalf("ResolveInputs: %v", err)
	}
	show := func(label string, v *float64) {
		if v == nil {
			t.Logf("  %-12s absent", label)
			return
		}
		t.Logf("  %-12s %.2f", label, *v)
	}
	t.Log("INPUTS")
	show("mass kg", in.MassKg)
	show("height cm", in.HeightCm)
	show("lean kg", in.LeanMassKg)
	t.Logf("  lean derived? %v", in.LeanMassDerived)

	basals := Basal(in)
	t.Logf("PRESELECTED  %s", Preselect(basals, trust))
	t.Log("BASAL")
	var katch *float64
	for _, b := range basals {
		if b.Kcal == nil {
			t.Logf("  %-16s uncomputable, missing %v", b.Name, b.Missing)
			continue
		}
		t.Logf("  %-16s %.0f kcal", b.Name, *b.Kcal)
		if b.Equation == "katch_mcardle" {
			katch = b.Kcal
		}
	}

	exp, err := e.Expenditure(ctx, 1, in, katch, now)
	if err != nil {
		t.Fatalf("Expenditure: %v", err)
	}
	t.Logf("EXPENDITURE  %.0f kcal  basis=%s", exp.Kcal, exp.Basis)
	if exp.MeanIntakeKcal != nil {
		t.Logf("  mean intake  %.0f kcal over %d days", *exp.MeanIntakeKcal, exp.IntakeDays)
	}
	if exp.MassSlopeKgPerDay != nil {
		t.Logf("  mass slope   %.4f kg/day (%d mass days)", *exp.MassSlopeKgPerDay, exp.MassDays)
	}

	rate, err := e.ActualRate(ctx, 1, now)
	if err != nil {
		t.Fatalf("ActualRate: %v", err)
	}
	if rate != nil {
		t.Logf("ACTUAL RATE  %.3f %%/week  (%.3f kg/week)", rate.PctPerWeek, rate.KgPerWeek)
	}
	if katch != nil {
		t.Logf("RATIO        TDEE/basal = %.2f", exp.Kcal / *katch)
	}
}
