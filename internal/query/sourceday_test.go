package query

import (
	"context"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
)

// Source resolution at day grain (ADR 0034). A Source wins the days it recorded,
// not the window it appears in: these tests pin the case that motivated it, which is
// two exports of the same life where one covers a fraction of the other's span.

// The mirror case, and the reason for the rule. A Google Health Takeout is mostly a
// copy of an Apple export synced through HealthKit, so importing both puts the same
// measurements under two Source names, with the copy covering three months of a
// history that runs for years. Electing one Source for the whole window (the old
// rule) hands every year to whichever name ranks first and blanks the rest.
func TestDayGrainKeepsWhatTheMirrorDoesNotCover(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// The watch, all four days.
		{"heart_rate", 60, "2024-01-01T08:00:00Z", "Apple Watch de Gauthier"},
		{"heart_rate", 61, "2024-01-02T08:00:00Z", "Apple Watch de Gauthier"},
		{"heart_rate", 62, "2024-01-03T08:00:00Z", "Apple Watch de Gauthier"},
		{"heart_rate", 63, "2024-01-04T08:00:00Z", "Apple Watch de Gauthier"},
		// Google's mirror of the same watch, two of them. It sorts before "Apple
		// Watch …" on the letter H, so alphabetical fallback used to hand it the lot.
		{"heart_rate", 62, "2024-01-03T08:00:00Z", "Apple Health Health Kit"},
		{"heart_rate", 63, "2024-01-04T08:00:00Z", "Apple Health Health Kit"},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-05T00:00:00Z")
	req.AccountID, req.Metric = acc, "heart_rate"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 4 {
		t.Fatalf("points = %d, want 4: the days the mirror does not cover are still there", len(s.Points))
	}
	// The days both Sources cover elect one of them, so nothing is counted twice.
	for _, p := range s.Points {
		if p.Count != 1 {
			t.Errorf("bucket %s folded %d rows, want 1: one Source wins a day", p.Bucket, p.Count)
		}
	}
	// heart_rate now has a priority entry, so the watch wins where both recorded.
	if s.Source != "Apple Watch de Gauthier" {
		t.Errorf("reported source = %q, want the watch, which won every day", s.Source)
	}
}

// Complementary coverage: two Sources, neither present on the other's days. Every
// day is served, by whichever Source has it.
func TestDayGrainServesComplementaryCoverage(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-01T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 200, "2024-01-02T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 300, "2024-01-03T08:00:00Z", "Phone Health Kit"},
		{"steps", 400, "2024-01-04T08:00:00Z", "Phone Health Kit"},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-05T00:00:00Z")
	req.AccountID, req.Metric = acc, "steps"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 4 {
		t.Fatalf("points = %d, want 4", len(s.Points))
	}
	got := []float64{}
	for _, p := range s.Points {
		got = append(got, p.Value)
	}
	want := []float64{100, 200, 300, 400}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("values = %v, want %v", got, want)
		}
	}
}

// Two Sources over the same days still elect one of them: day grain resolves
// overlap, it does not merge (ADR 0003 stands, only its grain moved).
func TestDayGrainStillElectsOnOverlap(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 900, "2024-01-01T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 500, "2024-01-01T08:00:00Z", "Phone Health Kit"},
		{"steps", 950, "2024-01-02T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 480, "2024-01-02T08:00:00Z", "Phone Health Kit"},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z")
	req.AccountID, req.Metric = acc, "steps"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 2 || s.Points[0].Value != 900 || s.Points[1].Value != 950 {
		t.Fatalf("points = %+v, want the watch's steps and no double counting", s.Points)
	}
	if s.Source != "Apple Watch de Gauthier" {
		t.Errorf("source = %q, want the watch", s.Source)
	}
}

