package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// seedPlanData gives the test Account the shape of the reference export: a dense food
// log, a falling body mass, measured lean mass, and devices that overstate the burn.
func seedPlanData(t *testing.T, models data.Models, days int) {
	t.Helper()
	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	now := time.Now().UTC()

	rows := []data.Measurement{}
	add := func(metric, unit, source string, i int, value float64) {
		at := now.AddDate(0, 0, -days+i).Format(time.RFC3339)
		rows = append(rows, data.Measurement{
			AccountID: acc.ID, Metric: metric, Value: value, OriginalUnit: unit,
			StartAt: at, EndAt: at, Source: source,
			ContentKey: fmt.Sprintf("%s-%s-%d", metric, source, i),
		})
	}
	for i := range days {
		add("dietary_energy", "kcal", "Yazio", i, 2078)
		add("height", "cm", "iPhone", i, 184)
		add("dietary_protein", "g", "Yazio", i, 118)
		add("body_mass", "kg", "Zepp Life", i, 92.75-1.75*float64(i)/float64(days-1))
		add("lean_body_mass", "kg", "Zepp Life", i, 66.4)
		add("basal_energy", "kcal", "Watch", i, 2280)
		add("active_energy", "kcal", "Watch", i, 1250)
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

type planPayload struct {
	Basal []struct {
		Equation string   `json:"equation"`
		Kcal     *float64 `json:"kcal"`
		Missing  []string `json:"missing"`
	} `json:"basal"`
	PreselectedEquation string `json:"preselected_equation"`
	Expenditure         *struct {
		Kcal  float64 `json:"kcal"`
		Basis string  `json:"basis"`
	} `json:"expenditure"`
	ActualRate *struct {
		PctPerWeek float64 `json:"pct_per_week"`
	} `json:"actual_rate"`
	Phase *struct {
		ID             int64   `json:"id"`
		RatePctPerWeek float64 `json:"rate_pct_per_week"`
	} `json:"phase"`
	Rate    float64 `json:"rate_pct_per_week"`
	Targets *struct {
		Kcal              float64 `json:"kcal"`
		ProteinG          float64 `json:"protein_g"`
		FatG              float64 `json:"fat_g"`
		CarbG             float64 `json:"carb_g"`
		ConventionalSplit bool    `json:"conventional_split"`
	} `json:"targets"`
	Adherence *struct {
		WindowDays     int      `json:"window_days"`
		Thin           bool     `json:"thin"`
		TargetProteinG float64  `json:"target_protein_g"`
		TargetKcal     float64  `json:"target_kcal"`
		ActualKcal     *float64 `json:"actual_kcal"`
		ActualProteinG *float64 `json:"actual_protein_g"`
	} `json:"adherence"`
	Guardrails []struct {
		Code string `json:"code"`
	} `json:"guardrails"`
	Insufficient bool `json:"insufficient"`
}

func getPlan(t *testing.T, srv *Server, target string, cookie *http.Cookie) planPayload {
	t.Helper()
	res, body := do(t, srv, target, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", target, res.StatusCode)
	}
	var plan planPayload
	if err := json.Unmarshal(body["plan"], &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	return plan
}

func TestPlanUsesObservedBasisAndDerivesTargets(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	plan := getPlan(t, srv, "/v1/plan?rate=-0.5", cookie)

	if plan.Expenditure == nil || plan.Expenditure.Basis != "observed" {
		t.Fatalf("expenditure = %+v, want the observed basis", plan.Expenditure)
	}
	// The devices claim 3530/day; the body says far less. The cascade must take the body.
	if plan.Expenditure.Kcal > 3000 {
		t.Errorf("expenditure = %.0f — it followed the devices", plan.Expenditure.Kcal)
	}
	if plan.Targets == nil {
		t.Fatal("no targets for an explicit rate")
	}
	// A cut must land *below* expenditure. This is the sign check: written as a
	// subtraction, the formula turns every cut into a surplus.
	if plan.Targets.Kcal >= plan.Expenditure.Kcal {
		t.Errorf("target %.0f is not below expenditure %.0f — the deficit sign is inverted",
			plan.Targets.Kcal, plan.Expenditure.Kcal)
	}
	if !plan.Targets.ConventionalSplit {
		t.Error("the fat/carb split is not flagged as a convention")
	}
	// Protein floor at −0.5 %/week is 2.0 g/kg of 66.4 kg lean ≈ 133 g.
	if plan.Targets.ProteinG < 125 || plan.Targets.ProteinG > 140 {
		t.Errorf("protein = %.0f g, want ≈133", plan.Targets.ProteinG)
	}
}

// TestPlanPreselectionFollowsTrust exercises the seam between the profile setting and the
// equation picker: the same data preselects differently depending on the declaration.
func TestPlanPreselectionFollowsTrust(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	// No age or sex yet: only the lean-mass equations can run, so even distrust cannot
	// move the preselection — demoted is not hidden.
	plan := getPlan(t, srv, "/v1/plan", cookie)
	if plan.PreselectedEquation != "katch_mcardle" {
		t.Errorf("preselected %q, want katch_mcardle when nothing else is computable", plan.PreselectedEquation)
	}

	// Fill in age and sex, and declare the scale untrustworthy.
	res, _ := send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{
		"date_of_birth": "1996-03-15", "biological_sex": "male",
		"body_composition_trust": "estimated",
	}, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("profile patch status = %d, want 200", res.StatusCode)
	}

	plan = getPlan(t, srv, "/v1/plan", cookie)
	if plan.PreselectedEquation != "mifflin_st_jeor" {
		t.Errorf("preselected %q, want mifflin_st_jeor once the scale is distrusted", plan.PreselectedEquation)
	}
	for _, b := range plan.Basal {
		if b.Equation == "katch_mcardle" && b.Kcal == nil {
			t.Error("katch_mcardle was hidden rather than demoted")
		}
	}

	// Trusting the scale flips it back.
	send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{"body_composition_trust": "measured"}, cookie)
	plan = getPlan(t, srv, "/v1/plan", cookie)
	if plan.PreselectedEquation != "katch_mcardle" {
		t.Errorf("preselected %q, want katch_mcardle once the scale is trusted", plan.PreselectedEquation)
	}
}

// TestPlanRateFallsBackToTheMeasuredRate: with no Phase and no override, the page opens
// on what the Account is already doing rather than on an empty form.
func TestPlanRateFallsBackToTheMeasuredRate(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	plan := getPlan(t, srv, "/v1/plan", cookie)
	if plan.ActualRate == nil {
		t.Fatal("no measured actual rate from 28 days of readings")
	}
	if plan.Rate != plan.ActualRate.PctPerWeek {
		t.Errorf("rate = %v, want the measured %v", plan.Rate, plan.ActualRate.PctPerWeek)
	}
	if plan.Phase != nil {
		t.Error("a phase appeared without one being opened")
	}
	if plan.Adherence != nil {
		t.Error("adherence reported with no open phase")
	}
}

func TestPlanRatePrefersTheOpenPhase(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	res, _ := send(t, srv, http.MethodPost, "/v1/phases", map[string]any{"rate_pct_per_week": -0.75}, cookie)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open phase status = %d, want 201", res.StatusCode)
	}

	plan := getPlan(t, srv, "/v1/plan", cookie)
	if plan.Phase == nil || plan.Phase.RatePctPerWeek != -0.75 {
		t.Fatalf("phase = %+v, want the opened −0.75", plan.Phase)
	}
	if plan.Rate != -0.75 {
		t.Errorf("rate = %v, want the phase's −0.75", plan.Rate)
	}

	// An explicit preview still wins, so dragging the slider does not require a commitment.
	preview := getPlan(t, srv, "/v1/plan?rate=0.25", cookie)
	if preview.Rate != 0.25 {
		t.Errorf("preview rate = %v, want 0.25", preview.Rate)
	}
	if preview.Phase == nil {
		t.Error("previewing a rate discarded the open phase from the payload")
	}
}

