package catalog

import "testing"

func TestResolveSourcePrefersConfiguredPriority(t *testing.T) {
	// steps prefers the Watch over the iPhone to avoid double-counting.
	got, ok := ResolveSource("steps", []string{"Gauthier's iPhone", "Gauthier's Apple Watch"})
	if !ok || got != "Gauthier's Apple Watch" {
		t.Fatalf("ResolveSource(steps) = %q, %v; want the Apple Watch", got, ok)
	}
}

func TestResolveSourceUnmatchedRankAfterMatched(t *testing.T) {
	// A Source matching no priority pattern loses to one that matches, even if
	// it sorts earlier alphabetically.
	got, ok := ResolveSource("steps", []string{"AAA Fitness App", "My Apple Watch"})
	if !ok || got != "My Apple Watch" {
		t.Fatalf("ResolveSource = %q, %v; want the matched Watch source", got, ok)
	}
}

func TestResolveSourceNoPriorityFallsBackAlphabetical(t *testing.T) {
	// heart_rate has no configured priority, so resolution is the deterministic
	// alphabetical pick.
	got, ok := ResolveSource("heart_rate", []string{"Polar H10", "Apple Watch"})
	if !ok || got != "Apple Watch" {
		t.Fatalf("ResolveSource(heart_rate) = %q, %v; want alphabetical first", got, ok)
	}
}

func TestResolveSourceEmpty(t *testing.T) {
	if got, ok := ResolveSource("steps", nil); ok || got != "" {
		t.Fatalf("ResolveSource(empty) = %q, %v; want \"\", false", got, ok)
	}
}
