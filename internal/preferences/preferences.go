// Package preferences holds the settings of each user that drive display and calculations (user
// preferences and profile). M1 has the timezone only.
package preferences

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// ErrTimezoneInvalid is returned for a timezone that is not an IANA name. Its message is the stable
// code that the API answers with.
var ErrTimezoneInvalid = errors.New("timezone_invalid")

// SetTimezone stores the IANA timezone name, such as Europe/Paris, as the timezone of the user of
// userID through repos, and returns their preferences. It returns ErrTimezoneInvalid when name is not
// one, and store.ErrNotFound when no user has userID.
func SetTimezone(ctx context.Context, repos store.Repositories, userID int64, name string) (store.Preferences, error) {
	if _, err := loadTimezone(name); err != nil {
		return store.Preferences{}, err
	}

	// The aggregates of M1-E8 will be recomputed in the new timezone from here.
	return repos.Preferences().SetTimezone(ctx, userID, name)
}

// Location returns the effective timezone of the user of userID, read in preferences: the one they
// confirmed, or UTC until they confirm one.
func Location(ctx context.Context, preferences store.PreferencesRepository, userID int64) (*time.Location, error) {
	got, err := preferences.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if got.Timezone == "" {
		return time.UTC, nil
	}

	loc, err := loadTimezone(got.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone of user %d: %w", userID, err)
	}

	return loc, nil
}

// loadTimezone returns the location of the IANA timezone name, or ErrTimezoneInvalid. time.LoadLocation
// reads the database of the system, then the one that cmd/verve embeds, so that every name loads on a
// system without one. It also takes the empty name and Local, which are not IANA names: the empty
// name stands for UTC, and Local for the zone of the server.
func loadTimezone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, ErrTimezoneInvalid
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrTimezoneInvalid
	}

	return loc, nil
}
