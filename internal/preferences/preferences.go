// Package preferences holds the settings of each user that drive display and calculations (user
// preferences and profile). M1 has the timezone only.
package preferences

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// ErrTimezoneInvalid is returned for a timezone that is not an IANA name. Its message is the stable
// code that the API answers with.
var ErrTimezoneInvalid = errors.New("timezone_invalid")

// Recomputer recomputes the aggregates of a user whose timezone changed.
type Recomputer interface {
	// RecomputeAll deletes the aggregates of the user of userID and records the ranges of all their
	// types, through tx, the transaction that stores their new timezone.
	RecomputeAll(ctx context.Context, tx store.Repositories, userID int64) error
}

// SetTimezone stores the IANA timezone name, such as Europe/Paris, as the timezone of the user of
// userID in st, and returns their preferences. A change of timezone recomputes the aggregates of
// the user with aggregates, in the transaction that stores it (FR-10 of Storage and aggregates). It
// returns ErrTimezoneInvalid when name is not one, and store.ErrNotFound when no user has userID.
func SetTimezone(ctx context.Context, st store.Store, aggregates Recomputer, userID int64, name string) (store.Preferences, error) {
	if _, err := loadTimezone(name); err != nil {
		return store.Preferences{}, err
	}

	var prefs store.Preferences

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		current, err := tx.Preferences().Get(ctx, userID)
		if err != nil {
			return err
		}

		prefs, err = tx.Preferences().SetTimezone(ctx, userID, name)
		if err != nil || current.Timezone == name {
			return err
		}

		return aggregates.RecomputeAll(ctx, tx, userID)
	})
	if err != nil {
		return store.Preferences{}, err
	}

	return prefs, nil
}

// Location returns the effective timezone of the user of userID, read in preferences: the one they
// confirmed, or UTC until they confirm one.
func Location(ctx context.Context, preferences store.PreferencesRepository, userID int64) (*time.Location, error) {
	prefs, err := preferences.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if prefs.Timezone == "" {
		return time.UTC, nil
	}

	loc, err := loadTimezone(prefs.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone of user %d: %w", userID, err)
	}

	return loc, nil
}

// loadTimezone returns the location of the IANA timezone name, or ErrTimezoneInvalid. time.LoadLocation
// reads the database of the system, then the one that cmd/verve embeds, so that every name loads on a
// system without one. It also takes names that are not IANA names:
//   - the empty name, which stands for UTC, and Local, the zone of the server;
//   - the posix/ and right/ trees that many systems install beside the IANA names, which the
//     embedded database lacks, so that they would stop loading on another system. right/ counts
//     leap seconds.
func loadTimezone(name string) (*time.Location, error) {
	if name == "" || name == "Local" || strings.HasPrefix(name, "posix/") || strings.HasPrefix(name, "right/") {
		return nil, ErrTimezoneInvalid
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrTimezoneInvalid
	}

	return loc, nil
}
