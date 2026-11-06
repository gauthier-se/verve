package store

import "context"

// Preferences are the settings of a user that drive display and calculations. A user who set none
// has the zero Preferences.
type Preferences struct {
	// Timezone is the IANA name of the timezone of the user, such as Europe/Paris, or empty until
	// they confirm one. preferences.SetTimezone checks the name.
	Timezone string
}

// PreferencesRepository stores the preferences of the users. They are deleted with their user.
type PreferencesRepository interface {
	// Get returns the preferences of the user of userID, or ErrNotFound when no user has userID.
	Get(ctx context.Context, userID int64) (Preferences, error)
	// SetTimezone replaces the timezone of the user of userID and returns their preferences, or
	// returns ErrNotFound when no user has userID. An empty timezone leaves the user without one.
	SetTimezone(ctx context.Context, userID int64, timezone string) (Preferences, error)
}