// TestPlanAdherenceUsesThePhaseWindow: a Phase opened moments ago is judged over that
// window, flagged thin — not over a fixed 28 days.
func TestPlanAdherenceUsesThePhaseWindow(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	send(t, srv, http.MethodPost, "/v1/phases", map[string]any{"rate_pct_per_week": -0.5}, cookie)

	plan := getPlan(t, srv, "/v1/plan", cookie)
	if plan.Adherence == nil {
		t.Fatal("no adherence with an open phase")
	}
	if plan.Adherence.WindowDays > 2 {
		t.Errorf("window = %d days, want the phase's own (just opened)", plan.Adherence.WindowDays)
	}
	if !plan.Adherence.Thin {
		t.Error("a one-day window is not flagged thin")
	}
	// A Phase opened moments ago has no window to measure over yet, so the actuals are
	// absent rather than zero — and the endpoint must not fail on the empty range.
	if plan.Adherence.ActualProteinG != nil {
		t.Errorf("protein = %v for a just-opened phase, want absent", *plan.Adherence.ActualProteinG)
	}
	if plan.Adherence.TargetProteinG == 0 {
		t.Error("the target protein floor is missing from adherence")
	}
}

func TestPlanGuardrailsWarnWithoutBlocking(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	plan := getPlan(t, srv, "/v1/plan?rate=-2", cookie)
	if plan.Targets == nil {
		t.Fatal("an aggressive rate returned no targets; guardrails must warn, not block")
	}
	codes := map[string]bool{}
	for _, g := range plan.Guardrails {
		codes[g.Code] = true
	}
	if !codes["rate_unsustainable"] {
		t.Errorf("no rate_unsustainable guardrail at −2 %%/week: %+v", plan.Guardrails)
	}
	if !codes["target_below_basal"] {
		t.Errorf("no target_below_basal guardrail at −2 %%/week: %+v", plan.Guardrails)
	}
}