// The summary shares the row set with the buckets, which is the argument for
// resolving at day grain rather than at the caller's bucket grain: a monthly chart,
// a daily chart and a headline figure cannot read different Sources.
func TestSummaryAgreesWithBucketsAcrossSources(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-01T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 200, "2024-01-02T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 300, "2024-01-03T08:00:00Z", "Phone Health Kit"},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-04T00:00:00Z")
	req.AccountID, req.Metric = acc, "steps"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Summary == nil {
		t.Fatal("no summary")
	}
	var total float64
	for _, p := range s.Points {
		total += p.Value
	}
	if s.Summary.Value != total || total != 600 {
		t.Errorf("summary = %v, buckets sum to %v, want both 600", s.Summary.Value, total)
	}
}

// A Manual entry still overlays its own day when the winner changes mid-window: the
// two day-grain mechanics compose rather than compete.
func TestOverlayComposesWithDayGrainWinners(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"body_mass", 91.0, "2024-01-01T08:00:00Z", "Zepp Life"},
		{"body_mass", 99.9, "2024-01-02T08:00:00Z", "YAZIO Health Kit"}, // the bad reading
		{"body_mass", 91.4, "2024-01-03T08:00:00Z", "YAZIO Health Kit"},
	})
	seed(t, e.DB, models, acc, []meas{
		{"body_mass", 91.2, "2024-01-02T09:00:00Z", catalog.SourceManual}, // the correction
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-04T00:00:00Z")
	req.AccountID, req.Metric = acc, "body_mass"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 3 {
		t.Fatalf("points = %+v, want three days", s.Points)
	}
	if s.Points[1].Value != 91.2 {
		t.Errorf("day 2 = %v, want the typed 91.2 rather than the scale's 99.9", s.Points[1].Value)
	}
	if s.Points[0].Value != 91.0 || s.Points[2].Value != 91.4 {
		t.Errorf("points = %+v, want the other days untouched", s.Points)
	}
}

// One Source everywhere emits the predicate this engine has always emitted, argument
// for argument. That is every Account holding a single export, so this milestone
// cannot move a curve any of them already has.
func TestSingleWinnerKeepsTheOriginalPredicate(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-01T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 200, "2024-01-02T08:00:00Z", "Apple Watch de Gauthier"},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z")
	req.AccountID, req.Metric = acc, "steps"
	f, err := e.resolveSource(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if len(f.runs) != 0 {
		t.Errorf("runs = %+v, want none: one winner needs no dated clause", f.runs)
	}
	pred, args := f.where(req)
	want := `account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?`
	if pred != want {
		t.Errorf("predicate = %q, want the original %q", pred, want)
	}
	if len(args) != 5 {
		t.Errorf("args = %v, want 5", args)
	}
}

// Consecutive days won by the same Source merge into one dated range, and merging
// across a day nobody recorded is safe: an empty day selects nothing either way. So
// the shape of the reference case is a handful of ranges, not one per day.
func TestRunsMergeAcrossDaysAndGaps(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 1, "2024-01-01T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 2, "2024-01-02T08:00:00Z", "Apple Watch de Gauthier"},
		// 2024-01-03 has nothing at all.
		{"steps", 3, "2024-01-04T08:00:00Z", "Apple Watch de Gauthier"},
		{"steps", 4, "2024-01-05T08:00:00Z", "Phone Health Kit"},
		{"steps", 5, "2024-01-06T08:00:00Z", "Apple Watch de Gauthier"},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-07T00:00:00Z")
	req.AccountID, req.Metric = acc, "steps"
	f, err := e.resolveSource(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if len(f.runs) != 3 {
		t.Fatalf("runs = %+v, want 3 (watch, phone, watch)", f.runs)
	}
	if f.runs[0].from != "2024-01-01T00:00:00Z" || f.runs[0].to != "2024-01-05T00:00:00Z" {
		t.Errorf("first run = %+v, want the three watch days merged across the empty one", f.runs[0])
	}
	pred, _ := f.where(req)
	if strings.Count(pred, "source = ?") != 3 {
		t.Errorf("predicate = %q, want one clause per run", pred)
	}
}
