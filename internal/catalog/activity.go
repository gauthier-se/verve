package catalog

import "strings"

// Activity display: what a Session was, as a screen can show it.
//
// The Activity slug itself is open by construction — a Connector derives it from
// its source type without a table, so all ~80 of Apple's arrive and none is
// dropped (ADR 0011). What closes is the display: the table below names the
// activities a person actually records, and anything outside it falls back to
// its own prettified slug rather than disappearing (ADR 0002).
//
// This lives in the Catalog and not in the web client because Group is a
// server-side filter. A server that did not know the groups would force a client
// to enumerate slugs for "all cardio", which silently drops every activity added
// after that client shipped.

// Group is the coarse family an Activity belongs to, and the unit the workout
// list filters by.
type Group string

const (
	GroupCardio   Group = "cardio"
	GroupStrength Group = "strength"
	GroupWater    Group = "water"
	GroupWinter   Group = "winter"
	GroupOther    Group = "other"
)

// Reading is how an Activity's speed reads: a runner thinks in minutes per
// kilometre, a cyclist in kilometres per hour, and a strength session in
// neither. It is a property of the Activity rather than an Account preference,
// because the data already knows the answer.
type Reading string

const (
	ReadingPace  Reading = "pace"  // min/km
	ReadingSpeed Reading = "speed" // km/h
	ReadingNone  Reading = "none"
)

// Activity is one Activity as a screen shows it.
type Activity struct {
	Slug    string  `json:"slug"`
	Label   string  `json:"label"`
	Group   Group   `json:"group"`
	Reading Reading `json:"reading"`
}

// activities is the curated display set, declared as data. Adding a row changes
// what a screen shows and never what an import keeps.
var activities = buildActivities()

func buildActivities() map[string]Activity {
	rows := []Activity{
		// Cardio, on foot or on wheels.
		{"running", "Running", GroupCardio, ReadingPace},
		{"walking", "Walking", GroupCardio, ReadingPace},
		{"hiking", "Hiking", GroupCardio, ReadingPace},
		{"cycling", "Cycling", GroupCardio, ReadingSpeed},
		{"elliptical", "Elliptical", GroupCardio, ReadingNone},
		{"rowing", "Rowing", GroupCardio, ReadingSpeed},
		{"stair_climbing", "Stair Climbing", GroupCardio, ReadingNone},
		{"stairs", "Stairs", GroupCardio, ReadingNone},
		{"step_training", "Step Training", GroupCardio, ReadingNone},
		{"high_intensity_interval_training", "HIIT", GroupCardio, ReadingNone},
		{"mixed_cardio", "Mixed Cardio", GroupCardio, ReadingNone},
		{"jump_rope", "Jump Rope", GroupCardio, ReadingNone},
		{"cardio_dance", "Cardio Dance", GroupCardio, ReadingNone},
		{"dance", "Dance", GroupCardio, ReadingNone},
		{"social_dance", "Social Dance", GroupCardio, ReadingNone},
		{"wheelchair_walk_pace", "Wheelchair (Walk Pace)", GroupCardio, ReadingSpeed},
		{"wheelchair_run_pace", "Wheelchair (Run Pace)", GroupCardio, ReadingSpeed},

		// Strength and the studio floor.
		{"traditional_strength_training", "Strength Training", GroupStrength, ReadingNone},
		{"functional_strength_training", "Functional Strength", GroupStrength, ReadingNone},
		{"core_training", "Core Training", GroupStrength, ReadingNone},
		{"cross_training", "Cross Training", GroupStrength, ReadingNone},
		{"barre", "Barre", GroupStrength, ReadingNone},
		{"pilates", "Pilates", GroupStrength, ReadingNone},
		{"yoga", "Yoga", GroupStrength, ReadingNone},
		{"flexibility", "Flexibility", GroupStrength, ReadingNone},
		{"cooldown", "Cooldown", GroupStrength, ReadingNone},
		{"mind_and_body", "Mind and Body", GroupStrength, ReadingNone},
		{"preparation_and_recovery", "Preparation and Recovery", GroupStrength, ReadingNone},

		// Water.
		{"swimming", "Swimming", GroupWater, ReadingPace},
		{"water_fitness", "Water Fitness", GroupWater, ReadingNone},
		{"water_polo", "Water Polo", GroupWater, ReadingNone},
		{"water_sports", "Water Sports", GroupWater, ReadingNone},
		{"surfing_sports", "Surfing", GroupWater, ReadingNone},
		{"paddle_sports", "Paddle Sports", GroupWater, ReadingSpeed},
		{"sailing", "Sailing", GroupWater, ReadingSpeed},

		// Winter.
		{"downhill_skiing", "Downhill Skiing", GroupWinter, ReadingSpeed},
		{"cross_country_skiing", "Cross-Country Skiing", GroupWinter, ReadingSpeed},
		{"snowboarding", "Snowboarding", GroupWinter, ReadingSpeed},
		{"snow_sports", "Snow Sports", GroupWinter, ReadingSpeed},
		{"skating_sports", "Skating", GroupWinter, ReadingSpeed},
		{"hockey", "Hockey", GroupWinter, ReadingNone},
		{"curling", "Curling", GroupWinter, ReadingNone},

		// Everything else that is common enough to deserve a name.
		{"tennis", "Tennis", GroupOther, ReadingNone},
		{"table_tennis", "Table Tennis", GroupOther, ReadingNone},
		{"badminton", "Badminton", GroupOther, ReadingNone},
		{"squash", "Squash", GroupOther, ReadingNone},
		{"racquetball", "Racquetball", GroupOther, ReadingNone},
		{"soccer", "Soccer", GroupOther, ReadingNone},
		{"basketball", "Basketball", GroupOther, ReadingNone},
		{"american_football", "American Football", GroupOther, ReadingNone},
		{"rugby", "Rugby", GroupOther, ReadingNone},
		{"baseball", "Baseball", GroupOther, ReadingNone},
		{"volleyball", "Volleyball", GroupOther, ReadingNone},
		{"handball", "Handball", GroupOther, ReadingNone},
		{"golf", "Golf", GroupOther, ReadingNone},
		{"climbing", "Climbing", GroupOther, ReadingNone},
		{"equestrian_sports", "Equestrian", GroupOther, ReadingNone},
		{"boxing", "Boxing", GroupOther, ReadingNone},
		{"kickboxing", "Kickboxing", GroupOther, ReadingNone},
		{"martial_arts", "Martial Arts", GroupOther, ReadingNone},
		{"wrestling", "Wrestling", GroupOther, ReadingNone},
		{"gymnastics", "Gymnastics", GroupOther, ReadingNone},
		{"track_and_field", "Track and Field", GroupOther, ReadingNone},
		{"fishing", "Fishing", GroupOther, ReadingNone},
		{"hunting", "Hunting", GroupOther, ReadingNone},
		{"play", "Play", GroupOther, ReadingNone},
		{"fitness_gaming", "Fitness Gaming", GroupOther, ReadingNone},
		{"other", "Other", GroupOther, ReadingNone},
	}

	byslug := make(map[string]Activity, len(rows))
	for _, a := range rows {
		byslug[a.Slug] = a
	}
	return byslug
}