// TestPlanOnEmptyAccountReportsInsufficient: no data must yield a stated refusal, never a
// zero that would flow straight into a calorie target.
func TestPlanOnEmptyAccountReportsInsufficient(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	plan := getPlan(t, srv, "/v1/plan", cookie)
	if !plan.Insufficient {
		t.Fatal("an empty Account did not report insufficient data")
	}
	if plan.Expenditure != nil || plan.Targets != nil {
		t.Errorf("figures returned for an empty Account: %+v / %+v", plan.Expenditure, plan.Targets)
	}
}

func TestPlanRejectsAbsurdRate(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	for _, rate := range []string{"9", "-9", "abc"} {
		res, _ := do(t, srv, "/v1/plan?rate="+rate, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("rate=%s status = %d, want 422", rate, res.StatusCode)
		}
	}
}

// --- Phase endpoints ---

func TestOpenPhaseClosesThePreviousOne(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	send(t, srv, http.MethodPost, "/v1/phases", map[string]any{"rate_pct_per_week": -0.5}, cookie)
	send(t, srv, http.MethodPost, "/v1/phases", map[string]any{"rate_pct_per_week": 0.25}, cookie)

	res, body := do(t, srv, "/v1/phases", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var phases []struct {
		ID      int64   `json:"id"`
		Rate    float64 `json:"rate_pct_per_week"`
		EndedAt *string `json:"ended_at"`
	}
	if err := json.Unmarshal(body["phases"], &phases); err != nil {
		t.Fatalf("decode phases: %v", err)
	}
	if len(phases) != 2 {
		t.Fatalf("history has %d phases, want 2", len(phases))
	}
	open := 0
	for _, p := range phases {
		if p.EndedAt == nil {
			open++
		}
	}
	if open != 1 {
		t.Errorf("%d open phases, want exactly 1", open)
	}
}

func TestClosePhaseThenDelete(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	_, body := send(t, srv, http.MethodPost, "/v1/phases", map[string]any{"rate_pct_per_week": -0.5}, cookie)
	var created struct {
		Phase struct {
			ID int64 `json:"id"`
		} `json:"phase"`
	}
	raw, _ := json.Marshal(map[string]json.RawMessage{"phase": body["phase"]})
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created phase: %v", err)
	}

	id := itoa(created.Phase.ID)
	res, _ := send(t, srv, http.MethodPatch, "/v1/phases/"+id, map[string]any{}, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("close status = %d, want 200", res.StatusCode)
	}
	// Re-closing rewrites history and must be refused.
	res, _ = send(t, srv, http.MethodPatch, "/v1/phases/"+id, map[string]any{}, cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("second close status = %d, want 404", res.StatusCode)
	}

	res, _ = send(t, srv, http.MethodDelete, "/v1/phases/"+id, nil, cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", res.StatusCode)
	}
}

func TestPhaseEndpointsAreAccountScoped(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedAccountWithPassword(t, models, "other@example.com", testPassword)
	otherCookie := login(t, srv, "other@example.com", testPassword)

	_, body := send(t, srv, http.MethodPost, "/v1/phases", map[string]any{"rate_pct_per_week": -0.5}, otherCookie)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body["phase"], &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	res, _ := send(t, srv, http.MethodDelete, "/v1/phases/"+itoa(created.ID), nil, cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("cross-account delete = %d, want 404", res.StatusCode)
	}
}

func TestOpenPhaseValidatesRate(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	for _, body := range []map[string]any{{}, {"rate_pct_per_week": 99}} {
		res, _ := send(t, srv, http.MethodPost, "/v1/phases", body, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("body %v status = %d, want 422", body, res.StatusCode)
		}
	}
}

// --- Profile ---

func TestProfileRoundTripsAndValidates(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	res, body := do(t, srv, "/v1/profile", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var profile profileView
	if err := json.Unmarshal(body["profile"], &profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.DateOfBirth != nil {
		t.Error("a fresh Account has a date of birth")
	}
	if profile.DerivedTrust != "unknown" {
		t.Errorf("derived trust = %q, want unknown with no composition data", profile.DerivedTrust)
	}

	res, _ = send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{"date_of_birth": "1996-03-15"}, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", res.StatusCode)
	}

	for _, bad := range []map[string]any{
		{"date_of_birth": "15/03/1996"},
		{"date_of_birth": "1850-01-01"},
		{"biological_sex": "other"},
		{"body_composition_trust": "kinda"},
	} {
		res, _ := send(t, srv, http.MethodPatch, "/v1/profile", bad, cookie)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("patch %v status = %d, want 422", bad, res.StatusCode)
		}
	}
}

