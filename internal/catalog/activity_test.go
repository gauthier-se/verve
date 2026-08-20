package catalog

import (
	"slices"
	"testing"
)

// TestLookupActivityFallback is the guard on ADR 0011's half of the split: the
// import keeps every Activity a Connector emits, so an Activity outside the
// curated table must still be displayable. A blank label or a panic here is a
// workout that disappears from the list the day Apple adds a sport.
func TestLookupActivityFallback(t *testing.T) {
	got := LookupActivity("underwater_basket_weaving")
	if got.Label != "Underwater Basket Weaving" {
		t.Errorf("label = %q, want %q", got.Label, "Underwater Basket Weaving")
	}
	if got.Group != GroupOther {
		t.Errorf("group = %q, want %q", got.Group, GroupOther)
	}
	if got.Reading != ReadingNone {
		t.Errorf("reading = %q, want %q", got.Reading, ReadingNone)
	}

	// The degenerate case an empty activity_type would produce.
	if label := LookupActivity("").Label; label == "" {
		t.Error("empty slug produced an empty label")
	}
}

func TestLookupActivityKnown(t *testing.T) {
	run := LookupActivity("running")
	if run.Label != "Running" || run.Group != GroupCardio || run.Reading != ReadingPace {
		t.Errorf("running = %+v, want Running/cardio/pace", run)
	}
	ride := LookupActivity("cycling")
	if ride.Reading != ReadingSpeed {
		t.Errorf("cycling reading = %q, want %q", ride.Reading, ReadingSpeed)
	}
	lift := LookupActivity("traditional_strength_training")
	if lift.Group != GroupStrength || lift.Reading != ReadingNone {
		t.Errorf("strength = %+v, want strength/none", lift)
	}
}

// TestActivityTableIsCoherent: every row names a group that exists, no label is
// blank, and no slug is declared twice under different labels.
func TestActivityTableIsCoherent(t *testing.T) {
	for slug, a := range activities {
		if a.Slug != slug {
			t.Errorf("row %q carries slug %q", slug, a.Slug)
		}
		if a.Label == "" {
			t.Errorf("activity %q has no label", slug)
		}
		if !IsGroup(string(a.Group)) {
			t.Errorf("activity %q is in unknown group %q", slug, a.Group)
		}
		switch a.Reading {
		case ReadingPace, ReadingSpeed, ReadingNone:
		default:
			t.Errorf("activity %q has unknown reading %q", slug, a.Reading)
		}
	}
}

// TestGroupSlugsOtherIsAComplement is the point of the negated filter: Other is
// where unknown Activities land, and they cannot be enumerated. Filtering on
// Other by listing its known members would omit exactly the Activities that most
// need it.
func TestGroupSlugsOtherIsAComplement(t *testing.T) {
	slugs, negated := GroupSlugs(GroupOther)
	if !negated {
		t.Fatal("GroupSlugs(other) must negate, or an unknown Activity is unreachable")
	}
	if slices.Contains(slugs, "tennis") {
		t.Error("tennis is in Other and must not be excluded from it")
	}
	if !slices.Contains(slugs, "running") {
		t.Error("running is not in Other and must be excluded from it")
	}

	cardio, negated := GroupSlugs(GroupCardio)
	if negated {
		t.Error("GroupSlugs(cardio) must not negate")
	}
	if !slices.Contains(cardio, "running") || slices.Contains(cardio, "swimming") {
		t.Errorf("cardio slugs = %v, want running in and swimming out", cardio)
	}
}