// LookupActivity returns the display entry for an Activity slug. An unknown slug
// is never an error and never blank: it is displayed as its own prettified self,
// in the Other group, with no speed reading. An Activity Apple adds next year is
// therefore listed and filterable the day it first appears in an export.
func LookupActivity(slug string) Activity {
	if a, ok := activities[slug]; ok {
		return a
	}
	return Activity{
		Slug:    slug,
		Label:   prettifySlug(slug),
		Group:   GroupOther,
		Reading: ReadingNone,
	}
}

// Groups returns the Activity groups in display order.
func Groups() []Group {
	return []Group{GroupCardio, GroupStrength, GroupWater, GroupWinter, GroupOther}
}

// IsGroup reports whether s names an Activity group.
func IsGroup(s string) bool {
	for _, g := range Groups() {
		if string(g) == s {
			return true
		}
	}
	return false
}

// GroupSlugs returns the Activity slugs a group filter must match, and whether
// the match is negated. Every group but Other is a plain set of known slugs. Other
// is the complement of the rest, because it is where unknown Activities land and
// those cannot be enumerated: a listing filtered on Other must return the
// activity Apple invented last month, not silently omit it.
func GroupSlugs(g Group) (slugs []string, negated bool) {
	if g == GroupOther {
		for _, a := range activities {
			if a.Group != GroupOther {
				slugs = append(slugs, a.Slug)
			}
		}
		return slugs, true
	}
	for _, a := range activities {
		if a.Group == g {
			slugs = append(slugs, a.Slug)
		}
	}
	return slugs, false
}

// prettifySlug turns running → "Running" and traditional_strength_training →
// "Traditional Strength Training": the fallback label for an Activity outside the
// curated set.
func prettifySlug(slug string) string {
	if slug == "" {
		return "Unknown"
	}
	words := strings.Split(slug, "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