// TestDerivedTrustFollowsTheSource: a hand-entered body fat is a judgement already made,
// a scale reading is not.
func TestDerivedTrustFollowsTheSource(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 14)

	res, body := do(t, srv, "/v1/profile", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var profile profileView
	json.Unmarshal(body["profile"], &profile)
	if profile.DerivedTrust != "estimated" {
		t.Errorf("derived trust = %q, want estimated for scale data", profile.DerivedTrust)
	}

	send(t, srv, http.MethodPost, "/v1/measurements", map[string]any{
		"metric": "body_fat_percentage", "value": 0.22,
	}, cookie)

	_, body = do(t, srv, "/v1/profile", cookie)
	json.Unmarshal(body["profile"], &profile)
	if profile.DerivedTrust != "measured" {
		t.Errorf("derived trust = %q, want measured once a value is entered by hand", profile.DerivedTrust)
	}
}

func TestPlanAndProfileRequireAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, target := range []string{"/v1/plan", "/v1/phases", "/v1/profile"} {
		res, _ := do(t, srv, target)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", target, res.StatusCode)
		}
	}
}

// TestProfileNullClearsAField goes through the HTTP layer on purpose. The data-layer test
// built a ProfilePatch in Go directly, so it proved the model and never exercised the JSON
// contract — which is how `{"date_of_birth": null}` shipped as a silent no-op. A `**string`
// field cannot express the difference: encoding/json decodes null by nilling the outer
// pointer, giving byte-for-byte the same result as an absent key.
func TestProfileNullClearsAField(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	res, _ := send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{
		"date_of_birth": "1996-11-10", "biological_sex": "male",
	}, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set status = %d, want 200", res.StatusCode)
	}

	res, body := send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{
		"date_of_birth": nil,
	}, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", res.StatusCode)
	}
	var profile profileView
	if err := json.Unmarshal(body["profile"], &profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.DateOfBirth != nil {
		t.Errorf("date_of_birth = %q, want cleared by an explicit null", *profile.DateOfBirth)
	}
	if profile.BiologicalSex == nil || *profile.BiologicalSex != "male" {
		t.Errorf("biological_sex = %v; clearing one field must not touch another", profile.BiologicalSex)
	}
}

// TestProfileOmittedFieldIsUntouched is the other half of the contract: absent must remain
// distinguishable from null now that both are representable.
func TestProfileOmittedFieldIsUntouched(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{
		"date_of_birth": "1996-11-10", "biological_sex": "male",
	}, cookie)
	_, body := send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{
		"body_composition_trust": "measured",
	}, cookie)

	var profile profileView
	if err := json.Unmarshal(body["profile"], &profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.DateOfBirth == nil || *profile.DateOfBirth != "1996-11-10" {
		t.Errorf("date_of_birth = %v, want untouched by an unrelated patch", profile.DateOfBirth)
	}
	if profile.BiologicalSex == nil || *profile.BiologicalSex != "male" {
		t.Errorf("biological_sex = %v, want untouched", profile.BiologicalSex)
	}
}

func TestProfileRejectsMalformedField(t *testing.T) {
	srv, _, cookie := newTestServer(t)

	res, _ := send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{"date_of_birth": 1996}, cookie)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("numeric date status = %d, want 400", res.StatusCode)
	}
	res, _ = send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{"favourite_colour": "blue"}, cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown field status = %d, want 422", res.StatusCode)
	}
}

// TestProfileClearingRemovesAnEquation closes the loop: clearing the date of birth must
// make the anthropometric equations uncomputable again, not leave a stale figure behind.
func TestProfileClearingRemovesAnEquation(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedPlanData(t, models, 28)

	send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{
		"date_of_birth": "1996-11-10", "biological_sex": "male",
	}, cookie)
	plan := getPlan(t, srv, "/v1/plan", cookie)
	if kcalFor(plan, "mifflin_st_jeor") == nil {
		t.Fatal("Mifflin-St Jeor uncomputable with a date of birth and a sex set")
	}

	send(t, srv, http.MethodPatch, "/v1/profile", map[string]any{"date_of_birth": nil}, cookie)
	plan = getPlan(t, srv, "/v1/plan", cookie)
	if kcalFor(plan, "mifflin_st_jeor") != nil {
		t.Error("Mifflin-St Jeor still computable after the date of birth was cleared")
	}
}

func kcalFor(plan planPayload, equation string) *float64 {
	for _, b := range plan.Basal {
		if b.Equation == equation {
			return b.Kcal
		}
	}
	return nil
}
