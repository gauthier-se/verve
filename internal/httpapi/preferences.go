package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/gauthier-se/verve/internal/preferences"
	"github.com/gauthier-se/verve/internal/store"
)

// Preferences are the preferences of the user of a session.
type Preferences struct {
	Timezone *string `json:"timezone" nullable:"true" example:"Europe/Paris" doc:"The IANA name of the timezone of the user, or null until they confirm one: their days are in UTC until then."`
}

// preferencesOutput is the response of the preferences operations.
type preferencesOutput struct {
	Body Preferences
}

// updatePreferencesInput is the input of PATCH /api/v1/account/preferences.
type updatePreferencesInput struct {
	Body struct {
		Timezone *string `json:"timezone,omitempty" example:"Europe/Paris" doc:"The IANA name of the timezone of the user. Left out, it is unchanged."`
	}
}

// registerPreferences registers GET and PATCH /api/v1/account/preferences, which read and change the
// preferences of the user of the session in st. A change of timezone recomputes the aggregates of
// the user with aggregates.
func registerPreferences(api huma.API, st store.Store, aggregates preferences.Recomputer) {
	huma.Register(api, huma.Operation{
		OperationID: "get-preferences",
		Method:      http.MethodGet,
		Path:        "/api/v1/account/preferences",
		Summary:     "Get the preferences of the current user",
		Description: "Returns the preferences of the user of the session.",
		Tags:        []string{"Account"},
		Security:    sessionSecurity(),
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*preferencesOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		prefs, err := st.Preferences().Get(ctx, principal.UserID)
		if err != nil {
			return nil, preferencesError("get preferences", err)
		}

		return &preferencesOutput{Body: preferencesOf(prefs)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-preferences",
		Method:      http.MethodPatch,
		Path:        "/api/v1/account/preferences",
		Summary:     "Change the preferences of the current user",
		Description: "Changes the preferences given in the body, keeps the others, and returns the preferences. A " +
			"timezone that is not an IANA name, such as `Europe/Paris`, answers 422 `timezone_invalid`.",
		Tags:     []string{"Account"},
		Security: sessionSecurity(),
		Errors:   []int{http.StatusUnauthorized, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *updatePreferencesInput) (*preferencesOutput, error) {
		principal, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}

		var prefs store.Preferences
		if in.Body.Timezone == nil {
			prefs, err = st.Preferences().Get(ctx, principal.UserID)
		} else {
			prefs, err = preferences.SetTimezone(ctx, st, aggregates, principal.UserID, *in.Body.Timezone)
		}

		if err != nil {
			return nil, preferencesError("update preferences", err)
		}

		return &preferencesOutput{Body: preferencesOf(prefs)}, nil
	})
}

// preferencesError returns the problem of the error of an operation on the preferences: 422
// timezone_invalid for a timezone that is not an IANA name, 401 for a user deleted since the session
// lookup, and the error, as the error of action, otherwise.
func preferencesError(action string, err error) error {
	switch {
	case errors.Is(err, preferences.ErrTimezoneInvalid):
		return codedProblem(http.StatusUnprocessableEntity, preferences.ErrTimezoneInvalid)
	case errors.Is(err, store.ErrNotFound):
		return huma.NewError(http.StatusUnauthorized, "")
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}

// preferencesOf returns prefs as the API describes the preferences.
func preferencesOf(prefs store.Preferences) Preferences {
	return Preferences{Timezone: timezoneOf(prefs)}
}

// timezoneOf returns the timezone of prefs as the API describes it, nil for none.
func timezoneOf(prefs store.Preferences) *string {
	if prefs.Timezone == "" {
		return nil
	}

	return &prefs.Timezone
}
