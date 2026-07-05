package applehealth

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeStateValue(t *testing.T) {
	cases := map[string]string{
		"HKCategoryValueSleepAnalysisInBed":      "in_bed",
		"HKCategoryValueSleepAnalysisAsleepCore": "asleep_core",
		"HKCategoryValueSleepAnalysisAsleepREM":  "asleep_rem",
		"HKCategoryValueSleepAnalysisAwake":      "awake",
		"HKCategoryValueAppleStandHourStood":     "stood",
		"HKCategoryValueAppleStandHourIdle":      "idle",
		// Unknown value under a mapped kind is derived (prefix trimmed,
		// snake-cased), not dropped — kept as a best-effort neutral slug.
		"HKCategoryValueSleepAnalysisSomethingNew": "sleep_analysis_something_new",
	}
	for in, want := range cases {
		if got := normalizeStateValue(in); got != want {
			t.Errorf("normalizeStateValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeActivityType(t *testing.T) {
	cases := map[string]string{
		"HKWorkoutActivityTypeRunning":                       "running",
		"HKWorkoutActivityTypeWalking":                       "walking",
		"HKWorkoutActivityTypeTraditionalStrengthTraining":   "traditional_strength_training",
		"HKWorkoutActivityTypeOther":                         "other",
		"HKWorkoutActivityTypeHighIntensityIntervalTraining": "high_intensity_interval_training",
	}
	for in, want := range cases {
		if got := normalizeActivityType(in); got != want {
			t.Errorf("normalizeActivityType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStateKind(t *testing.T) {
	if k, ok := stateKind("HKCategoryTypeIdentifierSleepAnalysis"); !ok || k != "sleep" {
		t.Errorf("sleep kind = (%q, %v), want (sleep, true)", k, ok)
	}
	if k, ok := stateKind("HKCategoryTypeIdentifierAppleStandHour"); !ok || k != "stand" {
		t.Errorf("stand kind = (%q, %v), want (stand, true)", k, ok)
	}
	// An event category is not a State: it must stay in the Unmapped bin.
	if _, ok := stateKind("HKCategoryTypeIdentifierLowHeartRateEvent"); ok {
		t.Error("LowHeartRateEvent should not be a State kind")
	}
}

// TestImportStreamStandHourIsState guards that stand hours join sleep as States
// rather than falling into the Unmapped bin.
func TestImportStreamStandHourIsState(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	const xml = `<HealthData locale="en_US">
 <Record type="HKCategoryTypeIdentifierAppleStandHour" sourceName="Watch" startDate="2025-07-23 13:00:00 +0000" endDate="2025-07-23 14:00:00 +0000" value="HKCategoryValueAppleStandHourStood"/>
 <Record type="HKCategoryTypeIdentifierLowHeartRateEvent" sourceName="Watch" startDate="2025-07-23 13:00:00 +0000" endDate="2025-07-23 13:00:00 +0000" value="HKCategoryValueLowHeartRateEvent"/>
</HealthData>`

	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(xml), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}
	if got := report.PerState["stand"].Added; got != 1 {
		t.Errorf("stand states added = %d, want 1", got)
	}
	// The heart-rate event is not a modeled family yet → Unmapped, kept.
	if report.Unmapped != 1 {
		t.Errorf("Unmapped = %d, want 1 (the event)", report.Unmapped)
	}

	var value string
	if err := db.QueryRowContext(ctx,
		`SELECT state_value FROM states WHERE account_id = ? AND kind = 'stand'`, acc).Scan(&value); err != nil {
		t.Fatalf("select stand state: %v", err)
	}
	if value != "stood" {
		t.Errorf("state_value = %q, want stood", value)
	}
}
